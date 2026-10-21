package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The fixtures under testdata were captured from a Jellyfin 12.2 server and
// scrubbed: server URL, user id and tokens replaced, a few items per
// listing kept. resume.json is built from captured episode and movie
// records with a resume point set (the server had nothing in progress).

const testUser = "0123456789abcdef0123456789abcdef"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fake struct {
	t    *testing.T
	mu   sync.Mutex
	reqs []*http.Request
	body map[string][]byte // request path -> body read
}

func (f *fake) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := io.ReadAll(r.Body)
	f.reqs = append(f.reqs, r)
	if f.body == nil {
		f.body = map[string][]byte{}
	}
	f.body[r.Method+" "+r.URL.Path] = b
}

// newFake serves the fixtures by path; a request without the token gets 401,
// except the endpoints that need none.
func newFake(t *testing.T) (*fake, *Client) {
	f := &fake{t: t}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if strings.Contains(r.URL.RawQuery, "TESTTOKEN") || strings.Contains(strings.ToLower(r.URL.RawQuery), "api_key") {
			t.Errorf("token in URL: %s", r.URL)
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, `MediaBrowser Client="MisterZine", Device="MiSTer CRT", DeviceId="dev-1", Version="`) {
			t.Errorf("Authorization header %q", auth)
		}
		open := map[string]bool{"/System/Info/Public": true, "/Users/AuthenticateByName": true, "/QuickConnect/Connect": true, "/Users/Public": true}
		if !open[r.URL.Path] && !strings.HasSuffix(auth, `Token="TESTTOKEN"`) {
			w.WriteHeader(401)
			return
		}
		q := r.URL.Query()
		serve := func(name string) { w.Header().Set("Content-Type", "application/json"); w.Write(fixture(t, name)) }
		switch {
		case r.URL.Path == "/System/Info/Public":
			serve("public.json")
		case r.URL.Path == "/Users/AuthenticateByName":
			serve("auth.json")
		case r.URL.Path == "/QuickConnect/Connect":
			serve("qc_connect.json")
		case r.URL.Path == "/Users/Public":
			io.WriteString(w, publicUsers)
		case r.URL.Path == "/UserViews":
			serve("views.json")
		case r.URL.Path == "/Items" && q.Get("searchTerm") != "":
			serve("search.json")
		case r.URL.Path == "/Items" && q.Get("includeItemTypes") == "Series":
			serve("shows.json")
		case r.URL.Path == "/Items" && q.Get("startIndex") == "3":
			serve("movies_p1.json")
		case r.URL.Path == "/Items":
			serve("movies_p0.json")
		case r.URL.Path == "/Items/358f1b71dbd1eb6238744f21a9a01f14":
			serve("item.json")
		case r.URL.Path == "/MediaSegments/358f1b71dbd1eb6238744f21a9a01f14":
			w.Write([]byte(`{"Items":[{"Type":"Intro","StartTicks":0,"EndTicks":600000000},{"Type":"Recap","StartTicks":1,"EndTicks":2},{"Type":"Outro","StartTicks":9000000000,"EndTicks":9538240000}]}`))
		case strings.HasSuffix(r.URL.Path, "/Seasons"):
			serve("seasons.json")
		case strings.HasSuffix(r.URL.Path, "/Episodes"):
			serve("episodes.json")
		case r.URL.Path == "/UserItems/Resume":
			serve("resume.json")
		case r.URL.Path == "/Shows/NextUp":
			// the first is also in progress: Continue Watching holds it once
			var l map[string]any
			json.Unmarshal(fixture(t, "episodes.json"), &l)
			l["Items"] = l["Items"].([]any)[1:3]
			json.NewEncoder(w).Encode(l)
		case r.URL.Path == "/Items/Latest":
			serve("latest.json")
		case strings.HasSuffix(r.URL.Path, "/PlaybackInfo"):
			serve("playbackinfo.json")
		case strings.HasPrefix(r.URL.Path, "/Videos/") && strings.HasSuffix(r.URL.Path, "/Stream.srt"):
			w.Write([]byte("1\r\n00:00:01,000 --> 00:00:02,500\r\nHello\r\n"))
		case strings.HasPrefix(r.URL.Path, "/Sessions/Playing"), strings.HasPrefix(r.URL.Path, "/UserPlayedItems/"),
			strings.HasPrefix(r.URL.Path, "/UserItems/") && strings.HasSuffix(r.URL.Path, "/UserData"):
			w.WriteHeader(204)
		case strings.HasPrefix(r.URL.Path, "/Items/") && strings.Contains(r.URL.Path, "/Images/"):
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8jpeg"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, t.TempDir(), "dev-1", nil)
	c.Backoff = time.Millisecond
	return f, c
}

func signIn(t *testing.T, c *Client) {
	t.Helper()
	if _, err := c.SignIn(context.Background(), "testuser", "pw"); err != nil {
		t.Fatal(err)
	}
}

func TestSignIn(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	info, err := c.PublicInfo(ctx)
	if err != nil || info.Version != "12.2.0" || info.ID == "" {
		t.Fatalf("public info %+v %v", info, err)
	}
	if _, err := c.Sections(ctx); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("before sign-in: %v, want ErrUnauthorized", err)
	}
	cr, err := c.SignIn(ctx, "testuser", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if cr.Token != "TESTTOKEN" || cr.UserID != testUser || cr.UserName != "testuser" || c.Store.Credentials() != cr {
		t.Fatalf("credentials %+v", cr)
	}
	var body map[string]string
	json.Unmarshal(f.body["POST /Users/AuthenticateByName"], &body)
	if body["Username"] != "testuser" || body["Pw"] != "pw" {
		t.Fatalf("sign-in body %v", body)
	}
	if !strings.HasSuffix(c.AuthHeader(), `, Token="TESTTOKEN"`) {
		t.Fatalf("header %q", c.AuthHeader())
	}
	ok, err := c.QuickConnectCheck(ctx, "SECRET")
	if err != nil || ok {
		t.Fatalf("quick connect check %v %v", ok, err)
	}
}

// publicUsers is a synthetic /Users/Public answer: a user with a password,
// one without (her name with an accent, as the server has it), one only
// the field's other name protects, and two broken entries.
const publicUsers = `[
 {"Name":"testuser","Id":"0123456789abcdef0123456789abcdef","PrimaryImageTag":"tag1","HasPassword":true,"HasConfiguredPassword":true},
 {"Name":"Zoë","Id":"11111111111111111111111111111111","HasPassword":false,"HasConfiguredPassword":false},
 {"Name":"guest","Id":"22222222222222222222222222222222","HasConfiguredPassword":true},
 {"Name":" ","Id":"33333333333333333333333333333333"},
 {"Name":"nobody"}
]`

// The users a server lists come without a token, their names as the server
// has them (signing in needs the exact name), the broken entries dropped.
func TestPublicUsers(t *testing.T) {
	_, c := newFake(t)
	users, err := c.PublicUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 || users[0].Name != "testuser" || users[0].ImageTag != "tag1" || users[1].Name != "Zoë" {
		t.Fatalf("users %+v", users)
	}
	if !users[0].Protected() || users[1].Protected() || !users[2].Protected() {
		t.Fatalf("passwords %+v", users)
	}
}

func TestNormalizeHost(t *testing.T) {
	for in, want := range map[string]string{
		"jellyfin.example.com":         "https://jellyfin.example.com",
		"127.0.0.1:8096":               "http://127.0.0.1:8096",
		"http://nas:8096/jellyfin/":    "http://nas:8096/jellyfin",
		" https://x.example/?a=b#frag": "https://x.example",
	} {
		if got, err := NormalizeHost(in); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeHost("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
}

func TestHostCandidates(t *testing.T) {
	for in, want := range map[string]string{
		"jellyfin.example.com":            "https://jellyfin.example.com http://jellyfin.example.com:8096",
		"192.0.2.10/jellyfin/":            "https://192.0.2.10/jellyfin http://192.0.2.10:8096/jellyfin",
		"[2001:db8::1]":                   "https://[2001:db8::1] http://[2001:db8::1]:8096",
		"192.0.2.10:8096":                 "http://192.0.2.10:8096",
		"jellyfin.example.com:8920":       "https://jellyfin.example.com:8920",
		"https://jellyfin.example.com":    "https://jellyfin.example.com",
		"http://jellyfin.example.com":     "http://jellyfin.example.com",
		"http://jellyfin.example.com:80/": "http://jellyfin.example.com:80",
	} {
		got, err := HostCandidates(in)
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := HostCandidates(""); err == nil {
		t.Error("empty accepted")
	}
}

func TestLibraries(t *testing.T) {
	_, c := newFake(t)
	signIn(t, c)
	ctx := context.Background()
	secs, err := c.Sections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Section{{"f137a2dd21bbc1b99aa5c0f6bf02a805", "Movies", "movie"}, {"767bffe4f11c93ef34b805451a696a4e", "TV Shows", "show"}}
	if len(secs) != 2 || secs[0] != want[0] || secs[1] != want[1] {
		t.Fatalf("sections %+v", secs)
	}
	path, q := SectionListing(secs[1].Key, secs[1].Type, ViewAZ)
	shows, total, err := c.Page(ctx, path, q, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if total < 700 || len(shows) != 3 {
		t.Fatalf("%d shows of %d", len(shows), total)
	}
	s := shows[0]
	if s.Type != "show" || s.Title != "3 Body Problem" || s.Year != 2024 || s.Leaves != 8 || s.Viewed != 0 ||
		s.Key != "/Shows/e8597adb9c3c4567dc82aac5981b22ac/Seasons" || s.SortLabel() != "3 body problem" {
		t.Fatalf("show %+v", s)
	}
	if s.Thumb != "e8597adb9c3c4567dc82aac5981b22ac/Primary/c98cfdc0a2362738e4cce2a624c99e03" ||
		!strings.Contains(s.Logo, "/Logo/") || !strings.Contains(s.Art, "/Backdrop/") || s.Colors == [4]uint32{} {
		t.Fatalf("show art %q %q %q %x", s.Thumb, s.Logo, s.Art, s.Colors)
	}
	seasons, err := c.Items(ctx, s.Key, nil, 500)
	if err != nil || len(seasons) != 1 {
		t.Fatalf("seasons %v %v", seasons, err)
	}
	se := seasons[0]
	if se.Type != "season" || se.Index != 1 || se.Leaves != 8 || se.ParentID != s.ID ||
		se.Key != "/Shows/"+s.ID+"/Episodes?seasonId="+se.ID {
		t.Fatalf("season %+v", se)
	}
	eps, err := c.Items(ctx, se.Key, nil, 500)
	if err != nil || len(eps) != 3 {
		t.Fatalf("episodes %d %v", len(eps), err)
	}
	e := eps[0]
	if e.Type != "episode" || e.Label() != "S1E1  Countdown" || e.SeriesTitle != "3 Body Problem" || e.SeriesID != s.ID ||
		e.ParentID != se.ID || e.Duration != 3651 || !e.Playable() {
		t.Fatalf("episode %+v", e)
	}
	if e.Thumb != s.Thumb || !strings.HasPrefix(e.Still, e.ID+"/Primary/") || e.Aspect != 1.78 || e.SourceID != e.ID {
		t.Fatalf("episode pictures %q %q aspect %v source %q", e.Thumb, e.Still, e.Aspect, e.SourceID)
	}
	if len(e.Audio) != 1 || e.Audio[0].ID != "1" || !e.Audio[0].Selected || len(e.Subs) != 2 || e.Subs[0].Title != "Arabic - SUBRIP" {
		t.Fatalf("streams %+v %+v", e.Audio, e.Subs)
	}
}

func TestItemAndMarkers(t *testing.T) {
	_, c := newFake(t)
	signIn(t, c)
	it, err := c.Item(context.Background(), "358f1b71dbd1eb6238744f21a9a01f14")
	if err != nil {
		t.Fatal(err)
	}
	if it.Type != "movie" || it.Title != "... nur noch Gewolk ..." || it.Duration != 953 || it.SourceID == "" {
		t.Fatalf("item %+v", it)
	}
	if len(it.Markers) != 2 || it.Markers[0] != (Marker{"intro", 0, 60}) || it.Markers[1].Type != "credits" {
		t.Fatalf("markers %+v", it.Markers)
	}
}

// Paging walks a listing by startIndex/limit and stops at the total; a
// page past the end is empty and keeps the total.
func TestPaging(t *testing.T) {
	var proto map[string]any
	var l struct{ Items []map[string]any }
	if err := json.Unmarshal(fixture(t, "movies_p0.json"), &l); err != nil {
		t.Fatal(err)
	}
	proto = l.Items[0]
	const total = 1203
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		calls = append(calls, q.Get("startIndex")+"+"+q.Get("limit"))
		if q.Get("enableTotalRecordCount") != "true" || q.Get("userId") != testUser || q.Get("fields") == "" {
			t.Errorf("query %v", q)
		}
		var start, limit int
		json.Unmarshal([]byte(q.Get("startIndex")), &start)
		json.Unmarshal([]byte(q.Get("limit")), &limit)
		var items []map[string]any
		for i := start; i < min(start+limit, total); i++ {
			m := map[string]any{}
			for k, v := range proto {
				m[k] = v
			}
			m["Id"] = "id" + itoa(i)
			items = append(items, m)
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": total, "StartIndex": start})
	}))
	defer srv.Close()
	c := New(srv.URL, t.TempDir(), "dev-1", &MemoryStore{})
	c.Store.SetCredentials(Credentials{UserID: testUser, Token: "TESTTOKEN"})
	ctx := context.Background()
	all, err := c.all(ctx, "/Items?parentId=lib", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != total || all[0].ID != "id0" || all[total-1].ID != "id1202" {
		t.Fatalf("walked %d", len(all))
	}
	if strings.Join(calls, " ") != "0+500 500+500 1000+500" {
		t.Fatalf("calls %v", calls)
	}
	items, n, err := c.Page(ctx, "/Items", nil, 1260, 60)
	if err != nil || len(items) != 0 || n != total {
		t.Fatalf("past the end: %d of %d, %v", len(items), n, err)
	}
	items, n, err = c.Page(ctx, "/Items", nil, 1200, 60)
	if err != nil || len(items) != 3 || n != total {
		t.Fatalf("last page: %d of %d, %v", len(items), n, err)
	}
}

// A 401 is ErrUnauthorized at once; a 5xx or a dropped connection is
// retried, a 404 is not.
func TestErrors(t *testing.T) {
	var hits int
	status := 401
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if status == 503 && hits == 3 {
			w.Write(fixture(t, "views.json"))
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := New(srv.URL, t.TempDir(), "dev-1", nil)
	c.Store.SetCredentials(Credentials{UserID: testUser, Token: "revoked"})
	c.Backoff = time.Millisecond
	ctx := context.Background()

	_, err := c.Sections(ctx)
	var he *HTTPError
	if !errors.Is(err, ErrUnauthorized) || !errors.As(err, &he) || he.Path != "/UserViews" || hits != 1 {
		t.Fatalf("401: %v after %d requests", err, hits)
	}
	if strings.Contains(err.Error(), "revoked") || strings.Contains(err.Error(), "userId") {
		t.Fatalf("error leaks the request: %v", err)
	}
	if err := c.Scrobble(ctx, "x", true); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("POST 401: %v", err)
	}

	status, hits = 503, 0
	secs, err := c.Sections(ctx)
	if err != nil || len(secs) != 2 || hits != 3 {
		t.Fatalf("503 twice then 200: %v %v after %d", secs, err, hits)
	}

	status, hits = 404, 0
	if _, err := c.Item(ctx, "nope"); errors.Is(err, ErrUnauthorized) || err == nil || hits != 1 {
		t.Fatalf("404: %v after %d", err, hits)
	}

	status, hits = 500, 0
	if err := c.Scrobble(ctx, "x", true); err == nil || hits != 1 {
		t.Fatalf("POST is not retried: %v after %d", err, hits)
	}

	// a cancelled context stops the backoff
	status, hits = 502, 0
	c.Backoff = time.Hour
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := c.Sections(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled: %v", err)
	}

	// the network: a closed server
	srv.Close()
	c.Backoff = time.Millisecond
	if _, err := c.Sections(ctx); err == nil || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("closed server: %v", err)
	}
}

func TestImageURL(t *testing.T) {
	ref := "e8597adb9c3c4567dc82aac5981b22ac/Primary/c98cfdc0a2362738e4cce2a624c99e03"
	c := New("https://jf.example/base/", t.TempDir(), "dev-1", nil)
	c.Store.SetCredentials(Credentials{Token: "secret"})
	got, err := c.ImageURL(ref, ImageQuery{FillWidth: 240, FillHeight: 360, Quality: 90})
	want := "https://jf.example/base/Items/e8597adb9c3c4567dc82aac5981b22ac/Images/Primary?fillHeight=360&fillWidth=240&quality=90&tag=c98cfdc0a2362738e4cce2a624c99e03"
	if err != nil || got != want {
		t.Fatalf("got %q %v\nwant %q", got, err, want)
	}
	got, _ = c.ImageURL("abc/Logo/t1", ImageQuery{MaxWidth: 400, MaxHeight: 120, Format: "Png"})
	if got != "https://jf.example/base/Items/abc/Images/Logo?format=Png&maxHeight=120&maxWidth=400&tag=t1" {
		t.Fatalf("logo %q", got)
	}
	for _, bad := range []string{"", "abc", "abc/Primary/", "/Primary/t"} {
		if _, err := c.ImageURL(bad, ImageQuery{}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if u, _ := c.ImageURL(ref, ImageQuery{}); strings.Contains(u, "secret") {
		t.Fatal("token in image URL")
	}

	// Art fetches once and then serves the cache; another size is another file
	f, c := newFake(t)
	signIn(t, c)
	ctx := context.Background()
	p1, err := c.Art(ctx, ref, 135, 203)
	if err != nil {
		t.Fatal(err)
	}
	n := len(f.reqs)
	p2, _ := c.Art(ctx, ref, 135, 203)
	p3, _ := c.Logo(ctx, ref, 120, 40)
	if p1 != p2 || len(f.reqs) != n+1 || p3 == p1 || !strings.HasSuffix(p3, ".png") {
		t.Fatalf("cache: %s %s %s, %d requests", p1, p2, p3, len(f.reqs)-n)
	}
	last := f.reqs[len(f.reqs)-1].URL.Query()
	if last.Get("maxWidth") != "120" || last.Get("format") != "Png" || last.Get("tag") != "c98cfdc0a2362738e4cce2a624c99e03" {
		t.Fatalf("logo query %v", last)
	}
	if data, err := c.ArtData(ctx, ref, 135, 203); err != nil || string(data) != "\xff\xd8jpeg" {
		t.Fatalf("art data %q %v", data, err)
	}
}

func TestHomeRows(t *testing.T) {
	_, c := newFake(t)
	signIn(t, c)
	ctx := context.Background()
	hubs, err := c.Hubs(ctx, 30)
	if err != nil || len(hubs) != 1 || !hubs[0].IsContinueWatching() {
		t.Fatalf("hubs %v %v", hubs, err)
	}
	// resume: an episode at 20 min and a film; next up adds one episode
	items := hubs[0].Items
	if len(items) != 3 || items[0].ViewOffset != 1200 || items[0].Type != "episode" || items[1].Type != "movie" || items[2].Type != "episode" {
		for _, it := range items {
			t.Logf("%s %s %d", it.Type, it.Title, it.ViewOffset)
		}
		t.Fatalf("continue watching: %d items", len(items))
	}
	keys, err := c.ContinueWatching(ctx)
	if err != nil || len(keys) != 3 || !keys[items[0].ID] {
		t.Fatalf("continue watching keys %v %v", keys, err)
	}
	hub, err := c.SectionRecent(ctx, "f137a2dd21bbc1b99aa5c0f6bf02a805", 30)
	if err != nil || hub == nil || len(hub.Items) != 3 || hub.Items[0].Title != "The Last First: Winter K2" {
		t.Fatalf("recent %+v %v", hub, err)
	}
	found, err := c.Search(ctx, "  the ")
	if err != nil || len(found) != 3 || found[0].Type != "movie" {
		t.Fatalf("search %v %v", found, err)
	}
	if found, _ := c.Search(ctx, " "); found != nil {
		t.Fatal("empty search sent")
	}
}

// Fold keeps Central European, Baltic, Turkish and Romanian letters as
// their base letters, as subtitles in those languages need; other scripts
// still become spaces.
func TestFoldLatinExtended(t *testing.T) {
	for in, want := range map[string]string{
		"Źdźbło łąki":       "Zdzblo laki",
		"Příliš žluťoučký":  "Prilis zlutoucky",
		"Árvíztűrő ığdır":   "Arvizturo igdir",
		"Ştiinţă și țară":   "Stiinta si tara",
		"Čađa IJsselmeer Ĳ": "Cada IJsselmeer IJ",
		"Привет":            "      ",
		"Œuvre – “quoted”…": "OEuvre - \"quoted\"...",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

// A text subtitle the server can hand over is marked Text and fetched whole
// as SRT; a picture subtitle, or a text one the server cannot deliver on its
// own, is not.
func TestSubtitleTracks(t *testing.T) {
	var x dto
	if err := json.Unmarshal([]byte(`{"Id":"film","Type":"Movie","MediaSources":[{"Id":"src","MediaStreams":[
		{"Type":"Subtitle","Index":2,"Codec":"subrip","IsTextSubtitleStream":true,"SupportsExternalStream":true},
		{"Type":"Subtitle","Index":3,"Codec":"PGSSUB","IsTextSubtitleStream":false,"SupportsExternalStream":false},
		{"Type":"Subtitle","Index":4,"Codec":"ass","IsTextSubtitleStream":true,"SupportsExternalStream":false}]}]}`), &x); err != nil {
		t.Fatal(err)
	}
	it := x.item()
	if len(it.Subs) != 3 || !it.Subs[0].Text || it.Subs[1].Text || it.Subs[2].Text {
		t.Fatalf("subtitle kinds %+v", it.Subs)
	}
	f, c := newFake(t)
	signIn(t, c)
	item, err := c.Item(context.Background(), "358f1b71dbd1eb6238744f21a9a01f14")
	if err != nil || len(item.Subs) == 0 || !item.Subs[0].Text {
		t.Fatalf("fixture's SubRip track %+v %v", item, err)
	}
	data, err := c.SubtitleText(context.Background(), "film", "src", "2")
	if err != nil || !strings.Contains(string(data), "Hello") {
		t.Fatalf("subtitle text %q %v", data, err)
	}
	r := f.reqs[len(f.reqs)-1]
	if r.Method != "GET" || r.URL.Path != "/Videos/film/src/Subtitles/2/Stream.srt" || r.URL.RawQuery != "" {
		t.Fatalf("subtitle request %s %s", r.Method, r.URL)
	}
}

func TestWatchedAndPlayback(t *testing.T) {
	f, c := newFake(t)
	signIn(t, c)
	ctx := context.Background()
	if err := c.Scrobble(ctx, "abc", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Scrobble(ctx, "abc", false); err != nil {
		t.Fatal(err)
	}
	if err := c.ClearResumePoint(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	var methods []string
	for _, r := range f.reqs[len(f.reqs)-3:] {
		methods = append(methods, r.Method+" "+r.URL.Path+" "+r.URL.Query().Get("userId"))
	}
	want := "POST /UserPlayedItems/abc " + testUser + "|DELETE /UserPlayedItems/abc " + testUser + "|POST /UserItems/abc/UserData " + testUser
	if strings.Join(methods, "|") != want {
		t.Fatalf("got %v", methods)
	}

	profile := json.RawMessage(`{"MaxStreamingBitrate":3000000,"TranscodingProfiles":[{"Container":"ts","Type":"Video","VideoCodec":"h264"}]}`)
	info, err := c.PlaybackInfo(ctx, "358f1b71dbd1eb6238744f21a9a01f14", PlaybackRequest{DeviceProfile: profile, StartSeconds: 90, AudioStream: "1", SubtitleStream: "-1"})
	if err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	json.Unmarshal(f.body["POST /Items/358f1b71dbd1eb6238744f21a9a01f14/PlaybackInfo"], &sent)
	if sent["UserId"] != testUser || sent["StartTimeTicks"] != float64(900000000) || sent["AudioStreamIndex"] != float64(1) ||
		sent["SubtitleStreamIndex"] != float64(-1) || sent["AlwaysBurnInSubtitleWhenTranscoding"] != nil ||
		sent["EnableDirectPlay"] != nil || sent["EnableDirectStream"] != nil ||
		sent["DeviceProfile"].(map[string]any)["MaxStreamingBitrate"] != float64(3000000) {
		t.Fatalf("playback info body %v", sent)
	}
	if info.PlaySessionID == "" || len(info.MediaSources) != 1 || !info.MediaSources[0].SupportsTranscoding {
		t.Fatalf("playback info %+v", info)
	}
	if info.MediaSources[0].DefaultAudioStream == nil || *info.MediaSources[0].DefaultAudioStream != 1 || len(info.MediaSources[0].MediaStreams) <= 1 {
		t.Fatalf("playback source stream details %+v", info.MediaSources[0])
	}
	if info.MediaSources[0].MediaStreams[1].Channels != 1 {
		t.Fatalf("playback source audio channels = %d", info.MediaSources[0].MediaStreams[1].Channels)
	}
	stream := c.StreamURL(info.MediaSources[0].TranscodingURL)
	u, _ := url.Parse(stream)
	if !strings.HasPrefix(stream, c.Host+"/videos/") || u.Query().Has("ApiKey") || u.Query().Get("VideoCodec") != "h264" {
		t.Fatalf("stream url %s", stream)
	}
	if u.Query().Get("SubtitleMethod") != "Encode" || u.Query().Get("SubtitleStreamIndex") != "2" {
		t.Fatalf("selected subtitle transcode URL lacks burn-in: %s", stream)
	}
	_, err = c.PlaybackInfo(ctx, "358f1b71dbd1eb6238744f21a9a01f14", PlaybackRequest{
		SubtitleStream: "2", AlwaysBurnInSubtitleWhenTranscoding: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var selected map[string]any
	json.Unmarshal(f.body["POST /Items/358f1b71dbd1eb6238744f21a9a01f14/PlaybackInfo"], &selected)
	if selected["SubtitleStreamIndex"] != float64(2) || selected["AlwaysBurnInSubtitleWhenTranscoding"] != true ||
		selected["EnableDirectPlay"] != false || selected["EnableDirectStream"] != false {
		t.Fatalf("selected subtitle request %v", selected)
	}
	_, err = c.PlaybackInfo(ctx, "358f1b71dbd1eb6238744f21a9a01f14", PlaybackRequest{
		SubtitleStream: "-1", MaxStreamingBitrate: 5000000, ForceTranscode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var forced map[string]any
	json.Unmarshal(f.body["POST /Items/358f1b71dbd1eb6238744f21a9a01f14/PlaybackInfo"], &forced)
	if forced["EnableDirectPlay"] != false || forced["EnableDirectStream"] != false ||
		forced["AlwaysBurnInSubtitleWhenTranscoding"] != nil || forced["MaxStreamingBitrate"] != float64(5000000) {
		t.Fatalf("forced transcode request %v", forced)
	}
	ds := c.DirectStreamURL("abc", MediaSource{ID: "abc", Container: "mkv"}, "ps")
	if !strings.HasPrefix(ds, c.Host+"/Videos/abc/stream.mkv?") || !strings.Contains(ds, "static=true") {
		t.Fatalf("direct stream %s", ds)
	}

	st := PlayState{ItemID: "abc", MediaSourceID: "abc", PlaySessionID: "ps", Position: 61.5, Paused: true}
	for _, report := range []func(context.Context, PlayState) error{c.ReportStart, c.ReportProgress, c.ReportStopped} {
		if err := report(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	var prog, stop map[string]any
	json.Unmarshal(f.body["POST /Sessions/Playing/Progress"], &prog)
	json.Unmarshal(f.body["POST /Sessions/Playing/Stopped"], &stop)
	if prog["PositionTicks"] != float64(615000000) || prog["IsPaused"] != true || prog["PlayMethod"] != "Transcode" || prog["PlaySessionId"] != "ps" {
		t.Fatalf("progress %v", prog)
	}
	if stop["PositionTicks"] != float64(615000000) || stop["IsPaused"] != nil {
		t.Fatalf("stopped %v", stop)
	}
}

func TestBlurCorners(t *testing.T) {
	// a 1x1 hash is one flat colour: its DC, at every corner
	flat := blurCorners("00Lm.") // too short for its size flag: zero
	if flat != [4]uint32{} {
		t.Fatalf("bad hash decoded: %x", flat)
	}
	dc := "0" + "0" + enc83(0x336699, 4)
	got := blurCorners(dc)
	for _, v := range got {
		if v != 0x336699 {
			t.Fatalf("flat hash corners %x", got)
		}
	}
	// a real one: corners differ and are not black
	got = blurCorners("dYHT{dD*bv-U1OoJj[R+0gx[V[M|}rxFWBoJODWBo0of")
	if got[0] == 0 || got[0] == got[2] {
		t.Fatalf("corners %x", got)
	}
}

func enc83(v, n int) string {
	b := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		b[i] = base83[v%83]
		v /= 83
	}
	return string(b)
}

func TestSortTitleAndAspect(t *testing.T) {
	for in, want := range map[string]string{"0000000003 body problem": "3 body problem", "    nur noch gewolk    ": "nur noch gewolk",
		"apollo 0000000013": "apollo 13", "0000000000": "0", "the 1000000000 club": "the 1000000000 club"} {
		if got := sortTitle(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if aspect("16:9", 0, 0) != 1.78 || aspect("", 720, 480) != 1.5 || aspect("x", 0, 0) != 0 {
		t.Fatal("aspect")
	}
	if LangName("ara") != "Arabic" || LangName("zh-Hant") != "Chinese" {
		t.Fatal("lang names")
	}
}

func TestItemKeepsSourceAudioChannelsAndDefaultSelection(t *testing.T) {
	item := (&dto{MediaSources: []mediaSource{{
		ID:           "source",
		MediaStreams: []MediaStream{{Type: "Audio", Index: 3, Channels: 6, IsDefault: true}},
	}}}).item()
	if len(item.Audio) != 1 || item.Audio[0].Channels != 6 || !item.Audio[0].Selected {
		t.Fatalf("source audio stream = %+v", item.Audio)
	}
}
