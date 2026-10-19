package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"plexcrt/internal/jellyfin"
)

// Shows are judged by a first episode: one sweep request per TV library
// answers for most, the rest are probed once, and everything is remembered
// on disk. An unknown show stays visible.
func TestShowsAreMeasuredOnceForTheFilter(t *testing.T) {
	var sweeps, probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/UserViews":
			fmt.Fprint(w, `{"Items":[{"Id":"2","Name":"TV","CollectionType":"tvshows"},{"Id":"3","Name":"Films","CollectionType":"movies"}]}`)
		case r.URL.Path == "/Items" && q.Get("parentId") == "2" && q.Get("includeItemTypes") == "Episode":
			sweeps.Add(1)
			if q.Get("indexNumber") != "1" {
				t.Errorf("sweep should ask for first episodes: %s", r.URL)
			}
			// the wide show's season 2 comes first; season 1 must win
			fmt.Fprint(w, `{"Items":[`+
				`{"Id":"201","Type":"Episode","SeriesId":"16","ParentIndexNumber":2,"Width":640,"Height":480},`+
				`{"Id":"101","Type":"Episode","SeriesId":"16","ParentIndexNumber":1,"Width":1920,"Height":1080},`+
				`{"Id":"102","Type":"Episode","SeriesId":"43","ParentIndexNumber":1,"Width":640,"Height":480}],"TotalRecordCount":3}`)
		case strings.HasPrefix(r.URL.Path, "/Shows/") && strings.HasSuffix(r.URL.Path, "/Episodes"):
			probes.Add(1)
			if q.Get("limit") != "1" {
				t.Errorf("probe should ask for one episode: %s", r.URL)
			}
			fmt.Fprint(w, `{"Items":[{"Id":"9","Type":"Episode","Name":"Special","Width":1920,"Height":817}],"TotalRecordCount":1}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	a := &App{Client: testClient(server.URL, "", dir), Cfg: &Config{FourThree: true}}
	a.SetCacheDir(dir)
	wide := &jellyfin.Item{ID: "16", Type: "show", Title: "Wide"}
	square := &jellyfin.Item{ID: "43", Type: "show", Title: "Square"}
	odd := &jellyfin.Item{ID: "77", Type: "show", Title: "Specials only"}
	movie := &jellyfin.Item{ID: "7", Type: "movie", Aspect: 2.35}
	items := []*jellyfin.Item{wide, square, odd, movie}
	if !a.Keep(wide) || !a.Keep(square) || !a.Keep(odd) {
		t.Fatal("an unmeasured show must stay visible")
	}
	a.measureShows(items)
	if sweeps.Load() != 1 || probes.Load() != 1 {
		t.Fatalf("sweeps %d probes %d, want one sweep and one probe for the show it missed", sweeps.Load(), probes.Load())
	}
	if a.Keep(wide) || !a.Keep(square) || a.Keep(odd) || a.Keep(movie) {
		t.Fatal("filter did not use the measured shapes")
	}
	if wide.Aspect != 1.78 {
		t.Fatalf("season 1 should decide a show, got %v", wide.Aspect)
	}

	// a fresh app on the same cache folder knows the answers without asking
	b := &App{Client: testClient(server.URL, "", dir), Cfg: &Config{FourThree: true}}
	b.SetCacheDir(dir)
	again := []*jellyfin.Item{{ID: "16", Type: "show"}, {ID: "43", Type: "show"}, {ID: "77", Type: "show"}}
	b.measureShows(again)
	if sweeps.Load() != 1 || probes.Load() != 1 {
		t.Fatal("remembered shows were asked about again")
	}
	if b.Keep(again[0]) || !b.Keep(again[1]) || b.Keep(again[2]) {
		t.Fatal("remembered shapes were not applied")
	}

	// with the filter off nothing is asked
	c := &App{Client: testClient(server.URL, "", t.TempDir()), Cfg: &Config{}}
	c.SetCacheDir(t.TempDir())
	c.measureShows([]*jellyfin.Item{{ID: "99", Type: "show"}, {ID: "season-99", Type: "season", SeriesID: "99", ParentID: "99"}})
	if sweeps.Load() != 1 || probes.Load() != 1 {
		t.Fatal("asked with the filter off")
	}
}

func TestSeasonAspectUsesSeriesIDAndPersists(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/UserViews":
			fmt.Fprint(w, `{"Items":[{"Id":"2","Name":"TV","CollectionType":"tvshows"}]}`)
		case r.URL.Path == "/Items" && q.Get("parentId") == "2":
			fmt.Fprint(w, `{"Items":[{"Id":"episode","Type":"Episode","SeriesId":"series-1","ParentIndexNumber":1,"Width":1920,"Height":1080}],"TotalRecordCount":1}`)
		case strings.HasPrefix(r.URL.Path, "/Shows/") && strings.HasSuffix(r.URL.Path, "/Episodes"):
			probes.Add(1)
			t.Errorf("season with a swept series aspect should not be probed: %s", r.URL)
			fmt.Fprint(w, `{"Items":[]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	a := &App{Client: testClient(server.URL, "", dir), Cfg: &Config{FourThree: true}}
	a.SetCacheDir(dir)
	wide := &jellyfin.Item{ID: "season-1", Type: "season", SeriesID: "series-1", ParentID: "series-1"}
	narrow := &jellyfin.Item{ID: "season-2", Type: "season", SeriesID: "series-2", ParentID: "series-2"}
	// The sweep only knows the first show; seed a narrow result through the
	// same series-keyed cache before measuring both seasons.
	a.aspects.set("series-2", 1.33)
	a.measureShows([]*jellyfin.Item{wide, narrow})
	if a.Keep(wide) || !a.Keep(narrow) {
		t.Fatalf("season filter kept wide=%v, narrow=%v; want wide hidden and narrow visible", a.Keep(wide), a.Keep(narrow))
	}
	if probes.Load() != 0 {
		t.Fatalf("got %d season probes, want none", probes.Load())
	}

	// A fresh cache load must restore the swept answer under the show's id.
	b := &App{Cfg: &Config{FourThree: true}}
	b.SetCacheDir(dir)
	fromCache := &jellyfin.Item{ID: "season-again", Type: "season", SeriesID: "series-1", ParentID: "series-1"}
	if unknown := b.aspects.apply([]*jellyfin.Item{fromCache}); len(unknown) != 0 || fromCache.Aspect != 1.78 {
		t.Fatalf("cached series aspect was not applied to its season: aspect=%v unknown=%d", fromCache.Aspect, len(unknown))
	}
	if _, ok := b.aspects.known["season-1"]; ok {
		t.Fatal("season cache entry used the season id")
	}
}

func TestSeasonAspectFallsBackToParentIDAndMissingParentStaysVisible(t *testing.T) {
	var probed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/UserViews":
			fmt.Fprint(w, `{"Items":[]}`)
		case r.URL.Path == "/Shows/parent-show/Episodes":
			probed.Add(1)
			if r.URL.Query().Get("limit") != "1" {
				t.Errorf("probe should ask for one episode: %s", r.URL)
			}
			fmt.Fprint(w, `{"Items":[{"Id":"ep","Type":"Episode","Width":640,"Height":480}],"TotalRecordCount":1}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	a := &App{Client: testClient(server.URL, "", dir), Cfg: &Config{FourThree: true}}
	a.SetCacheDir(dir)
	fallback := &jellyfin.Item{ID: "season-fallback", Type: "season", ParentID: "parent-show"}
	missing := &jellyfin.Item{ID: "season-missing", Type: "season"}
	a.measureShows([]*jellyfin.Item{fallback, missing})
	if probed.Load() != 1 {
		t.Fatalf("got %d probes, want only the season with a parent id", probed.Load())
	}
	if !a.Keep(fallback) || !a.Keep(missing) {
		t.Fatal("narrow or unmeasured season should stay visible")
	}
	if _, ok := a.aspects.known[""]; ok {
		t.Fatal("missing parent was stored under an empty key")
	}
}

func TestSeasonAspectPreservesExistingValue(t *testing.T) {
	a := &App{Cfg: &Config{FourThree: true}}
	a.aspects.known = map[string]float64{"series": 1.78}
	season := &jellyfin.Item{ID: "season", Type: "season", SeriesID: "series", ParentID: "series", Aspect: 1.33}
	if unknown := a.aspects.apply([]*jellyfin.Item{season}); len(unknown) != 0 {
		t.Fatalf("existing season aspect was treated as unknown: %d", len(unknown))
	}
	if season.Aspect != 1.33 {
		t.Fatalf("existing season aspect changed to %v", season.Aspect)
	}
}
