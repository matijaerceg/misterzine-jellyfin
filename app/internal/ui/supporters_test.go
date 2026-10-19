package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	SupportersURL = "" // no test reaches the site; the ones that fetch serve their own
	os.Exit(m.Run())
}

const supportersFixture = `{"updated":"2026-09-28",
 "current":[{"name":"Alex Example","since":"2026-09"},{"name":"  "},{"name":"Zoë  Émile"}],
 "past":[{"name":"Sam Sample","since":"2026-08","until":"2026-09"}],
 "early_adopters":["Pat Tester"]}`

func TestDecodeSupporters(t *testing.T) {
	s, err := DecodeSupporters([]byte(supportersFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Current) != 2 || s.Current[0].Name != "Alex Example" || s.Current[1].Name != "Zoe Emile" {
		t.Fatalf("current: %+v", s.Current)
	}
	if len(s.Past) != 1 || s.Past[0].Name != "Sam Sample" {
		t.Fatalf("past: %+v", s.Past)
	}
	for _, reply := range []string{"<html>not found</html>", `{}`, `{"error":"temporary"}`, `{"current":null,"past":[]}`} {
		if _, err := DecodeSupporters([]byte(reply)); err == nil {
			t.Fatalf("%s was taken for a list", reply)
		}
	}
	if s, err := DecodeSupporters([]byte(`{"current":[]}`)); err != nil || len(s.Current) != 0 {
		t.Fatalf("an empty list: %+v, %v", s, err)
	}
}

func TestErrorReplyKeepsTheListAndItsCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer srv.Close()
	t.Cleanup(func() { SupportersURL = "" })
	SupportersURL = srv.URL
	cache := t.TempDir()
	path := filepath.Join(cache, "supporters.json")
	if err := os.WriteFile(path, []byte(supportersFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.SetCacheDir(cache)
	now := time.Now()
	a.checkSupporters(now)
	settleSupporters(t, a, now) // the cache, then the reply
	if n := len(a.supporters.list.Current); n != 2 {
		t.Fatalf("%d current supporters after an error reply", n)
	}
	if b, _ := os.ReadFile(path); string(b) != supportersFixture {
		t.Fatalf("the cache now holds %q", b)
	}
}

func TestSupportersChecksNeverOverlapAndAlwaysLand(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release // a slow site: the check stays out
		w.Write([]byte(supportersFixture))
	}))
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	defer srv.Close()
	defer free() // before Close, which waits for the handler
	t.Cleanup(func() { SupportersURL = "" })
	SupportersURL = srv.URL
	a := &App{later: make(chan func())} // Later's queue is always full: nothing may rely on it
	now := time.Now()
	a.checkSupporters(now)
	for end := time.Now().Add(5 * time.Second); hits.Load() == 0 && time.Now().Before(end); {
		time.Sleep(5 * time.Millisecond)
	}
	for _, later := range []time.Duration{time.Minute, time.Hour, 7 * time.Hour} {
		a.checkSupporters(now.Add(later))
	}
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d fetches while the first was out", n)
	}
	free()
	settleSupporters(t, a, now)
	if n := len(a.supporters.list.Current); n != 2 || hits.Load() != 1 {
		t.Fatalf("%d supporters after %d fetches", n, hits.Load())
	}
}

// settleSupporters plays the render thread until the check that is out
// has finished and its result has been taken up.
func settleSupporters(t *testing.T, a *App, now time.Time) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		a.checkSupporters(now)
		if !a.supporters.checking {
			return
		}
		if time.Now().After(end) {
			t.Fatal("the check did not finish")
		}
	}
}

func TestSupportersFetchedCachedAndKeptOffline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(supportersFixture))
	}))
	t.Cleanup(func() { SupportersURL = "" })
	SupportersURL = srv.URL + "/supporters.json"
	cache := t.TempDir()
	a := &App{}
	a.SetCacheDir(cache)
	now := time.Now()
	a.checkSupporters(now)
	settleSupporters(t, a, now) // no cache yet: the fetch only
	if n := len(a.supporters.list.Current); n != 2 {
		t.Fatalf("%d current supporters after the fetch", n)
	}
	if _, err := os.Stat(filepath.Join(cache, "supporters.json")); err != nil {
		t.Fatalf("the list was not cached: %v", err)
	}
	a.checkSupporters(now.Add(time.Hour))
	if a.supporters.checking {
		t.Fatal("checked again within six hours")
	}

	srv.Close() // offline: the next run shows the cached list
	b := &App{}
	b.SetCacheDir(cache)
	b.checkSupporters(now)
	settleSupporters(t, b, now) // the cache, then the failed fetch
	if n := len(b.supporters.list.Current); n != 2 {
		t.Fatalf("%d current supporters offline", n)
	}
	if !b.supporters.nextCheck.Before(time.Now().Add(10 * time.Minute)) {
		t.Fatal("a failed fetch waits hours before the next one")
	}
}
