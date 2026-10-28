package jellyfin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// versionsRecord is a synthetic movie's record in two versions, the copy
// first (the server lists the last one played first): a 720p copy, which
// has preview sheets, and the 1080p original whose source id is the item's.
const versionsRecord = `{"Id":"film","Type":"Movie","Name":"Film","RunTimeTicks":10000000000,
"UserData":{"PlaybackPositionTicks":500000000},
"Trickplay":{"film-720":{"320":{"Width":320,"Height":180,"TileWidth":10,"TileHeight":10,"ThumbnailCount":50,"Interval":10000}}},
"MediaSources":[
 {"Id":"film-720","Name":"720p","Path":"/media/Film (2001)/Film (2001) - 720p.mkv","Container":"mkv","Bitrate":1200000,"MediaStreams":[
  {"Type":"Video","Index":0,"Codec":"h264","Width":1280,"Height":720,"AspectRatio":"16:9"},
  {"Type":"Audio","Index":1,"Codec":"aac","Channels":2,"IsDefault":true,"Language":"eng","DisplayTitle":"English"}]},
 {"Id":"film","Name":"Film (2001)","Path":"/media/Film (2001)/Film (2001).mkv","Container":"mkv","Bitrate":7800000,"MediaStreams":[
  {"Type":"Video","Index":0,"Codec":"h264","Width":1920,"Height":800,"AspectRatio":"2.40:1"},
  {"Type":"Audio","Index":1,"Codec":"ac3","Channels":6,"IsDefault":true,"Language":"eng","DisplayTitle":"English 5.1"},
  {"Type":"Subtitle","Index":2,"Codec":"subrip","Language":"fre","DisplayTitle":"French","IsTextSubtitleStream":true,"SupportsExternalStream":true}]}]}`

func decodeItem(t *testing.T, data string) *Item {
	t.Helper()
	var x dto
	if err := json.Unmarshal([]byte(data), &x); err != nil {
		t.Fatal(err)
	}
	return x.item()
}

// An item's own record lists its versions in the server's order, the first
// in force; a listing's only counts them.
func TestItemVersions(t *testing.T) {
	it := decodeItem(t, versionsRecord)
	if len(it.Versions) != 2 || it.SourceID != "film-720" || it.VersionID != "" || it.Version().ID != "film-720" {
		t.Fatalf("versions %+v, source %q", it.Versions, it.SourceID)
	}
	v := it.Versions[1]
	if v.Label != "Film (2001)" || v.File != "Film (2001).mkv" || v.Codec != "h264" || v.Width != 1920 || v.Offset != -1 ||
		v.Source.Bitrate != 7800000 || len(v.Audio) != 1 || len(v.Subs) != 1 || v.Subs[0].Language != "fre" || v.Trickplay != nil {
		t.Fatalf("original %+v", v)
	}
	if it.Versions[0].Trickplay == nil || it.Trickplay == nil {
		t.Fatal("the copy's preview sheets are missing")
	}
	if !it.UseVersion("film") || it.SourceID != "film" || len(it.Subs) != 1 || it.Trickplay != nil || it.Aspect != 2.4 ||
		len(it.SourceStreams) != 3 || it.ViewOffset != 50 {
		t.Fatalf("after UseVersion: %+v", it)
	}
	if it.UseVersion("") || it.UseVersion("other") {
		t.Fatal("UseVersion took a version the item lacks")
	}
	listed := decodeItem(t, `{"Id":"film","Type":"Movie","Name":"Film","MediaSourceCount":2}`)
	if len(listed.Versions) != 2 || listed.Versions[0].Detailed() || VersionIndex(listed.Versions, "") >= 0 {
		t.Fatalf("listing's versions %+v", listed.Versions)
	}
	if one := decodeItem(t, `{"Id":"x","Type":"Movie","MediaSources":[{"Id":"x"}]}`); one.Versions != nil {
		t.Fatalf("one version: %+v", one.Versions)
	}
}

func TestVersionLabels(t *testing.T) {
	it := decodeItem(t, versionsRecord)
	plays := func(v Version) bool { return v.Width <= 1280 }
	got := VersionLabels(it.Versions, plays)
	if got[0] != "720p H.264, 1.2 Mbps, plays directly" || got[1] != "1080p H.264, 7.8 Mbps, server converts" {
		t.Fatalf("labels %q", got)
	}
	// the same picture twice: told apart by the files' own parts, or the
	// server's names without paths
	same := []Version{{ID: "a", Codec: "h264", Width: 1280, Height: 720, File: "Film (2001) - Director's Cut.mkv", Label: "Director's Cut"},
		{ID: "b", Codec: "h264", Width: 1280, Height: 720, File: "Film (2001).mkv", Label: "Film (2001)"}}
	if names := VersionNames(same); names[0] != "720p H.264 - Director's Cut" || names[1] != "720p H.264 - version 2" {
		t.Fatalf("names %q", names)
	}
	same[0].File, same[1].File = "", ""
	if names := VersionNames(same); names[0] != "720p H.264 - Director's Cut" || names[1] != "720p H.264 - Film (2001)" {
		t.Fatalf("names without paths %q", names)
	}
}

func TestPickAndMatchVersion(t *testing.T) {
	vs := []Version{
		{ID: "1080", Codec: "h264", Width: 1920, Height: 1080, Source: MediaSource{Bitrate: 7_800_000}},
		{ID: "720", Codec: "h264", Width: 1280, Height: 720, Source: MediaSource{Bitrate: 1_200_000}},
		{ID: "480", Codec: "h264", Width: 854, Height: 480, Source: MediaSource{Bitrate: 700_000}},
	}
	direct := func(v Version) bool { return v.Width <= 1280 }
	none := func(Version) bool { return false }
	if i := PickVersion(vs, direct); vs[i].ID != "720" {
		t.Fatalf("direct pick %s", vs[i].ID)
	}
	if i := PickVersion(vs, none); vs[i].ID != "720" {
		t.Fatalf("converted pick %s, want the smallest of 720 lines", vs[i].ID)
	}
	if i := PickVersion(vs[2:], none); i != 0 {
		t.Fatalf("pick among small ones %d", i)
	}
	if i := PickVersion([]Version{{}, {}}, direct); i != 0 {
		t.Fatal("blank versions were judged")
	}
	if MatchVersion(vs, "480p h264") != 2 || MatchVersion(vs, "") != -1 || MatchVersion(vs, "4K hevc") != -1 {
		t.Fatal("MatchVersion")
	}
	vs[2].Offset = 30
	if ResumeVersion(vs) != 2 {
		t.Fatal("ResumeVersion")
	}
}

// From 12 the record's versions carry their own resume points, read once
// each, and the item's is the first version's; before 12 nothing is read.
func TestItemReadsEachVersionsResumePoint(t *testing.T) {
	for _, server := range []string{"12.4.0", "10.11.0"} {
		var mu sync.Mutex
		var asked []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			asked = append(asked, r.URL.Path)
			mu.Unlock()
			switch {
			case r.URL.Path == "/System/Info/Public":
				fmt.Fprintf(w, `{"Id":"server","ServerName":"Test","Version":%q}`, server)
			case r.URL.Path == "/Items/film":
				_, _ = w.Write([]byte(versionsRecord))
			case r.URL.Path == "/UserItems/film-720/UserData":
				_, _ = w.Write([]byte(`{"PlaybackPositionTicks":1200000000}`))
			case strings.HasPrefix(r.URL.Path, "/MediaSegments/"):
				_, _ = w.Write([]byte(`{"Items":[]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		store := &MemoryStore{}
		store.SetCredentials(Credentials{UserID: "user", Token: "synthetic-token"})
		c := testClient(srv.URL, "", "device", store)
		c.SetHTTPClient(srv.Client())
		it, err := c.Item(t.Context(), "film")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Item(t.Context(), "film"); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		mu.Lock()
		joined := strings.Join(asked, " ")
		mu.Unlock()
		if server == "12.4.0" {
			if it.Versions[0].Offset != 120 || it.Versions[1].Offset != 50 || it.ViewOffset != 120 {
				t.Fatalf("12: offsets %d, %d, item %d", it.Versions[0].Offset, it.Versions[1].Offset, it.ViewOffset)
			}
			if strings.Count(joined, "/System/Info/Public") != 1 || strings.Count(joined, "/UserData") != 2 {
				t.Fatalf("12: asked %s", joined)
			}
			continue
		}
		if it.Versions[0].Offset != -1 || it.ViewOffset != 50 || strings.Contains(joined, "/UserData") {
			t.Fatalf("%s: offset %d, item %d, asked %s", server, it.Versions[0].Offset, it.ViewOffset, joined)
		}
	}
}
