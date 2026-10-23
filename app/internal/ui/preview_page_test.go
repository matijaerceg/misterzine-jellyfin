package ui

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// pageOSD keeps the overlay's two pages as the core's are kept: a Show
// writes the lines it covers into the page not on screen, alpha merged,
// and leaves the rest of that page as it was.
type pageOSD struct {
	coreOSD
	pages [2][]byte
	cur   int
}

func (o *pageOSD) Show(x, y int, cv *gfx.Canvas, alpha []byte) {
	o.coreOSD.Show(x, y, cv, alpha)
	o.cur ^= 1
	if o.pages[o.cur] == nil {
		o.pages[o.cur] = make([]byte, 720*480*4)
	}
	pg := o.pages[o.cur]
	for r := 0; r < cv.H; r++ {
		row := pg[(y+r)*720*4 : (y+r+1)*720*4]
		clear(row)
		copy(row[x*4:], cv.Pix[r*cv.W*4:(r+1)*cv.W*4])
		for i := 0; i < cv.W; i++ {
			row[(x+i)*4+3] = alpha[r*cv.W+i]
		}
	}
}

// drawn reports whether any byte of rows [y0, y1) of either page is set.
func (o *pageOSD) drawn(y0, y1 int) bool {
	for _, pg := range o.pages {
		if pg == nil {
			continue
		}
		for _, b := range pg[y0*720*4 : y1*720*4] {
			if b != 0 {
				return true
			}
		}
	}
	return false
}

// fieldsOn runs Tick for n fields on any OSD, as tickFields does.
func fieldsOn(p *Playing, o OSD, vsync func(), now time.Time, n int, each func()) time.Time {
	for i := 0; i < n; i++ {
		vsync()
		p.Tick(now, o, true)
		p.painter.flush()
		if each != nil {
			each()
		}
		now = now.Add(16683 * time.Microsecond)
	}
	return now
}

func brightPreview(t *testing.T) (*Playing, *fakePreview) {
	p, fp, _ := previewPlaying(t)
	for i := range fp.img.Pix {
		fp.img.Pix[i] = 0xEE
	}
	return p, fp
}

// A frame moved on leaves nothing of where it was: the lift rows are
// cleared whole, pixels and alpha, every time they are drawn.
func TestAMovedFrameLeavesNothingOfTheLast(t *testing.T) {
	p, fp := brightPreview(t)
	p.pvImg, p.scrubTo = fp.img, 900
	p.drawPreview(previewLift)
	first := append([]byte(nil), p.canvas.Pix[:720*previewLift*4]...)
	p.scrubTo += 40 * p.dur / float64(p.safeW()) // 40 px on
	p.drawPreview(previewLift)
	moved := 0
	for i := 0; i < 720*previewLift; i++ {
		if first[i*4] == 0xEE && p.alpha[i] == 0 {
			moved++
			if !bytes.Equal(p.canvas.Pix[i*4:i*4+4], make([]byte, 4)) {
				t.Fatalf("pixel %d keeps the last frame", i)
			}
		}
	}
	if moved == 0 {
		t.Fatal("the frame did not move")
	}
}

// A run with no frame ready (there was one, then the dot outran the
// decodes) shows the placeholder above the strip, nothing else, and once
// the hold ends the rows go clear: no pixel, no alpha.
func TestARunWithoutAFrameLeavesTheRowsClear(t *testing.T) {
	p, fp := brightPreview(t)
	c := &coreOSD{}
	now := time.Now()
	p.framed(now)
	now = tickFields(p, c, now, 1, nil)
	keyAt(p, c, input.Right, now)
	now = tickFields(p, c, now, 40, nil)
	if p.pvImg == nil {
		t.Fatal("no frame while one is ready")
	}
	fp.ready = false
	placeholder, shows := 0, c.shows
	now = tickFields(p, c, now, 6, func(int) {
		if p.pvImg != nil {
			t.Fatal("a frame not ready is still drawn")
		}
		if p.pvDot == nil {
			t.Fatal("no placeholder while the run waits on a frame")
		}
		if c.shows > shows && c.showH == p.peekStripH()+previewLift {
			n := 0
			for _, a := range c.showAlpha[:previewLift*c.showW] {
				if a != 0 {
					n++
				}
			}
			fw, fh := previewSize(720, 480)
			if n != fw*fh {
				t.Fatalf("%d pixels drawn above the strip, the box is %d", n, fw*fh)
			}
			placeholder++
		}
	})
	if placeholder == 0 {
		t.Fatal("the placeholder was never shown")
	}
	p.Key(input.Event{Key: input.Right, Release: true}, now)
	checked := 0
	tickFields(p, c, now, 40, func(int) {
		if p.pvShown() {
			return // until the gesture's seek, as a frame would
		}
		if c.showH > p.peekStripH() {
			lift := c.showH - p.peekStripH()
			for i, b := range c.showPix[:lift*c.showW*4] {
				if b != 0 || c.showAlpha[i/4] != 0 {
					t.Fatalf("byte %d above the strip is drawn", i)
				}
			}
			checked++
		}
	})
	if checked == 0 {
		t.Fatal("the rows were never shown clear")
	}
	if p.pvShown() || p.lift() != 0 {
		t.Fatal("the placeholder outlived the hold")
	}
}

// Ending the preview leaves nothing above the panel on either page, and
// the panel back at its own height.
func TestEndingThePreviewLeavesNothingAboveThePanel(t *testing.T) {
	p, _ := brightPreview(t)
	c := &pageOSD{}
	now := time.Now()
	p.framed(now)
	now = fieldsOn(p, c, c.vsync, now, 1, nil)
	p.Key(input.Event{Key: input.Up}, now)
	p.Tick(now, c, false)
	now = fieldsOn(p, c, c.vsync, now, 2, nil)
	p.Key(input.Event{Key: input.Right}, now)
	lifted := false
	now = fieldsOn(p, c, c.vsync, now, 40, func() {
		if c.showY == p.osdY()-previewLift {
			lifted = true
		}
	})
	if !lifted || !c.drawn(p.osdY()-previewLift, p.osdY()) {
		t.Fatal("the frame was never drawn above the panel")
	}
	p.Key(input.Event{Key: input.Right, Release: true}, now)
	fieldsOn(p, c, c.vsync, now, 40, nil)
	if p.pvImg != nil || c.showY != p.osdY() || c.showH != p.osdHeight() {
		t.Fatalf("panel at %d, %d high after the preview", c.showY, c.showH)
	}
	if c.drawn(0, p.osdY()) {
		t.Fatal("the pages keep the preview above the panel")
	}
}

// trickplayServer serves item 9's trickplay: sheets of 2x2 frames of
// 32x18, one a second, n in all, each sheet a 64x36 JPEG.
func trickplayServer(t *testing.T, n int) *httptest.Server {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 64, 36)), nil); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/Videos/9/Trickplay/32/"):
			w.Write(buf.Bytes())
		case strings.HasSuffix(r.URL.Path, "/Items/9"):
			w.Write([]byte(`{"Id":"9","Type":"Movie","Name":"A","RunTimeTicks":18000000000,` +
				`"MediaSources":[{"Id":"101"}],"Trickplay":{"101":{"32":{"Width":32,"Height":18,` +
				`"TileWidth":2,"TileHeight":2,"ThumbnailCount":` + strconv.Itoa(n) + `,"Interval":1000}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// The previewer the first Tick makes prepares round the start, so the
// hold's first running field already has its frame.
func TestTheFirstTicksPreviewerHasTheHoldsFirstFrame(t *testing.T) {
	p := waitPlaying(t)
	p.app.Client = testClient(trickplayServer(t, 1800).URL, "t", t.TempDir())
	p.item.ID, p.item.SourceID = "9", "101"
	p.item.Trickplay = &jellyfin.Trickplay{Width: 32, Height: 18, TileWidth: 2, TileHeight: 2, Count: 1800, Interval: 1000}
	c := &coreOSD{}
	now := time.Now()
	p.framed(now)
	now = tickFields(p, c, now, 1, nil)
	if p.preview == nil {
		t.Fatal("the first Tick made no previewer")
	}
	defer p.closePainter(c)
	deadline := time.Now().Add(5 * time.Second)
	for p.preview.Frame(p.pos) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the frames round the start were never made")
		}
		time.Sleep(time.Millisecond)
	}
	first := true
	hold(p, c, now, time.Second, func() {
		if p.pvOn && first {
			first = false
			if p.pvImg == nil {
				t.Fatal("the hold's first field has no frame")
			}
		}
	})
	if first {
		t.Fatal("the hold never ran")
	}
}

// A listing's item may come with its markers and source but not its
// previews: the playback asks for the item again and starts them then.
func TestAnItemWithoutItsIndexesIsAskedForAgain(t *testing.T) {
	a := cropApp(t)
	a.Client = testClient(trickplayServer(t, 1800).URL, "t", t.TempDir())
	it := &jellyfin.Item{ID: "9", Type: "movie", Title: "A", Duration: 1800, SourceID: "101",
		Markers: []jellyfin.Marker{{Type: "intro", End: 5}}}
	p := NewPlaying(a, it, nil, 0, func(string) {})
	c := &coreOSD{}
	defer p.closePainter(c)
	now := time.Now()
	deadline := time.Now().Add(5 * time.Second)
	for p.preview == nil {
		if time.Now().After(deadline) {
			t.Fatal("the previews never started")
		}
		now = tickFields(p, c, now, 1, nil)
		time.Sleep(time.Millisecond)
	}
}
