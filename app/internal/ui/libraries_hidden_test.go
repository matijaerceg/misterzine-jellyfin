package ui

import (
	"testing"

	"plexcrt/internal/jellyfin"
)

// Hiding a library is app-wide: its items leave Continue Watching too.
func TestContinueWatchingLeavesOutHiddenLibraries(t *testing.T) {
	a := betaTestApp(t)
	a.Client = &jellyfin.Client{}
	a.secs = testLibraries
	a.Cfg.SetLibraryHidden("2", true)
	h := &Home{app: a, home: true}
	row := homeRow("home.continue", "c1", "c2", "c3")
	row.Items[0].Library = "1"
	row.Items[1].Library = "2"
	// an item whose library the server did not name stays
	h.applyHome([]*jellyfin.Hub{row})
	if len(h.hubs) != 1 || len(h.hubs[0].Items) != 2 {
		t.Fatalf("Continue Watching after hiding library 2: %+v", h.hubs)
	}
	for _, it := range h.hubs[0].Items {
		if it.Library == "2" {
			t.Fatal("a hidden library's item stayed in Continue Watching")
		}
	}
}
