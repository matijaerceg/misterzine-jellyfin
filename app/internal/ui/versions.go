package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	"plexcrt/internal/jellyfin"
)

// Versions: a movie or episode the server holds in several files. A play
// from the start asks which (the chooser, with the version in force
// selected); a resume plays the version that has the resume point, asking
// only when nobody knows which. Jellyfin 12 and later keep each version's
// own point and list the last one played first (jellyfin.Version.Offset);
// an older server keeps one point for all and no word of which played, so
// the app records the version that last started (noteVersion). Later
// items of a queue, and shuffles, never ask (quietVersion). The Version row
// of the movie page and the episode actions changes it for the next play.
//
// Listings only count an item's versions: the record says which they are,
// read before a first play asks (readVersions), and by the start for a
// queue's later items (Session.settleVersion).

// versionCap is the video cap and the Direct play setting the versions are
// judged at.
func (a *App) versionCap() (kbps int, direct bool) {
	if a.Cfg == nil {
		return 0, true
	}
	return a.Cfg.BitrateKbps(), a.Cfg.DirectPlay()
}

// versionPlays says which versions play as stored at a cap with Direct play
// on or off: the chooser's lines and the pick go by it.
func versionPlays(kbps int, direct bool) func(jellyfin.Version) bool {
	return func(v jellyfin.Version) bool {
		if !direct || !v.Detailed() {
			return false
		}
		ok, _ := directEligible(v.Source, kbps)
		return ok
	}
}

func (a *App) versionPlays() func(jellyfin.Version) bool { return versionPlays(a.versionCap()) }

// rememberedVersion is the version recorded for an item (noteVersion)
// while the item still has it, "" otherwise.
func (a *App) rememberedVersion(it *jellyfin.Item) string {
	if a.Cfg == nil {
		return ""
	}
	id := versionRecord(a.Cfg.Versions, it)
	if jellyfin.VersionIndex(it.Versions, id) < 0 {
		return ""
	}
	return id
}

// versionRecord is the version recorded for an item in records, "" for
// none. Jellyfin names a title by any of its versions' items (Continue
// Watching by the one in progress, the library by the first), so the
// record may be under any of them: the newest counts.
func versionRecord(records map[string]VersionChoice, it *jellyfin.Item) string {
	var newest VersionChoice
	for _, k := range versionKeys(it) {
		if ch, ok := records[k]; ok && (newest.Media == "" || ch.At > newest.At) {
			newest = ch
		}
	}
	return newest.Media
}

// versionKeys are the ids an item's record may be under: its own and, once
// its record is read, its versions' (a version's item id is its source id).
func versionKeys(it *jellyfin.Item) []string {
	keys := []string{it.ID}
	for _, v := range it.Versions {
		if v.ID != "" && v.ID != it.ID {
			keys = append(keys, v.ID)
		}
	}
	return keys
}

// versionKind is the kind of version an episode's show last started with,
// "" for none (and for a movie).
func (a *App) versionKind(it *jellyfin.Item) string {
	if a.Cfg == nil || it.Type != "episode" {
		return ""
	}
	return a.Cfg.Streams[streamChoiceKey(it)].VersionKind
}

// preferVersion is the version an item plays unasked, and why: where each
// version keeps its own resume point, the first with one (the server lists
// the last one played first); else the one recorded for the item, else the
// first of the kind its show last started with, else the one that suits
// the cores (jellyfin.PickVersion).
func preferVersion(vs []jellyfin.Version, remembered, kind string, plays func(jellyfin.Version) bool) (int, string) {
	if i := jellyfin.ResumeVersion(vs); i >= 0 {
		return i, "in progress"
	}
	if i := jellyfin.VersionIndex(vs, remembered); i >= 0 {
		return i, "remembered"
	}
	if i := jellyfin.MatchVersion(vs, kind); i >= 0 {
		return i, "matched"
	}
	return jellyfin.PickVersion(vs, plays), "our pick"
}

// preferredVersion is preferVersion for an item with the app's records.
func (a *App) preferredVersion(it *jellyfin.Item) (int, string) {
	remembered := ""
	if a.Cfg != nil {
		remembered = versionRecord(a.Cfg.Versions, it)
	}
	return preferVersion(it.Versions, remembered, a.versionKind(it), a.versionPlays())
}

// applyVersion puts in force the version an item plays: keep (the one it
// had) while the item still has it, else preferredVersion. A listing's
// record, which only counts its versions, keeps its first.
func (a *App) applyVersion(it *jellyfin.Item, keep string) {
	if len(it.Versions) < 2 || !it.Versions[0].Detailed() {
		return
	}
	if keep != "" && it.UseVersion(keep) {
		return
	}
	i, _ := a.preferredVersion(it)
	it.UseVersion(it.Versions[i].ID)
}

// useVersion puts a version in force and marks the tracks chosen for the
// item's movie or show on its tracks.
func (a *App) useVersion(it *jellyfin.Item, id string) {
	if it.UseVersion(id) {
		a.applyStreams(it)
	}
}

// noteVersion records an item's version (and for an episode its kind, for
// the show's next episodes), keeping the newest VersionsKept.
func (a *App) noteVersion(it *jellyfin.Item, id string) {
	i := jellyfin.VersionIndex(it.Versions, id)
	if a.Cfg == nil || i < 0 {
		return
	}
	if a.Cfg.Versions == nil {
		a.Cfg.Versions = map[string]VersionChoice{}
	}
	for _, k := range versionKeys(it) {
		delete(a.Cfg.Versions, k) // one record a title
	}
	a.Cfg.Versions[it.ID] = VersionChoice{Media: id, At: time.Now().Unix()}
	if over := len(a.Cfg.Versions) - VersionsKept; over > 0 {
		keys := make([]string, 0, len(a.Cfg.Versions))
		for k := range a.Cfg.Versions {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(x, y string) int {
			return cmp.Compare(a.Cfg.Versions[x].At, a.Cfg.Versions[y].At)
		})
		for _, k := range keys[:over] {
			if k != it.ID {
				delete(a.Cfg.Versions, k)
			}
		}
	}
	if kind := it.Versions[i].Kind(); it.Type == "episode" && kind != "" {
		key := streamChoiceKey(it)
		ch := a.Cfg.Streams[key]
		ch.VersionKind = kind
		if a.Cfg.Streams == nil {
			a.Cfg.Streams = map[string]StreamChoice{}
		}
		a.Cfg.Streams[key] = ch
	}
	a.saveSettings()
}

// forgetVersion drops an item's recorded version: it finished, or was
// marked watched, and its next play is from the start.
func (a *App) forgetVersion(it *jellyfin.Item) {
	if a.Cfg == nil {
		return
	}
	dropped := false
	for _, k := range versionKeys(it) {
		if _, ok := a.Cfg.Versions[k]; ok {
			delete(a.Cfg.Versions, k)
			dropped = true
		}
	}
	if dropped {
		a.saveSettings()
	}
}

// resumeVersion is the version a resume of an item plays unasked, "" when
// nobody knows: where each version keeps its own resume point, the one in
// force while it has one (applyVersion puts the server's in force first);
// elsewhere the one recorded.
func (a *App) resumeVersion(it *jellyfin.Item) (id, why string) {
	if v := it.Version(); v != nil && v.Offset >= 0 {
		if v.Offset > 0 {
			return v.ID, "in progress"
		}
		return "", ""
	}
	if id := a.rememberedVersion(it); id != "" {
		return id, "remembered"
	}
	return "", ""
}

// firstVersion settles the version a queue's first item plays. A play from
// the start asks (the chooser), as does a resume of a version nobody knows;
// a resume of a known version, and a shuffle, do not. A listing's item has
// its record read first (readVersions). It returns how the version was
// settled ("" for an item with one), or held while the chooser is up or
// the record is read: play then starts it, told how.
func (a *App) firstVersion(it *jellyfin.Item, offset int, shuffled bool, play func(why string)) (why string, held bool) {
	if len(it.Versions) < 2 {
		return "", false
	}
	if shuffled {
		return a.quietVersion(it), false
	}
	if !it.Versions[0].Detailed() {
		a.readVersions(it, func() {
			if len(it.Versions) > 1 && !it.Versions[0].Detailed() {
				play("") // never read again: the server's first
				return
			}
			if why, held := a.firstVersion(it, offset, shuffled, play); !held {
				play(why)
			}
		})
		return "", true
	}
	resume := offset > 0 || offset < 0 && it.ViewOffset > 0
	if id, why := a.resumeVersion(it); resume && id != "" {
		a.useVersion(it, id)
		return why, false
	}
	a.askVersion(it, play)
	return "", true
}

// askVersion opens the chooser for the version to play, the one in force
// selected; a pick plays it, Back plays nothing.
func (a *App) askVersion(it *jellyfin.Item, play func(why string)) {
	cur := jellyfin.VersionIndex(it.Versions, it.VersionID)
	if cur < 0 {
		cur, _ = a.preferredVersion(it)
	}
	a.Push(NewChooser(a, a.top(), "Play which version?", jellyfin.VersionLabels(it.Versions, a.versionPlays()), cur, func(i int) {
		a.useVersion(it, it.Versions[i].ID)
		play("picked")
	}))
}

// chooseVersion opens the chooser for an item's Version row over the
// current screen: a pick puts the version in force (with its own resume
// point, where it keeps one) and records it for the next play, then done
// runs.
func (a *App) chooseVersion(it *jellyfin.Item, done func()) {
	cur := 0
	if v := it.Version(); v != nil {
		cur = max(jellyfin.VersionIndex(it.Versions, v.ID), 0)
	}
	a.Push(NewChooser(a, a.top(), "Version", jellyfin.VersionLabels(it.Versions, a.versionPlays()), cur, func(i int) {
		a.useVersion(it, it.Versions[i].ID)
		a.noteVersion(it, it.Versions[i].ID)
		done()
	}))
}

// quietVersion settles the version of a queue's later item, or a
// shuffle's, unasked (preferredVersion) and returns why ("" for an item
// with one version). A listing's item cannot be judged here: its start
// settles it from the record (versionRule).
func (a *App) quietVersion(it *jellyfin.Item) string {
	if len(it.Versions) < 2 {
		return ""
	}
	if !it.Versions[0].Detailed() {
		it.VersionID = ""
		return ""
	}
	i, why := a.preferredVersion(it)
	a.useVersion(it, it.Versions[i].ID)
	return why
}

// versionRule is what a start goes by to settle the version of an item
// whose versions the app could not judge (quietVersion): the app's records,
// taken on the UI thread (Session.settleVersion).
type versionRule struct {
	records map[string]VersionChoice // the recorded versions (versionRecord finds the item's once its versions are read)
	kind    string
	streams *StreamChoice // the tracks chosen for its movie or show, nil for none
}

// versionRule is the rule for an item's start, nil when its version is
// settled.
func (a *App) versionRule(it *jellyfin.Item) *versionRule {
	if len(it.Versions) < 2 || it.VersionID != "" {
		return nil
	}
	r := &versionRule{kind: a.versionKind(it)}
	if a.Cfg != nil {
		r.records = maps.Clone(a.Cfg.Versions)
		if ch, ok := a.Cfg.Streams[streamChoiceKey(it)]; ok {
			r.streams = &ch
		}
	}
	return r
}

// startingVersion logs the version an item starts with and how it was
// settled, and records it so a resume plays it again.
func (a *App) startingVersion(it *jellyfin.Item, why string) {
	if len(it.Versions) < 2 {
		return
	}
	a.Log.Print(versionLog(it, why))
	a.noteVersion(it, it.VersionID)
}

// versionLog is the log line of the version an item starts with.
func versionLog(it *jellyfin.Item, why string) string {
	if it.VersionID == "" {
		return fmt.Sprintf("version of %d: chosen once the start reads the record", len(it.Versions))
	}
	return versionLine(it.Versions, it.VersionID) + ", chosen: " + why
}

// versionLine names a version for the logs: "version 2 of 3 (source
// 6c936ccb) 480p h264".
func versionLine(vs []jellyfin.Version, id string) string {
	short := id[:min(len(id), 8)]
	i := jellyfin.VersionIndex(vs, id)
	if i < 0 {
		return fmt.Sprintf("version ? of %d (source %s)", len(vs), short)
	}
	return fmt.Sprintf("version %d of %d (source %s) %s", i+1, len(vs), short, vs[i].Kind())
}

// versionInfo is Playback info's Version value: "2 of 3, 480p H.264".
func versionInfo(vs []jellyfin.Version, id string) string {
	i := jellyfin.VersionIndex(vs, id)
	if i < 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d, %s", i+1, len(vs), jellyfin.VersionNames(vs)[i])
}

// versionRow is the Version action of an item whose versions are read
// ("Version: 720p H.264"), "" otherwise.
func versionRow(it *jellyfin.Item) string {
	if len(it.Versions) < 2 || !it.Versions[0].Detailed() {
		return ""
	}
	i := max(jellyfin.VersionIndex(it.Versions, it.Version().ID), 0)
	return "Version: " + jellyfin.VersionNames(it.Versions)[i]
}

// versionRead is an item whose versions a start is reading.
type versionRead struct{ it *jellyfin.Item }

// readVersions reads an item's record in the background, with the start
// indicator running and Back giving up as while a shuffle lists its
// episodes (see key), then takes it and runs then.
func (a *App) readVersions(it *jellyfin.Item, then func()) {
	client := a.Client
	if client == nil {
		then()
		return
	}
	w := &versionRead{it: it}
	a.reading, a.Starting = w, time.Now()
	a.dirty = true
	id := it.ID
	go func() {
		fresh, err := client.Item(bg(), id)
		// waits for room rather than being dropped, as a shuffle's answer does
		a.later <- func() {
			if a.reading != w {
				return // given up
			}
			a.reading, a.Starting = nil, time.Time{}
			a.dirty = true
			if err != nil {
				a.Log.Printf("versions of %s: %v", it.Title, err)
				a.Notice, a.NoticeAt = "Could not read this title's versions.", time.Now()
				return
			}
			a.takeFresh(it, fresh)
			then()
		}
		a.WakeUp()
	}()
}
