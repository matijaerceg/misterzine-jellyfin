package ui

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// collectionServer is a synthetic server with two box sets, which Jellyfin
// lists server-wide (every library's Collections tab shows them). The
// box sets' listing waits for hold to close, so a test can look before it lands,
// and answers with an error while failing is set. The first set holds a
// 4:3 film, or only a wide one while wideFirst is set.
type collectionServer struct {
	*httptest.Server
	hold      chan struct{}
	mu        sync.Mutex
	asks      map[string]int
	failing   bool
	wideFirst bool
}

func newCollectionServer(t *testing.T) *collectionServer {
	s := &collectionServer{hold: make(chan struct{}), asks: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		q := r.URL.Query()
		route := r.URL.Path
		switch {
		case r.URL.Path == "/Items" && q.Get("includeItemTypes") == "BoxSet":
			route = "collections"
		case r.URL.Path == "/Items" && q.Get("parentId") != "":
			route = "parent:" + q.Get("parentId")
		}
		s.asks[route]++
		failing, wideFirst := s.failing, s.wideFirst
		s.mu.Unlock()
		switch route {
		case "collections":
			if failing {
				http.Error(w, "busy", http.StatusServiceUnavailable)
				return
			}
			<-s.hold
			jfPage(w, r, `{"Id":"901","Type":"BoxSet","Name":"First Set","ImageTags":{"Primary":"c901"},"ChildCount":2}`,
				`{"Id":"9020","Type":"BoxSet","Name":"Second Set","ImageTags":{"Primary":"c902"},"BackdropImageTags":["a902"],"ChildCount":3}`)
		case "parent:1", "parent:2":
			jfPage(w, r, `{"Id":"11","Type":"Movie","Name":"Alpha"}`, `{"Id":"12","Type":"Movie","Name":"Beta"}`)
		case "parent:901":
			if wideFirst {
				jfPage(w, r, `{"Id":"31","Type":"Movie","Name":"Wide Two","Width":1920,"Height":800}`)
				return
			}
			jfPage(w, r, `{"Id":"32","Type":"Movie","Name":"Square Two","Width":720,"Height":540}`)
		case "parent:9020":
			// the collection's own order; one wide film for the 4:3 filter to hide
			jfPage(w, r, `{"Id":"23","Type":"Movie","Name":"Later Square","Width":640,"Height":480}`,
				`{"Id":"21","Type":"Movie","Name":"Wide One","Width":1920,"Height":1080}`,
				`{"Id":"22","Type":"Movie","Name":"Unmeasured"}`)
		case "/Items/23":
			io.WriteString(w, `{"Id":"23","Type":"Movie","Name":"Later Square","Overview":"A synthetic film.","Width":640,"Height":480}`)
		default:
			io.WriteString(w, `{"Items":[],"TotalRecordCount":0}`)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-s.hold:
		default:
			close(s.hold)
		}
		s.Close()
	})
	return s
}

func (s *collectionServer) asked(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asks[path]
}

func collectionTestApp(t *testing.T, s *collectionServer, fourThree bool) *App {
	t.Helper()
	a := &App{Client: testClient(s.URL, "tok", t.TempDir()), Wake: make(chan struct{}, 1), pagers: map[string]*Pager{},
		Cfg: &Config{NoTheme: true, FourThree: fourThree}, Log: log.New(io.Discard, "", 0), Art: NewArt(nil, 0, nil)}
	a.F = appFonts()
	a.T = gfx.NewTextCache(1, nil)
	a.stack = []Screen{&splashTestScreen{}}
	return a
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// settled reports whether p's first page has landed and nothing more is on
// its way: the pager asks for pages 0 and 1 together before it knows the
// total, and the empty page 1 may land first.
func settled(p *Pager) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total >= 0 && len(p.inFlt) == 0
}

var (
	filmsLibrary = jellyfin.Section{Key: "1", Title: "Films", Type: "movie"}
	showsLibrary = jellyfin.Section{Key: "2", Title: "Shows", Type: "show"}
)

// The Collections tab shows only once the library is known to have a
// collection: not while the listing is on its way, not for a library
// without any, and never for a library that cannot hold them.
func TestWallCollectionsTabOnlyWithCollections(t *testing.T) {
	s := newCollectionServer(t)
	a := collectionTestApp(t, s, false)
	now := time.Now()

	cw := len(wallViews) - 2 // Folders, the tab before Collections
	films := NewWallView(a, filmsLibrary, cw)
	if n := films.tabCount(); n != len(wallViews)-1 {
		t.Fatalf("%d tabs before the collections listing landed", n)
	}
	films.tabs = true
	films.Key(input.Event{Key: input.Right}, now)
	if films.view != cw {
		t.Fatal("moved to a Collections tab that is not shown")
	}
	close(s.hold)
	waitUntil(t, "the collections listing", func() bool { return settled(films.collections()) })
	if n := films.tabCount(); n != len(wallViews) {
		t.Fatalf("%d tabs once the library listed two collections", n)
	}
	films.Key(input.Event{Key: input.Right}, now)
	if films.view != len(wallViews)-1 || !wallViews[films.view].collections {
		t.Fatalf("Right from Folders went to view %d", films.view)
	}
	if films.pager != films.collections() || films.pager.Filtered() {
		t.Fatal("the tab does not show the collections listing")
	}
	if it := films.pager.Get(1); it == nil || it.Type != "collection" || it.Title != "Second Set" || it.Thumb != "9020/Primary/c902" {
		t.Fatalf("second collection %+v", it)
	}
	if got := heroFacts(films.pager.Get(1)); len(got) != 1 || got[0] != "3 items" { // Jellyfin does not say what a box set holds (no Subtype)
		t.Fatalf("the band reads %q", got)
	}
	// settings changes drop the listings; the tab in use stays while its
	// count is fetched again
	a.pagers = map[string]*Pager{}
	if films.tabCount() != len(wallViews) {
		t.Fatal("the Collections tab vanished while it was the view")
	}

	// box sets are the server's: a show library offers the same ones
	shows := NewWall(a, showsLibrary)
	waitUntil(t, "the collections listing", func() bool { return settled(shows.collections()) })
	if n := shows.tabCount(); n != len(wallViews) {
		t.Fatalf("a show library shows %d tabs", n)
	}

	before := s.asked("collections")
	other := NewWall(a, jellyfin.Section{Key: "3", Title: "Clips", Type: "artist"})
	if n := other.tabCount(); n != len(wallViews)-1 || s.asked("collections") != before {
		t.Fatal("a library that cannot hold collections was asked for them")
	}
}

// A collections listing that failed is asked again while the library is
// on screen, spaced out, so the tab still appears once the server answers.
func TestWallCollectionsRetryAfterFailure(t *testing.T) {
	s := newCollectionServer(t)
	close(s.hold)
	s.mu.Lock()
	s.failing = true
	s.mu.Unlock()
	a := collectionTestApp(t, s, false)
	const path = "collections"
	films := NewWall(a, filmsLibrary)
	p := films.collections()
	idle := func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.inFlt) == 0
	}
	waitUntil(t, "the failed listing", func() bool { return p.Err() != nil && idle() })
	if films.tabCount() != len(wallViews)-1 {
		t.Fatal("a failed listing showed the tab")
	}
	now := time.Now()
	asked := s.asked(path)
	films.retryCollections(now)
	waitUntil(t, "the first retry", func() bool { return s.asked(path) > asked && idle() })
	asked = s.asked(path)
	films.retryCollections(now.Add(CollectionsRetry - time.Second))
	if !idle() || s.asked(path) != asked {
		t.Fatal("asked again before the retry interval")
	}
	s.mu.Lock()
	s.failing = false
	s.mu.Unlock()
	films.retryCollections(now.Add(CollectionsRetry))
	waitUntil(t, "the recovered listing", func() bool { return p.Total() >= 0 })
	if films.tabCount() != len(wallViews) {
		t.Fatal("the tab did not appear once the server answered")
	}
	waitUntil(t, "the retry to settle", idle)
	asked = s.asked(path)
	films.retryCollections(now.Add(3 * CollectionsRetry))
	if !idle() || s.asked(path) != asked {
		t.Fatal("a listing that landed was asked for again")
	}
}

// All six tabs fit inside the frame's safe area at 720x480, before the
// scrollbar at its right edge; a collection's single tab is cut to the
// posters' width however long its title.
func TestWallTabsFit(t *testing.T) {
	s := newCollectionServer(t)
	a := collectionTestApp(t, s, false)
	w := NewWallView(a, filmsLibrary, len(wallViews)-1)
	right := func(tabs int) int {
		c := gfx.NewCanvas(720, 480)
		w.compose(c, nil, 0, tabs)
		edge := -1
		for y := WallTabsY; y < WallTabsY+a.F.SmallBold.Height()+8; y++ {
			for x := 0; x < c.W; x++ {
				o := (y*c.W + x) * 4
				if string(c.Pix[o:o+4]) != string(c.Pix[y*c.W*4:y*c.W*4+4]) && x > edge {
					edge = x
				}
			}
		}
		return edge
	}
	four, five := right(len(wallViews)-1), right(len(wallViews))
	if five <= four+20 {
		t.Fatalf("the fifth tab was not drawn: %d then %d", four, five)
	}
	if five >= WallBarX {
		t.Fatalf("the tabs run to x=%d, into the scrollbar at %d", five, WallBarX)
	}

	coll := NewCollectionWall(a, filmsLibrary, &jellyfin.Item{ID: "902", Type: "collection", Title: strings.Repeat("A very long collection title ", 8)})
	w = coll
	if edge := right(coll.tabCount()); edge >= SafeX+WallBandW {
		t.Fatalf("the collection's tab runs to x=%d, past the posters at %d", edge, SafeX+WallBandW)
	}
}

// OK on a collection opens its items as a wall, in the collection's order
// and through the 4:3 filter; an item opens as anywhere else, and Back
// comes out to the Collections tab on the same collection.
func TestWallOpensCollection(t *testing.T) {
	s := newCollectionServer(t)
	close(s.hold)
	a := collectionTestApp(t, s, true)
	now := time.Now()
	library := NewWallView(a, filmsLibrary, len(wallViews)-1)
	a.Push(library)
	waitUntil(t, "the collections", func() bool { return library.pager.Done() && library.total() == 2 }) // both hold a 4:3 film
	a.key(input.Event{Key: input.Right}, now)
	a.key(input.Event{Key: input.Enter}, now)
	coll, ok := a.top().(*Wall)
	if !ok || coll == library || coll.coll == nil || coll.coll.ID != "9020" {
		t.Fatalf("OK on the second collection opened %T", a.top())
	}
	waitUntil(t, "the collection's items", func() bool { return coll.pager.Done() && coll.total() >= 0 })
	if !coll.pager.Filtered() || coll.total() != 2 {
		t.Fatalf("the 4:3 filter kept %d of the collection's items", coll.total())
	}
	if first, second := coll.pager.Get(0), coll.pager.Get(1); first.ID != "23" || second.ID != "22" {
		t.Fatalf("items %s, %s: not the collection's order less the wide film", first.ID, second.ID)
	}
	if s.asked("parent:9020") == 0 {
		t.Fatal("the collection's items were not asked for at the listing the server gave")
	}
	if coll.tabCount() != 1 || coll.az() {
		t.Fatal("a collection has its title as its only tab, with no letters")
	}
	c := gfx.NewCanvas(720, 480)
	coll.Draw(c, now) // the page composes with the collection's tab

	// Up from the first row has no tabs to go to
	a.key(input.Event{Key: input.Up}, now)
	if coll.tabs {
		t.Fatal("Up put a collection on tabs it does not have")
	}
	a.key(input.Event{Key: input.Enter, Keyboard: true, ScanCode: 0x29}, now) // Space is OK in the menus
	if p, ok := a.top().(*Preplay); !ok || p.item.ID != "23" {
		t.Fatalf("OK on a film in the collection opened %T", a.top())
	}
	a.key(input.Event{Key: input.Back}, now)
	if a.top() != coll {
		t.Fatal("Back from the film did not return to the collection")
	}
	a.key(input.Event{Key: input.Back}, now)
	if a.top() != library {
		t.Fatalf("one Back from the collection went to %T", a.top())
	}
	if library.view != len(wallViews)-1 || library.cur != 1 || library.tabs {
		t.Fatalf("returned to view %d, item %d, tabs %v", library.view, library.cur, library.tabs)
	}

}

// Under the 4:3 filter a collection holding nothing it keeps is left out
// of the Collections tab; one with an item it cannot judge stays. Without
// the filter every collection shows.
func TestWallCollectionsFilteredEmptyHidden(t *testing.T) {
	s := newCollectionServer(t)
	close(s.hold)
	s.mu.Lock()
	s.wideFirst = true
	s.mu.Unlock()
	a := collectionTestApp(t, s, true)
	films := NewWallView(a, filmsLibrary, len(wallViews)-1)
	waitUntil(t, "the filtered collections", func() bool { return films.pager.Done() && films.total() >= 0 })
	if films.pager != films.collections() || films.total() != 1 {
		t.Fatalf("%d collections under the filter, want the second set only", films.total())
	}
	if it := films.pager.Get(0); it == nil || it.ID != "9020" {
		t.Fatalf("kept %+v", it)
	}
	if s.asked("parent:901") == 0 {
		t.Fatal("the first set's items were not looked into")
	}
	if n := films.tabCount(); n != len(wallViews) {
		t.Fatalf("%d tabs with a collection left", n)
	}

	a.Cfg.FourThree = false
	films = NewWallView(a, filmsLibrary, len(wallViews)-1)
	waitUntil(t, "the collections", func() bool { return films.total() == 2 })
	if films.pager.Filtered() {
		t.Fatal("the collections were filtered with the filter off")
	}
}

// Drawing asks on every frame whether the Collections tab shows; once the
// listing is in, that costs no allocations.
func TestWallTabCheckDoesNotAllocate(t *testing.T) {
	s := newCollectionServer(t)
	close(s.hold)
	a := collectionTestApp(t, s, false)
	now := time.Now()
	for _, lib := range []jellyfin.Section{filmsLibrary, showsLibrary} {
		w := NewWall(a, lib)
		waitUntil(t, "the collections", func() bool { return w.collections().Total() >= 0 })
		if n := testing.AllocsPerRun(100, func() {
			if w.tabCount() < len(wallViews) {
				w.retryCollections(now)
			}
		}); n != 0 {
			t.Fatalf("%s: %v allocations per frame", lib.Title, n)
		}
	}
}
