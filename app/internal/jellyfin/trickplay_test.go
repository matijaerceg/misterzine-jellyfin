package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestItemTrickplayPicksTheSourceAndSmallestWidth(t *testing.T) {
	const body = `{"Id":"item-1","Type":"Movie","MediaSources":[{"Id":"source-1"}],
		"Trickplay":{"source-1":{
			"640":{"Width":640,"Height":360,"TileWidth":10,"TileHeight":10,"ThumbnailCount":122,"Interval":10000},
			"320":{"Width":320,"Height":180,"TileWidth":10,"TileHeight":10,"ThumbnailCount":122,"Interval":10000,"Bandwidth":1}},
		"other":{"160":{"Width":160,"Height":90,"TileWidth":10,"TileHeight":10,"ThumbnailCount":1,"Interval":10000}}}}`
	var x dto
	if err := json.Unmarshal([]byte(body), &x); err != nil {
		t.Fatal(err)
	}
	got := x.item().Trickplay
	want := Trickplay{Width: 320, Height: 180, TileWidth: 10, TileHeight: 10, Count: 122, Interval: 10000}
	if got == nil || *got != want {
		t.Fatalf("trickplay = %+v, want %+v", got, want)
	}
}

func TestItemTrickplayAbsentOrBroken(t *testing.T) {
	for _, body := range []string{
		`{"Id":"item-1","Type":"Movie","MediaSources":[{"Id":"source-1"}]}`,
		`{"Id":"item-1","Type":"Movie","MediaSources":[{"Id":"source-1"}],"Trickplay":{"source-1":{"320":{"Width":320,"Height":180,"TileWidth":0,"TileHeight":10,"ThumbnailCount":5,"Interval":10000}}}}`,
		`{"Id":"item-1","Type":"Series","Trickplay":{"source-1":{"320":{"Width":320,"Height":180,"TileWidth":10,"TileHeight":10,"ThumbnailCount":5,"Interval":10000}}}}`,
	} {
		var x dto
		if err := json.Unmarshal([]byte(body), &x); err != nil {
			t.Fatal(err)
		}
		if tp := x.item().Trickplay; tp != nil {
			t.Fatalf("%s: trickplay = %+v", body, tp)
		}
	}
}

func TestTrickplaySheetRequest(t *testing.T) {
	var path, query, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		w.Write([]byte("jpeg"))
	}))
	defer srv.Close()
	store := &MemoryStore{}
	store.SetCredentials(Credentials{Token: "TESTTOKEN", UserID: "user-1"})
	c := New(srv.URL, t.TempDir(), "dev-1", store)
	data, err := c.TrickplaySheet(context.Background(), "item-1", "source-1", 320, 1)
	if err != nil || string(data) != "jpeg" {
		t.Fatalf("sheet = %q, %v", data, err)
	}
	if path != "/Videos/item-1/Trickplay/320/1.jpg" || query != "mediaSourceId=source-1" {
		t.Fatalf("request %s?%s", path, query)
	}
	if strings.Contains(query, "TESTTOKEN") || !strings.Contains(auth, `Token="TESTTOKEN"`) {
		t.Fatalf("token travels in the header only: %q %q", query, auth)
	}
}
