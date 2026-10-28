package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// testVersions are a synthetic title's versions in the server's order: a
// 1080p original over the HD direct limit (its source id is the item's
// own), a 720p copy and a 480p copy (<id>-12, <id>-13).
var testVersions = []struct {
	suffix     string
	w, h, kbps int
}{{"", 1920, 1080, 7800}, {"-12", 1280, 720, 1200}, {"-13", 854, 480, 700}}

// versionRig is an app against a server holding a film and a season of
// two episodes, each in the three versions, whose player stops at stopAt of
// the items' 1000 s. server is the version the server says it runs;
// offsets are each version's own resume point (Jellyfin 12 and later), and
// a version with one comes first, as the server lists the last played.
type versionRig struct {
	a       *App
	logs    *syncBuffer
	logTo   string
	mu      sync.Mutex
	server  string
	offsets map[string]int
	played  []string  // each start's source (its transcode request)
	starts  []float64 // and where it started, in seconds
}

func newVersionRig(t *testing.T, stopAt float64) *versionRig {
	t.Helper()
	r := &versionRig{logs: &syncBuffer{}, server: "12.4.0", offsets: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		p := req.URL.Path
		switch {
		case p == "/System/Info/Public":
			fmt.Fprintf(w, `{"Id":"server","ServerName":"Test","Version":%q}`, r.server)
		case p == "/Items/film":
			_, _ = w.Write([]byte(r.record("film", "Movie")))
		case p == "/Items/ep1" || p == "/Items/ep2":
			_, _ = w.Write([]byte(r.record(strings.TrimPrefix(p, "/Items/"), "Episode")))
		case p == "/Shows/show/Episodes":
			jfPage(w, req, r.listed("ep1"), r.listed("ep2"))
		case strings.HasPrefix(p, "/UserItems/") && strings.HasSuffix(p, "/UserData"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/UserItems/"), "/UserData")
			fmt.Fprintf(w, `{"PlaybackPositionTicks":%d}`, int64(r.offsets[id])*jellyfin.TicksPerSecond)
		case strings.HasSuffix(p, "/PlaybackInfo"):
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			source, _ := body["MediaSourceId"].(string)
			if body["EnableDirectPlay"] == false {
				ticks, _ := body["StartTimeTicks"].(float64)
				r.played = append(r.played, source)
				r.starts = append(r.starts, ticks/jellyfin.TicksPerSecond)
			}
			fmt.Fprintf(w, `{"PlaySessionId":"session","MediaSources":[{"Id":%q,"TranscodingUrl":"/stream.ts?x=1","TranscodingSubProtocol":"http"}]}`, source)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	store := &jellyfin.MemoryStore{}
	store.SetCredentials(jellyfin.Credentials{UserID: "user", Token: "synthetic-token"})
	client := testJellyfinClient(srv.URL, "", "device", store)
	client.SetHTTPClient(srv.Client())
	dir := t.TempDir()
	status := dir + "/status"
	r.logTo = dir + "/log"
	p := &Player{Client: client, Binary: "jfplay-test", Fifo: dir + "/fifo", Status: status, LogTo: r.logTo}
	p.exec = func(string, ...string) *exec.Cmd {
		script := fmt.Sprintf("printf 'state=playing\\nposition=%g\\nduration=1000\\n' > '%s'", stopAt, status)
		return exec.Command("sh", "-c", script)
	}
	cfg := &Config{NoTheme: true, path: filepath.Join(t.TempDir(), "settings.json")}
	p.Kbps, p.DirectPlay = cfg.BitrateKbps, cfg.DirectPlay
	r.a = &App{Log: log.New(r.logs, "", 0), Out: &foreignPresenter{gfx.NewCanvas(8, 4)}, Client: client, Player: p,
		Cfg: cfg, Wake: make(chan struct{}, 1), later: make(chan func(), 32), F: appFonts()}
	r.a.stack = []Screen{&splashTestScreen{}}
	return r
}

// record is an item's own record with the three versions, a version with
// a resume point first. mu is held.
func (r *versionRig) record(id, typ string) string {
	var sources []string
	for _, v := range testVersions {
		sid := id + v.suffix
		s := fmt.Sprintf(`{"Id":%q,"Name":"Title %s - %dp","Path":"/media/Title %s - %dp.mkv","Container":"mkv","Bitrate":%d,"MediaStreams":[`+
			`{"Type":"Video","Index":0,"Codec":"h264","Profile":"High","Level":40,"BitDepth":8,"Width":%d,"Height":%d,"RealFrameRate":23.976,"BitRate":%d},`+
			`{"Type":"Audio","Index":1,"Codec":"aac","Channels":2,"IsDefault":true,"Language":"eng","DisplayTitle":"English AAC %s"}]}`,
			sid, id, v.h, id, v.h, v.kbps*1000, v.w, v.h, (v.kbps-100)*1000, sid)
		if r.offsets[sid] > 0 {
			sources = append([]string{s}, sources...)
		} else {
			sources = append(sources, s)
		}
	}
	return fmt.Sprintf(`{"Id":%q,"Type":%q,"Name":"Title %s","SeriesId":"show","SeasonId":"season","RunTimeTicks":%d,`+
		`"UserData":{"PlaybackPositionTicks":%d},"MediaSources":[%s]}`,
		id, typ, id, int64(1000)*jellyfin.TicksPerSecond, int64(r.offsets[id])*jellyfin.TicksPerSecond, strings.Join(sources, ","))
}

// listed is an episode as a listing has it: the versions only counted.
func (r *versionRig) listed(id string) string {
	return fmt.Sprintf(`{"Id":%q,"Type":"Episode","Name":"Title %s","SeriesId":"show","SeasonId":"season","RunTimeTicks":%d,"MediaSourceCount":3}`,
		id, id, int64(1000)*jellyfin.TicksPerSecond)
}

func (r *versionRig) playedSources() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.played...)
}

func (r *versionRig) item(t *testing.T, id string) *jellyfin.Item {
	t.Helper()
	it, err := r.a.Client.Item(bg(), id)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func (r *versionRig) listing(t *testing.T) []*jellyfin.Item {
	t.Helper()
	eps, err := r.a.Client.Items(bg(), "/Shows/show/Episodes?seasonId=season", nil, 50)
	if err != nil || len(eps) != 2 {
		t.Fatalf("listing %v, %v", eps, err)
	}
	return eps
}

// playLog is the playback log of the last start.
func (r *versionRig) playLog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.logTo)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// chooser is the chooser on top, failing the test without one.
func (r *versionRig) chooser(t *testing.T) *Chooser {
	t.Helper()
	ch, ok := r.a.top().(*Chooser)
	if !ok {
		t.Fatalf("no chooser on top: %T; played %v", r.a.top(), r.playedSources())
	}
	return ch
}

// pick moves the chooser's cursor to i and presses OK, which plays.
func (r *versionRig) pick(t *testing.T, i int) {
	t.Helper()
	ch := r.chooser(t)
	ch.cur = i
	ch.Key(input.Event{Key: input.Enter}, time.Now())
}

func sameSources(got []string, want ...string) bool {
	return strings.Join(got, " ") == strings.Join(want, " ")
}

// A first play asks, the version that suits the cores (the 720p copy, the
// largest that plays as stored) selected; OK plays the one picked, and
// after runs only then. The pick is recorded.
func TestFirstPlayAsksForTheVersion(t *testing.T) {
	r := newVersionRig(t, 300)
	it := r.item(t, "film")
	afters := 0
	r.a.PlayQueue(it, 0, nil, 0, func(*jellyfin.Item, bool) { afters++ })
	ch := r.chooser(t)
	if ch.title != "Play which version?" || ch.cur != 1 || len(ch.items) != 3 || ch.items[1] != "720p H.264, 1.2 Mbps, plays directly" {
		t.Fatalf("chooser %q at %d: %q", ch.title, ch.cur, ch.items)
	}
	if len(r.playedSources()) != 0 || afters != 0 {
		t.Fatal("played before the pick")
	}
	r.pick(t, 2)
	if !sameSources(r.playedSources(), "film-13") || afters != 1 {
		t.Fatalf("played %v, after ran %d times", r.playedSources(), afters)
	}
	if got := r.a.Cfg.Versions["film"].Media; got != "film-13" {
		t.Fatalf("recorded %q", got)
	}
	if !strings.Contains(r.logs.String(), "version 3 of 3 (source film-13) 480p h264, chosen: picked") {
		t.Fatalf("log:\n%s", r.logs.String())
	}
	if !strings.Contains(r.playLog(t), "app: version 3 of 3 (source film-13) 480p h264") {
		t.Fatalf("playback log:\n%s", r.playLog(t))
	}
	if it.VersionID != "film-13" || it.SourceID != "film-13" {
		t.Fatalf("item left on %q, source %q", it.VersionID, it.SourceID)
	}
}

// Back on the chooser plays nothing and never runs after.
func TestBackOnTheVersionChooserPlaysNothing(t *testing.T) {
	r := newVersionRig(t, 300)
	afters := 0
	r.a.PlayQueue(r.item(t, "film"), 0, nil, 0, func(*jellyfin.Item, bool) { afters++ })
	r.chooser(t)
	r.a.Pop()
	if len(r.playedSources()) != 0 || afters != 0 || r.a.Cfg.Versions != nil {
		t.Fatalf("played %v, after %d, recorded %v", r.playedSources(), afters, r.a.Cfg.Versions)
	}
}

// Before 12 the server keeps one resume point: a resume plays the recorded
// version unasked; a resume nobody recorded a version for asks; From start
// asks with the recorded one selected.
func TestResumeAndFromStartVersionsBefore12(t *testing.T) {
	r := newVersionRig(t, 300)
	r.server = "10.11.0"
	r.offsets["film"] = 120
	r.a.Cfg.Versions = map[string]VersionChoice{"film": {Media: "film-13", At: 1}}
	it := r.item(t, "film")
	r.a.applyVersion(it, "")
	r.a.PlayQueue(it, it.ViewOffset, nil, 0, nil)
	if !sameSources(r.playedSources(), "film-13") {
		t.Fatalf("resume played %v (top %T), want the recorded 480p unasked", r.playedSources(), r.a.top())
	}
	if !strings.Contains(r.logs.String(), "chosen: remembered") {
		t.Fatalf("log:\n%s", r.logs.String())
	}

	r.a.PlayQueue(it, 0, nil, 0, nil)
	if ch := r.chooser(t); ch.cur != 2 {
		t.Fatalf("From start selected %d, want the recorded 480p", ch.cur)
	}
	r.a.Pop()

	delete(r.a.Cfg.Versions, "film")
	it = r.item(t, "film")
	r.a.applyVersion(it, "")
	r.a.PlayQueue(it, 120, nil, 0, nil)
	if ch := r.chooser(t); ch.cur != 1 {
		t.Fatalf("unrecorded resume selected %d, want our pick", ch.cur)
	}
}

// From 12 each version keeps its own resume point and the server lists
// the last played first: the item's is that one's, and a resume plays it
// unasked from there. The Version row moves the item to another version's
// own point.
func TestEachVersionsOwnResumePoint(t *testing.T) {
	r := newVersionRig(t, 300)
	r.offsets["film-13"] = 120
	it := r.item(t, "film")
	p := NewPreplay(r.a, it)
	r.a.Push(p)
	if it.VersionID != "film-13" || it.ViewOffset != 120 || p.actions[0] != "Resume from "+clock(120) {
		t.Fatalf("page on %q at %d: %q", it.VersionID, it.ViewOffset, p.actions)
	}
	p.cur = 0
	p.Key(input.Event{Key: input.Enter}, time.Now())
	r.mu.Lock()
	played, starts := r.played, r.starts
	r.mu.Unlock()
	if !sameSources(played, "film-13") || starts[0] != 120 {
		t.Fatalf("resume played %v from %v (top %T)", played, starts, r.a.top())
	}
	if !strings.Contains(r.logs.String(), "chosen: in progress") {
		t.Fatalf("log:\n%s", r.logs.String())
	}

	r.mu.Lock() // the playback's own record reads may still be on their way
	r.offsets["film"] = 50
	r.mu.Unlock()
	r.a.takeFresh(it, r.item(t, "film"))
	p.rebuild()
	p.cur = restoreAction(p.actions, "Version")
	p.Key(input.Event{Key: input.Enter}, time.Now())
	r.pick(t, jellyfin.VersionIndex(it.Versions, "film"))
	if it.VersionID != "film" || it.ViewOffset != 50 || p.actions[0] != "Resume from "+clock(50) {
		t.Fatalf("after the row's pick: %q at %d, %q", it.VersionID, it.ViewOffset, p.actions)
	}
}

// A finished playback forgets the version: the next play is from the start.
func TestFinishingForgetsTheVersion(t *testing.T) {
	r := newVersionRig(t, 990)
	r.a.PlayQueue(r.item(t, "film"), 0, nil, 0, nil)
	r.pick(t, 0) // the 1080p original
	if !sameSources(r.playedSources(), "film") {
		t.Fatalf("played %v", r.playedSources())
	}
	if _, ok := r.a.Cfg.Versions["film"]; ok {
		t.Fatal("a finished item kept its version")
	}
}

// A listing's episode reached along a queue never asks: its start reads
// the record and plays the kind the show last started with; the item and
// the records take it.
func TestQueueMatchesTheShowsVersion(t *testing.T) {
	r := newVersionRig(t, 300)
	eps := r.listing(t)
	r.a.PlayQueue(r.item(t, "ep1"), 0, nil, 0, nil)
	r.pick(t, 2)
	if kind := r.a.Cfg.Streams["show"].VersionKind; kind != "480p h264" {
		t.Fatalf("show's kind %q", kind)
	}
	next := eps[1]
	if why := r.a.quietVersion(next); why != "" || next.VersionID != "" {
		t.Fatalf("a listing's episode settled as %q on %q before its record", why, next.VersionID)
	}
	r.a.playQueue(next, 0, eps, 1, nil, true)
	if _, ok := r.a.top().(*Chooser); ok || !sameSources(r.playedSources(), "ep1-13", "ep2-13") {
		t.Fatalf("played %v (top %T), want both episodes' 480p unasked", r.playedSources(), r.a.top())
	}
	if !strings.Contains(r.playLog(t), "app: version 3 of 3 (source ep2-13) 480p h264, chosen: matched") {
		t.Fatalf("playback log:\n%s", r.playLog(t))
	}
	if next.VersionID != "ep2-13" || !next.Versions[0].Detailed() || r.a.Cfg.Versions["ep2"].Media != "ep2-13" {
		t.Fatalf("item on %q, recorded %v", next.VersionID, r.a.Cfg.Versions)
	}
}

// A shuffle never asks; a listing's episode nobody chose a version for gets
// the one that suits the cores from its start.
func TestShuffleNeverAsks(t *testing.T) {
	r := newVersionRig(t, 300)
	eps := r.listing(t)
	r.a.playQueue(eps[0], 0, eps, 0, nil, true)
	if _, ok := r.a.top().(*Chooser); ok {
		t.Fatal("a shuffle asked")
	}
	if !sameSources(r.playedSources(), "ep1-12") {
		t.Fatalf("played %v, want our pick, the 720p", r.playedSources())
	}
	if eps[0].VersionID != "ep1-12" || r.a.Cfg.Versions["ep1"].Media != "ep1-12" {
		t.Fatalf("item on %q, recorded %v", eps[0].VersionID, r.a.Cfg.Versions)
	}
}

// A start that settles the version resumes a negative offset at that
// version's own point.
func TestSettledStartResumesTheVersionsPoint(t *testing.T) {
	r := newVersionRig(t, 300)
	r.offsets["ep2-12"] = 200
	ep := r.listing(t)[1]
	s, err := r.a.Player.StartAsyncSettling(ep, -1, &versionRule{})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	r.mu.Lock()
	played, starts := r.played, r.starts
	r.mu.Unlock()
	if !sameSources(played, "ep2-12") || starts[0] != 200 {
		t.Fatalf("played %v from %v", played, starts)
	}
	if id, settled := s.Version(); id != "ep2-12" || !settled {
		t.Fatalf("session on %q, settled %v", id, settled)
	}
}

// A listing's item reads its record before asking: the lines need it.
func TestListingItemReadsItsVersionsFirst(t *testing.T) {
	r := newVersionRig(t, 300)
	eps := r.listing(t)
	if len(eps[0].Versions) != 3 || eps[0].Versions[0].Detailed() {
		t.Fatalf("listing's versions %+v", eps[0].Versions)
	}
	r.a.PlayQueue(eps[0], 0, eps, 0, nil)
	if r.a.reading == nil || r.a.Starting.IsZero() {
		t.Fatal("no read of the versions under way")
	}
	for end := time.Now().Add(5 * time.Second); r.a.reading != nil && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		r.a.runLater()
	}
	ch := r.chooser(t)
	if ch.cur != 1 || !strings.HasSuffix(ch.items[1], "plays directly") || !eps[0].Versions[0].Detailed() {
		t.Fatalf("chooser at %d: %q", ch.cur, ch.items)
	}
}

// A fresh record keeps the version in force, and the Version row shows it.
func TestFreshRecordKeepsTheVersion(t *testing.T) {
	r := newVersionRig(t, 300)
	it := r.item(t, "film")
	r.a.useVersion(it, "film-13")
	r.a.takeFresh(it, r.item(t, "film"))
	if it.VersionID != "film-13" || it.SourceID != "film-13" || it.Audio[0].Title != "English AAC film-13" {
		t.Fatalf("fresh record on %q, source %q, audio %+v", it.VersionID, it.SourceID, it.Audio)
	}
	if row := versionRow(it); row != "Version: 480p H.264" {
		t.Fatalf("row %q", row)
	}
	if versionRow(&jellyfin.Item{ID: "x", SourceID: "x"}) != "" {
		t.Fatal("a single version item has a Version row")
	}
}

// Continue Watching names a title by its version in progress, the library
// by its first: either finds the title's one record.
func TestVersionRecordUnderAnyVersionsID(t *testing.T) {
	a := newVersionRig(t, 300).a
	vs := []jellyfin.Version{{ID: "film"}, {ID: "film-12"}, {ID: "film-13"}}
	resumed, library := &jellyfin.Item{ID: "film-13", Versions: vs}, &jellyfin.Item{ID: "film", Versions: vs}
	a.noteVersion(resumed, "film-13")
	if got := a.rememberedVersion(library); got != "film-13" {
		t.Fatalf("the library's item remembered %q", got)
	}
	a.noteVersion(library, "film-12")
	if len(a.Cfg.Versions) != 1 || a.rememberedVersion(resumed) != "film-12" {
		t.Fatalf("records %v", a.Cfg.Versions)
	}
	listed := &jellyfin.Item{ID: "film-13", Versions: make([]jellyfin.Version, 3)}
	if r := a.versionRule(listed); r == nil || versionRecord(r.records, library) != "film-12" {
		t.Fatalf("rule %+v", r)
	}
	a.forgetVersion(resumed)
	if len(a.Cfg.Versions) != 0 {
		t.Fatalf("records after forgetting %v", a.Cfg.Versions)
	}
}

// A listed episode shows no resume point when its version in progress is
// not its first: once the record brings that version's, the cursor on Play
// stays first, on Resume.
func TestSeasonCursorTakesTheVersionsResume(t *testing.T) {
	r := newVersionRig(t, 300)
	r.offsets["ep2-12"] = 200
	listed := r.listing(t)[1]
	s := &Season{app: r.a, seasons: []*jellyfin.Item{{Key: "/Shows/show/Episodes?seasonId=season"}}, eps: []*jellyfin.Item{listed}, acts: true}
	s.rebuild()
	if s.actions[0] != "Play" {
		t.Fatalf("listed actions %q", s.actions)
	}
	s.streamsArrived(listed, r.item(t, "ep2"))
	if s.act != 0 || s.actions[0] != "Resume "+clock(200) || listed.VersionID != "ep2-12" {
		t.Fatalf("cursor %d on %q, version %q", s.act, s.actions, listed.VersionID)
	}
}

// Continue Watching names an episode by its version in progress; the
// season lists it by its first, at the same number.
func TestListedEpisode(t *testing.T) {
	eps := []*jellyfin.Item{{ID: "ep1", Index: 1}, {ID: "ep2", Index: 2}}
	for _, tc := range []struct {
		it   jellyfin.Item
		want string
	}{{jellyfin.Item{ID: "ep2", Index: 2}, "ep2"}, {jellyfin.Item{ID: "ep2-13", Index: 2}, "ep2"}, {jellyfin.Item{ID: "other"}, "other"}} {
		if got := listedEpisode(eps, &tc.it); got != tc.want {
			t.Errorf("listedEpisode(%s) = %s, want %s", tc.it.ID, got, tc.want)
		}
	}
}

// The movie page's Version row changes the version for the next play and
// records it.
func TestPreplayVersionRow(t *testing.T) {
	r := newVersionRig(t, 300)
	it := r.item(t, "film")
	p := NewPreplay(r.a, it)
	r.a.Push(p)
	if it.VersionID != "film-12" {
		t.Fatalf("page opened on %q, want our pick", it.VersionID)
	}
	at := restoreAction(p.actions, "Version")
	if p.actions[at] != "Version: 720p H.264" {
		t.Fatalf("actions %q", p.actions)
	}
	p.cur = at
	p.Key(input.Event{Key: input.Enter}, time.Now())
	r.pick(t, 0)
	if it.VersionID != "film" || r.a.Cfg.Versions["film"].Media != "film" || p.actions[p.cur] != "Version: 1080p H.264" {
		t.Fatalf("after the row's pick: %q, recorded %v, row %q", it.VersionID, r.a.Cfg.Versions, p.actions[p.cur])
	}
}

// A chosen track's index counts in another file only in the same language.
func TestTrackChoiceKeepsItsLanguage(t *testing.T) {
	subs := []jellyfin.Stream{{ID: "2", Language: "fre", Selected: true}, {ID: "3", Language: "eng"}}
	markChoice(subs, "3", "eng")
	if subs[0].Selected || !subs[1].Selected {
		t.Fatalf("same language: %+v", subs)
	}
	markChoice(subs, "2", "eng")
	if subs[0].Selected || !subs[1].Selected {
		t.Fatalf("index 2 in another language was taken: %+v", subs)
	}
	markChoice(subs, "2", "")
	if !subs[0].Selected {
		t.Fatalf("a choice saved without its language: %+v", subs)
	}
}
