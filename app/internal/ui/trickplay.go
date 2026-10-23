package ui

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
	"runtime"
	"sync"
	"syscall"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
)

// The Jellyfin server's previews are trickplay sheets per media source
// (jellyfin.Trickplay): each a JPEG grid of TileWidth x TileHeight frames,
// one every Interval. A sheet is fetched and decoded in the background
// when the scrub nears it and cut into display-size frames; at most two
// are kept: the one under the dot and the next the way it moves.

// trickplayKeep is how many decoded sheets are kept.
const trickplayKeep = 2

// trickplayTries is how often a sheet is tried before it is given up.
const trickplayTries = 2

// trickplayNear is how far (in frames) Frame looks for a ready one while
// the one under the dot is still decoding, so the preview does not blink.
const trickplayNear = 6

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
	mu     sync.Mutex
	cond   *sync.Cond
	w, h   int
	tp     jellyfin.Trickplay
	want   int     // the frame the scrub is at, -1 before any
	at     float64 // where the direction was last taken from
	dir    int     // the way the scrub moves: +1 or -1
	sheets map[int][]*gfx.Image
	failed map[int]int // failed fetches or decodes, by sheet
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

func newTrickplayPreview(tp jellyfin.Trickplay, w, h int, start float64) *trickplayPreview {
	b := &trickplayPreview{w: w, h: h, tp: tp, want: -1, at: start, dir: 1,
		sheets: map[int][]*gfx.Image{}, failed: map[int]int{}}
	b.cond = sync.NewCond(&b.mu)
	b.Near(start)
	return b
}

// run fetches and decodes what Near asks for, a sheet at a time, on its
// own thread at a low priority as the art workers do.
func (b *trickplayPreview) run(fetch func(n int) ([]byte, error)) {
	runtime.LockOSThread()
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
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
		b.mu.Unlock()
		var frames []*gfx.Image
		data, err := fetch(n)
		if err == nil {
			decoding(func() {
				var img image.Image
				if img, err = jpeg.Decode(bytes.NewReader(data)); err == nil {
					frames = trickplayCut(img, tp, n, w, h)
				}
			})
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return
		}
		if frames == nil {
			b.failed[n]++
			if b.failed[n] >= trickplayTries {
				log.Printf("preview: sheet %d failed", n)
			}
		} else {
			b.add(n, frames)
		}
		b.mu.Unlock()
	}
}

// wanted is the sheets to have: the one under the dot, then the next the
// way it moves (-1 for none). Called under mu.
func (b *trickplayPreview) wanted() [trickplayKeep]int {
	ws := [trickplayKeep]int{-1, -1}
	if b.want < 0 {
		return ws
	}
	s := b.want / trickplayFrames(b.tp)
	ws[0] = s
	if s+b.dir >= 0 && s+b.dir < trickplaySheets(b.tp) {
		ws[1] = s + b.dir
	}
	return ws
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
	ws := b.wanted()
	if n == ws[0] || n == ws[1] {
		b.sheets[n] = frames
	}
	b.drop()
}

// drop lets go of the sheets no longer wanted. Called under mu.
func (b *trickplayPreview) drop() {
	ws := b.wanted()
	for k := range b.sheets {
		if k != ws[0] && k != ws[1] {
			delete(b.sheets, k)
		}
	}
}

func (b *trickplayPreview) Near(t float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if d := t - b.at; d >= 1 || d <= -1 {
		b.dir = 1
		if d < 0 {
			b.dir = -1
		}
		b.at = t
	}
	i, _, _, _ := trickplayAt(b.tp, t)
	before := b.wanted()
	b.want = i
	if b.wanted() != before {
		b.drop()
		b.cond.Broadcast()
	}
}

func (b *trickplayPreview) Frame(t float64) *gfx.Image {
	i, _, _, _ := trickplayAt(b.tp, t)
	if i < 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	per := trickplayFrames(b.tp)
	for d := 0; d <= trickplayNear; d++ {
		for _, k := range []int{i - d, i + d} {
			if k < 0 {
				continue
			}
			if frames := b.sheets[k/per]; k%per < len(frames) && frames[k%per] != nil {
				return frames[k%per]
			}
		}
	}
	return nil
}

func (b *trickplayPreview) Close() {
	b.mu.Lock()
	b.closed = true
	b.sheets = nil
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
