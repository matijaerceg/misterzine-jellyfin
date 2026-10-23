package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
)

// The Jellyfin server's previews are trickplay sheets per media source
// (jellyfin.Trickplay): each a JPEG grid of TileWidth x TileHeight frames,
// one every Interval. A sheet is fetched and decoded in the background and
// cut into display-size frames. Three are kept, as the previewer contract
// asks: the one under the position and the one either side of it,
// prepared in that order with the neighbour the scrub moves towards first.

// trickplayKeep is how many decoded sheets are kept.
const trickplayKeep = 3

// trickplayTries is how often a sheet is tried before it is given up.
const trickplayTries = 2

// trickplayNear is how far (in frames) Frame looks for a ready one while
// the one under the dot is still decoding, so the preview does not blink.
const trickplayNear = 6

// trickplayRunNice is the worker's priority while a run waits on it.
const trickplayRunNice = 0

// trickplayLeadMax caps how far (in frames) a fast run widens Frame's
// look for a ready frame.
const trickplayLeadMax = 60

// trickplayFrames is how many frames a sheet holds.
func trickplayFrames(tp jellyfin.Trickplay) int { return tp.TileWidth * tp.TileHeight }

// trickplaySheets is how many sheets there are.
func trickplaySheets(tp jellyfin.Trickplay) int {
	n := trickplayFrames(tp)
	if n <= 0 || tp.Count <= 0 {
		return 0
	}
	return (tp.Count + n - 1) / n
}

// trickplayAt is the frame for film time t: its number, its sheet and its
// column and row there. The last frame stands for everything after it.
func trickplayAt(tp jellyfin.Trickplay, t float64) (thumb, sheet, x, y int) {
	n := trickplayFrames(tp)
	if n <= 0 || tp.Interval <= 0 || tp.Count <= 0 || tp.TileWidth <= 0 {
		return -1, -1, 0, 0
	}
	thumb = max(0, min(int(t*1000)/tp.Interval, tp.Count-1))
	tile := thumb % n
	return thumb, thumb / n, tile % tp.TileWidth, tile / tp.TileWidth
}

// trickplayPreview is the previewer over a source's trickplay sheets.
type trickplayPreview struct {
	mu   sync.Mutex
	cond *sync.Cond
	w, h int
	tp   jellyfin.Trickplay
	want int     // the frame the scrub is at, -1 before any
	at   float64 // the last Near's time
	dir  int     // the way the scrub moves: +1 or -1
	// how fast it moves (frames a second), when want last changed, and
	// how long a sheet takes to decode (seconds, averaged): a run widens
	// Frame's look by the frames the dot passes in a decode
	speed   float64
	movedAt time.Time
	dec     float64
	sheets  map[int][]*gfx.Image
	last    *gfx.Image // Frame's last frame, and its number
	lastAt  int
	failed  map[int]int // failed fetches or decodes, by sheet
	// the worker's priority, and the sheet under the start position while
	// it is still to come (-1 after): that one decodes at normal priority
	boot   int
	prio   workerPrio
	closed bool
	cancel context.CancelFunc
}

// newPreviewer starts the item's previews: nil when its source has none.
func newPreviewer(app *App, it *jellyfin.Item, w, h int, start float64) previewer {
	if app == nil || app.Client == nil || it == nil || it.Trickplay == nil || trickplaySheets(*it.Trickplay) == 0 {
		return nil
	}
	b := newTrickplayPreview(*it.Trickplay, w, h, start)
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	client, id, source, width := app.Client, it.ID, it.SourceID, it.Trickplay.Width
	go b.run(func(n int) ([]byte, error) { return client.TrickplaySheet(ctx, id, source, width, n) })
	return b
}

// newTrickplayPreview makes a previewer wanting the sheets round start, so
// its worker prepares them from the first without waiting for a Near.
func newTrickplayPreview(tp jellyfin.Trickplay, w, h int, start float64) *trickplayPreview {
	b := &trickplayPreview{w: w, h: h, tp: tp, want: -1, at: start, dir: 1, dec: 2,
		sheets: map[int][]*gfx.Image{}, failed: map[int]int{}, prio: newWorkerPrio()}
	b.cond = sync.NewCond(&b.mu)
	b.want, b.boot, _, _ = trickplayAt(tp, start)
	return b
}

// run fetches and decodes the wanted sheets, a sheet at a time, on its
// own thread: at nice 10 as the art workers do, at normal priority for
// the sheet under the start and while a held run waits on it
// (previewNice).
func (b *trickplayPreview) run(fetch func(n int) ([]byte, error)) {
	runtime.LockOSThread()
	b.mu.Lock()
	b.prio.tid = syscall.Gettid()
	b.mu.Unlock()
	for {
		b.mu.Lock()
		n := -1
		for !b.closed {
			if n = b.next(); n >= 0 {
				break
			}
			b.cond.Wait()
		}
		if b.closed {
			b.mu.Unlock()
			return
		}
		tp, w, h := b.tp, b.w, b.h
		// a run waits on this (the paused picture leaves the time), or it
		// is the first: at nice 10 a sheet took half a minute
		b.prio.to(previewNice(b.lead() != 0, n == b.boot))
		b.mu.Unlock()
		var frames []*gfx.Image
		data, err := fetch(n)
		at := time.Now()
		if err == nil {
			decoding(func() {
				var img image.Image
				if img, err = jpeg.Decode(bytes.NewReader(data)); err == nil {
					frames = trickplayCut(img, tp, n, w, h)
				}
			})
		}
		took := time.Since(at).Seconds()
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		if frames == nil {
			b.failed[n]++
			if b.failed[n] >= trickplayTries {
				log.Printf("preview: sheet %d failed", n)
				if n == b.boot {
					b.boot = -1
				}
			}
		} else {
			b.dec = (3*b.dec + took) / 4
			b.add(n, frames)
			if n == b.boot {
				b.boot = -1
			}
		}
		b.mu.Unlock()
	}
}

// wanted is the sheets to have, in the order to prepare them: the one
// under the position, the neighbour the scrub moves towards, the other
// neighbour (-1 for none). Called under mu.
func (b *trickplayPreview) wanted() [trickplayKeep]int {
	ws := [trickplayKeep]int{-1, -1, -1}
	if b.want < 0 {
		return ws
	}
	s := b.want / trickplayFrames(b.tp)
	ws[0] = s
	for k, d := range []int{b.dir, -b.dir} {
		if s+d >= 0 && s+d < trickplaySheets(b.tp) {
			ws[k+1] = s + d
		}
	}
	return ws
}

// wants is whether sheet n is one of the wanted. Called under mu.
func (b *trickplayPreview) wants(n int) bool {
	for _, w := range b.wanted() {
		if n >= 0 && n == w {
			return true
		}
	}
	return false
}

// next is the sheet to fetch, -1 for none. Called under mu.
func (b *trickplayPreview) next() int {
	for _, n := range b.wanted() {
		if n >= 0 && b.sheets[n] == nil && b.failed[n] < trickplayTries {
			return n
		}
	}
	return -1
}

// add keeps a decoded sheet if it is still wanted, and drops any sheet
// no longer wanted. Called under mu.
func (b *trickplayPreview) add(n int, frames []*gfx.Image) {
	if b.wants(n) {
		b.sheets[n] = frames
	}
	b.drop()
}

// drop lets go of the sheets that are none of the wanted. Called under mu.
func (b *trickplayPreview) drop() {
	for k := range b.sheets {
		if !b.wants(k) {
			delete(b.sheets, k)
		}
	}
}

// lead is how many frames the dot moves in a decode and a half, signed:
// 0 when it stands. Called under mu.
func (b *trickplayPreview) lead() int {
	if b.speed == 0 || time.Since(b.movedAt) > 250*time.Millisecond {
		return 0
	}
	l := int(b.speed * b.dec * 1.5)
	return max(-trickplayLeadMax, min(l, trickplayLeadMax))
}

func (b *trickplayPreview) Near(t float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	// later than the last Near is forward
	if t > b.at {
		b.dir = 1
	} else if t < b.at {
		b.dir = -1
	}
	b.at = t
	i, _, _, _ := trickplayAt(b.tp, t)
	if i == b.want {
		return
	}
	now := time.Now()
	if b.want >= 0 {
		// a run's speed, from successive moves; a jump (a seek, a first
		// move after a rest) is no speed
		if dt := now.Sub(b.movedAt).Seconds(); dt > 0 && dt < 0.25 && abs(i-b.want) <= trickplayLeadMax/4 {
			b.speed = (b.speed + float64(i-b.want)/dt) / 2
		} else {
			b.speed = 0
		}
	}
	before := b.wanted()
	b.want, b.movedAt = i, now
	if b.wanted() != before {
		b.drop()
	}
	if b.lead() != 0 {
		// a run: a decode under way speeds up now, not at the next sheet
		b.prio.to(trickplayRunNice)
	}
	b.cond.Broadcast()
}

func (b *trickplayPreview) Frame(t float64) *gfx.Image {
	i, _, _, _ := trickplayAt(b.tp, t)
	if i < 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	per := trickplayFrames(b.tp)
	// the nearest ready one: during a run the dot outpaces the decodes
	for d := 0; d <= trickplayNear+abs(b.lead()); d++ {
		for _, k := range []int{i - d, i + d} {
			if k < 0 {
				continue
			}
			if frames := b.sheets[k/per]; k%per < len(frames) && frames[k%per] != nil {
				b.last, b.lastAt = frames[k%per], k
				return b.last
			}
		}
	}
	// a run into a sheet still decoding: the last frame, from the sheet
	// next to it, stays until it lands
	if l := b.lead(); l != 0 && b.last != nil && abs(i-b.lastAt) <= per+trickplayNear+abs(l) {
		return b.last
	}
	return nil
}

func (b *trickplayPreview) Close() {
	b.mu.Lock()
	b.closed = true
	b.sheets, b.last = nil, nil
	b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
	}
	b.cond.Broadcast()
}

// trickplayCut cuts sheet n's frames out of its decoded picture at w x h,
// each tile converted alone (a whole sheet in RGBA would be tens of
// megabytes). A frame the picture does not hold whole ends the sheet.
// Run it holding a decode slot.
func trickplayCut(img image.Image, tp jellyfin.Trickplay, n, w, h int) []*gfx.Image {
	if ycc, ok := img.(*image.YCbCr); ok && w < tp.Width && h < tp.Height {
		return trickplayCutYCbCr(ycc, tp, n, w, h)
	}
	per := trickplayFrames(tp)
	count := min(per, tp.Count-n*per)
	bounds := img.Bounds()
	var frames []*gfx.Image
	for i := 0; i < count; i++ {
		x, y := i%tp.TileWidth, i/tp.TileWidth
		r := image.Rect(x*tp.Width, y*tp.Height, (x+1)*tp.Width, (y+1)*tp.Height).Add(bounds.Min)
		if !r.In(bounds) {
			break
		}
		rgba := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
		draw.Draw(rgba, rgba.Rect, img, r.Min, draw.Src)
		tile := &gfx.Image{W: r.Dx(), H: r.Dy(), Pix: rgba.Pix}
		for o := 0; o < len(tile.Pix); o += 4 {
			tile.Pix[o], tile.Pix[o+2], tile.Pix[o+3] = tile.Pix[o+2], tile.Pix[o], 0
		}
		frames = append(frames, previewImage(tile, w, h))
	}
	return frames
}

// trickplayCutYCbCr is trickplayCut for a decoded JPEG shrunk to frames
// smaller than its tiles: each frame is box-filtered straight from the
// luma, the chroma taken at each box's centre, without converting the
// tiles to RGBA first. On the device the RGBA route cost more than a
// second a sheet on top of the decode.
func trickplayCutYCbCr(img *image.YCbCr, tp jellyfin.Trickplay, n, w, h int) []*gfx.Image {
	per := trickplayFrames(tp)
	count := min(per, tp.Count-n*per)
	bounds := img.Bounds()
	tw, th := tp.Width, tp.Height
	xs := make([]int, w+1) // each frame column's span in the tile
	for i := range xs {
		xs[i] = i * tw / w
	}
	cx := make([]int, w) // and its chroma sample's offset along a row
	var frames []*gfx.Image
	for i := 0; i < count; i++ {
		x, y := i%tp.TileWidth, i/tp.TileWidth
		r := image.Rect(x*tw, y*th, (x+1)*tw, (y+1)*th).Add(bounds.Min)
		if !r.In(bounds) {
			break
		}
		for ox := range cx {
			cx[ox] = img.COffset(r.Min.X+(xs[ox]+xs[ox+1])/2, r.Min.Y) - img.COffset(r.Min.X, r.Min.Y)
		}
		out := &gfx.Image{W: w, H: h, Pix: make([]byte, w*h*4)}
		o := 0
		for oy := 0; oy < h; oy++ {
			y0, y1 := r.Min.Y+oy*th/h, r.Min.Y+(oy+1)*th/h
			crow := img.COffset(r.Min.X, (y0+y1)/2)
			for ox := 0; ox < w; ox++ {
				x0, x1 := r.Min.X+xs[ox], r.Min.X+xs[ox+1]
				sum := 0
				for sy := y0; sy < y1; sy++ {
					for _, v := range img.Y[img.YOffset(x0, sy):img.YOffset(x1, sy)] {
						sum += int(v)
					}
				}
				cnt := (y1 - y0) * (x1 - x0)
				c := crow + cx[ox]
				cr, cg, cb := color.YCbCrToRGB(uint8((sum+cnt/2)/cnt), img.Cb[c], img.Cr[c])
				out.Pix[o], out.Pix[o+1], out.Pix[o+2] = cb, cg, cr
				o += 4
			}
		}
		frames = append(frames, out)
	}
	return frames
}
