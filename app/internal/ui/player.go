package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"plexcrt/internal/jellyfin"
	"plexcrt/internal/safelog"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Player struct {
	Client                      *jellyfin.Client
	Binary, Fifo, Status, LogTo string
	// Script is retained for launcher compatibility. Access is checked before
	// opening a playback log or contacting Jellyfin.
	Script      string
	Access      func() error
	Kbps, Boost func() int
	// DirectPlay reports whether a file the decoder can play is streamed as
	// stored, read at each start (Config.DirectPlay); nil allows it. False
	// has every stream transcoded, with "Direct play off" as the reason.
	DirectPlay func() bool
	Crop       func() Crop
	Geometry   func() string
	// DrawsSubtitle reports whether the app draws a selected subtitle
	// track of an item itself (a text track: subtitles.go); the stream is
	// then asked for without subtitles. nil, or false, has the server burn
	// the track into a transcode.
	DrawsSubtitle func(itemID string, s jellyfin.Stream) bool
	Hide          func()
	exec          func(string, ...string) *exec.Cmd
	stopGrace     time.Duration // how long a worker asked to stop has before it is killed; 0 is 3 s
	reportOnce    sync.Once
	reports       chan func()
}

type Session struct {
	sourceInfo                   playbackSource
	conversionReason             string
	videoDecision, audioDecision string // server decisions for the selected streams
	videoCopy                    bool   // original video in a server-generated audio-conversion stream
	p                            *Player
	item                         *jellyfin.Item
	// rule settles the item's version once the start has read its record
	// (settleVersion), nil when the app settled it; versionWhy is how the
	// start did, versionSettled whether the version is known by now.
	rule           *versionRule
	versionWhy     string
	versionSettled bool
	// opMu serializes player lifecycle changes and status reports. mu only
	// protects fields shared with readers such as UI state.
	opMu                                   sync.Mutex
	mu                                     sync.Mutex
	cmd                                    *exec.Cmd
	cmdDone                                chan struct{}
	lf                                     *os.File
	state                                  jellyfin.PlayState
	direct, fullTimeline, started, closing bool
	pausePending                           bool
	gateway                                *jellyfin.Gateway
	url                                    string
	writeCommand                           func(string)
	Done                                   chan error
	reported                               chan struct{}
	async                                  bool
	ctx                                    context.Context
	cancel                                 context.CancelFunc
	doneOnce                               sync.Once
	uiMu                                   sync.Mutex
	uiQueue                                []string
	uiWake                                 chan struct{}
	uiStop                                 bool
	stopRequested                          bool
	seekID                                 uint64 // opMu owns the in-process seek sequence
	surroundSource                         bool
	// forceTranscode makes every later PlaybackInfo of this session ask for a
	// transcode; fellBack records that the one automatic fallback was used.
	forceTranscode, fellBack bool
	// probedTranscode is the "audio|subtitle" selection for which the
	// direct-play probe last got a transcode answer ("" for none).
	probedTranscode string
	// burned is the subtitle track the running stream has burned in, "-1"
	// for none (burnedSubtitle).
	burned string
	// kbps is the video cap More > Video bitrate chose during this session
	// (0: the Player's Kbps). It travels with the command, so the worker
	// never reads the settings the UI thread writes.
	kbps int
}

func (p *Player) Started(t time.Time) bool {
	st, err := os.Stat(p.Status)
	return err == nil && !st.ModTime().Before(t.Add(-time.Second))
}

func (p *Player) Reap() {
	entries, _ := os.ReadDir("/proc")
	owner := p.Binary
	if owner == "" {
		owner = p.Script
	}
	owner = filepath.Clean(owner)
	var owned []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		env, err := os.ReadFile("/proc/" + entry.Name() + "/environ")
		if err == nil && ownedPlayer(env, owner) {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			owned = append(owned, pid)
		}
	}
	if len(owned) != 0 {
		time.Sleep(300 * time.Millisecond)
	}
	for _, pid := range owned {
		env, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
		if err == nil && ownedPlayer(env, owner) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func ownedPlayer(env []byte, owner string) bool {
	for _, field := range strings.Split(string(env), "\x00") {
		if field == "MISTERZINE_JFPLAY_OWNER="+owner || field == "MISTERZINE_PLEX_OWNER="+owner {
			return true
		}
	}
	return false
}

func (p *Player) Start(item *jellyfin.Item, offset int) (*Session, error) {
	if p.Access != nil {
		if err := p.Access(); err != nil {
			return nil, err
		}
	}
	if p.Client == nil {
		return nil, fmt.Errorf("playback has no Jellyfin client")
	}
	if offset < 0 {
		offset = item.ViewOffset
	}
	lf, err := os.OpenFile(p.LogTo, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open playback log: %w", err)
	}
	s := &Session{p: p, item: copyPlaybackItem(item), lf: lf, Done: make(chan error, 1), reported: make(chan struct{}), ctx: context.Background(), versionSettled: true}
	if err := s.startContext(offset, false, s.ctx); err != nil {
		lf.Close()
		return nil, err
	}
	go s.watch()
	return s, nil
}

// StartAsync brings playback up in the background. It returns a session at
// once so the caller can keep drawing its starting screen while Jellyfin
// prepares a transcode or a slow server answers PlaybackInfo.
func (p *Player) StartAsync(item *jellyfin.Item, offset int) (*Session, error) {
	return p.StartAsyncSettling(item, offset, nil)
}

// StartAsyncSettling is StartAsync for an item whose version rule settles
// once the start has read its record (settleVersion); a negative offset is
// then the settled version's resume point.
func (p *Player) StartAsyncSettling(item *jellyfin.Item, offset int, rule *versionRule) (*Session, error) {
	if p.Access != nil {
		if err := p.Access(); err != nil {
			return nil, err
		}
	}
	if p.Client == nil {
		return nil, fmt.Errorf("playback has no Jellyfin client")
	}
	if offset < 0 && rule == nil {
		offset = item.ViewOffset
	}
	lf, err := os.OpenFile(p.LogTo, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open playback log: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{p: p, item: copyPlaybackItem(item), lf: lf, Done: make(chan error, 1), reported: make(chan struct{}), async: true, ctx: ctx, cancel: cancel, uiWake: make(chan struct{}, 1),
		rule: rule, versionSettled: rule == nil}
	_ = os.Remove(p.Status) // never let a canceled start consume an old session's position.
	p.startReporter()
	// Reserve the lifecycle before publishing the session to command callers.
	// An immediate seek must wait for initial startup, not launch ahead of it.
	s.opMu.Lock()
	go s.runUICommands()
	go func() {
		offset := s.settleVersion(ctx, offset)
		err := s.startContext(offset, false, ctx)
		s.opMu.Unlock()
		if err != nil {
			s.opMu.Lock()
			s.finishLocked(s.suppressStopCancellation(err))
			s.opMu.Unlock()
			return
		}
		go s.watch()
	}()
	return s, nil
}

func copyPlaybackItem(item *jellyfin.Item) *jellyfin.Item {
	copy := *item
	copy.Audio = append([]jellyfin.Stream(nil), item.Audio...)
	copy.Subs = append([]jellyfin.Stream(nil), item.Subs...)
	copy.Markers = append([]jellyfin.Marker(nil), item.Markers...)
	copy.Versions = append([]jellyfin.Version(nil), item.Versions...)
	return &copy
}

// settleVersion reads, for a start with a version rule, the item's record
// and puts in force the version the rule prefers (preferVersion): its
// source, tracks (the chosen ones marked) and, where it keeps its own,
// resume point. A record that cannot be read leaves the server's first.
// It logs the version of an item with several, and returns the offset to
// start at: a negative one is the item's resume point.
func (s *Session) settleVersion(ctx context.Context, offset int) int {
	if r := s.rule; r != nil {
		fresh, err := s.p.Client.Item(ctx, s.item.ID)
		if err != nil {
			s.logf("versions: %v", err)
		} else if len(fresh.Versions) > 1 && fresh.Versions[0].Detailed() {
			i, why := preferVersion(fresh.Versions, versionRecord(r.records, fresh), r.kind, versionPlays(s.capKbps(), s.p.directPlay()))
			fresh.UseVersion(fresh.Versions[i].ID)
			if ch := r.streams; ch != nil {
				markChoice(fresh.Audio, ch.Audio, ch.AudioLang)
				markChoice(fresh.Subs, ch.Subtitle, ch.SubtitleLang)
			}
			s.mu.Lock()
			it := s.item
			it.Versions, it.VersionID, it.SourceID, it.SourceStreams = fresh.Versions, fresh.VersionID, fresh.SourceID, fresh.SourceStreams
			it.Audio, it.Subs, it.Trickplay, it.Aspect, it.ViewOffset = fresh.Audio, fresh.Subs, fresh.Trickplay, fresh.Aspect, fresh.ViewOffset
			s.versionWhy = why
			s.mu.Unlock()
		}
		s.mu.Lock()
		s.versionSettled = true
		s.mu.Unlock()
	}
	if offset < 0 {
		offset = s.item.ViewOffset
	}
	s.logVersion()
	return offset
}

// logVersion names the version the session plays when its item has
// several, and how the start chose it when it did.
func (s *Session) logVersion() {
	s.mu.Lock()
	vs, id, why := s.item.Versions, s.item.VersionID, s.versionWhy
	s.mu.Unlock()
	if len(vs) < 2 {
		return
	}
	if id == "" {
		s.logf("version of %d: the server's first", len(vs))
		return
	}
	line := versionLine(vs, id)
	if why != "" {
		line += ", chosen: " + why
	}
	s.logf("%s", line)
}

// Version is the version the session plays (jellyfin.Version.ID), and
// whether it is settled: an item whose start settles it (settleVersion)
// has none until then, and "" for good when its record could not be read.
func (s *Session) Version() (id string, settled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.item.VersionID, s.versionSettled
}

// Versions are the versions of the session's item, as its start read them.
func (s *Session) Versions() []jellyfin.Version {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]jellyfin.Version(nil), s.item.Versions...)
}

// capKbps is the video cap a start asks for: More's choice for this
// session, else the setting.
func (s *Session) capKbps() int {
	kbps := 0
	if s.p.Kbps != nil {
		kbps = s.p.Kbps()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.kbps > 0 {
		kbps = s.kbps
	}
	return kbps
}

func (p *Player) startReporter() {
	p.reportOnce.Do(func() {
		p.reports = make(chan func(), 64)
		go func() {
			for report := range p.reports {
				report()
			}
		}()
	})
}

func (p *Player) reportAsync(report func(), progress bool) {
	if progress {
		select {
		case p.reports <- report:
		default: // UI position updates may be skipped when the server is slow.
		}
		return
	}
	// Lifecycle reports are rare and must stay ordered. This send runs on a
	// session worker, never on the render/input goroutine.
	p.reports <- report
}

func selectedStreams(item *jellyfin.Item) (audio, subtitle string) {
	for _, v := range item.Audio {
		if v.Selected {
			audio = v.ID
			break
		}
	}
	subtitle = "-1"
	for _, v := range item.Subs {
		if v.Selected {
			subtitle = v.ID
			break
		}
	}
	return
}

// burnedSubtitle is the subtitle track the server is to burn into the
// stream for the selected one: "-1" for none, and for a track the app
// draws itself, which leaves the stream free to play as stored.
func (s *Session) burnedSubtitle(selected string) string {
	if selected == "-1" || s.p.DrawsSubtitle == nil {
		return selected
	}
	for _, st := range s.item.Subs {
		if st.ID == selected && s.p.DrawsSubtitle(s.item.ID, st) {
			return "-1"
		}
	}
	return selected
}

func (s *Session) startContext(offset int, reuseDirect bool, ctx context.Context) error {
	p := s.p
	// A replacement worker should inherit the session's pause state. A newly
	// created session starts with the zero value, so this also leaves startup
	// behavior unchanged.
	s.mu.Lock()
	paused := s.state.Paused
	s.mu.Unlock()
	audio, subtitle := selectedStreams(s.item)
	base, inFile := offset, 0
	if !reuseDirect {
		kbps := s.capKbps()
		s.mu.Lock()
		force := s.forceTranscode || !p.directPlay()
		s.mu.Unlock()
		burn := s.burnedSubtitle(subtitle)
		info, err := s.playbackInfo(ctx, kbps, offset, audio, burn, force)
		if err != nil {
			return err
		}
		src := info.MediaSources[0]
		s.surroundSource = mediaSourceSurround(src, s.item, audio)
		if burn != "-1" && src.TranscodingURL == "" {
			return fmt.Errorf("Jellyfin did not offer a subtitle transcode; check this user's transcoding permissions")
		}
		s.mu.Lock() // Direct reads it from the UI thread
		s.direct = (src.SupportsDirectPlay || src.SupportsDirectStream) && src.TranscodingURL == ""
		s.videoCopy = src.VideoCopy
		s.sourceInfo = playbackSource{}
		for _, st := range s.item.SourceStreams {
			if st.Type == "Video" {
				s.sourceInfo.Video = videoDescription(st.Codec, st.Width, st.Height)
			}
			if st.Type == "Audio" && ((audio != "" && strconv.Itoa(st.Index) == audio) || (audio == "" && st.IsDefault)) {
				s.sourceInfo.Audio = audioDescription(st.Codec, st.Channels)
			}
		}
		s.videoDecision, s.audioDecision = streamDecisions(src.TranscodingURL)
		s.conversionReason = jellyfinConversionReason(src.TranscodingURL)
		if s.direct {
			s.conversionReason = ""
		} else if burn != "-1" {
			s.conversionReason = "Subtitle burn-in"
		} else if force && !p.directPlay() {
			s.conversionReason = "Direct play off"
		} else if force {
			s.conversionReason = "Decoder compatibility"
		} else if src.VideoCopy && (s.conversionReason == "" || s.conversionReason == "Server policy") {
			s.conversionReason = "Audio compatibility"
		}
		s.mu.Unlock()
		s.fullTimeline = !s.direct && hasFullTimelineHLS(src.TranscodingProtocol, src.TranscodingURL)
		if s.direct {
			s.url = p.Client.DirectStreamURL(s.item.ID, src, info.PlaySessionID)
		} else {
			if src.TranscodingURL == "" {
				return fmt.Errorf("Jellyfin offered no transcode URL")
			}
			s.url = p.Client.StreamURL(src.TranscodingURL)
		}
		// the report names the selected subtitle, drawn or burned in
		s.state = jellyfin.PlayState{ItemID: s.item.ID, MediaSourceID: src.ID, PlaySessionID: info.PlaySessionID, Position: float64(offset), AudioStream: audio, SubtitleStream: subtitle, PlayMethod: "Transcode", Paused: paused}
		s.burned = burn
		if s.direct {
			s.state.PlayMethod = "DirectStream"
		}
	}
	if s.direct || s.fullTimeline {
		base, inFile = 0, offset
	}
	args := []string{"--base", strconv.Itoa(base)}
	if inFile > 0 {
		args = append(args, "--start-in-file", strconv.Itoa(inFile))
	}
	if s.direct && audio != "" {
		// Jellyfin's MediaStreams.Index is the absolute stream index in the
		// source, which jfplay accepts as --audio for a direct stream.
		index, err := strconv.Atoi(audio)
		if err != nil || index < 0 {
			return fmt.Errorf("invalid Jellyfin audio stream index %q", audio)
		}
		args = append(args, "--audio", strconv.Itoa(index))
	}
	if paused {
		args = append(args, "--hold")
	}
	if s.direct || s.videoCopy {
		// jfplay checks the stream before the first picture and watches that
		// the core keeps up; it exits 7 when a transcode is needed.
		args = append(args, "--direct")
	} else if base > 0 {
		// a transcode started at an offset: jfplay exits 8 when the server
		// sends nothing in time (openTimeoutError)
		args = append(args, "--open-timeout", strconv.Itoa(int(openTimeout/time.Second)))
	}
	args = append(args, "--fifo", p.Fifo, "--status", p.Status)
	if p.Boost != nil {
		if v := p.Boost(); v >= 100 && v <= 300 {
			args = append(args, "--audio-boost", strconv.Itoa(v))
		}
	}
	if !s.direct && s.surroundSource {
		args = append(args, "--surround-source")
	}
	if p.Crop != nil {
		args = append(args, "--analog-aspect", strconv.Itoa(p.Crop().analogAspect()))
	}
	if p.Geometry != nil {
		if geometry := p.Geometry(); geometry != "" {
			args = append(args, "--geometry", geometry)
		}
	}
	if s.gateway != nil {
		s.gateway.Close()
		s.gateway = nil
	}
	gateway, err := p.Client.NewGateway(ctx, p.Binary)
	if err != nil {
		return err
	}
	workerURL, err := gateway.URL(s.url)
	if err != nil {
		gateway.Close()
		return err
	}
	s.gateway = gateway
	launched := false
	defer func() {
		if !launched {
			gateway.Close()
		}
	}()
	args = append(args, "--gateway", workerURL)
	_ = os.Remove(p.Status)
	run := p.exec
	if run == nil {
		run = exec.Command
	}
	cmd := run(p.Binary, args...)
	output := &safelog.Writer{Out: s.lf, Secrets: []string{p.Client.Store.Credentials().Token}}
	cmd.Stdout, cmd.Stderr = output, output
	// Credentials stay in the gateway. Clear any inherited legacy header
	// variable so the worker cannot accidentally forward an old token.
	cmd.Env = append(os.Environ(), "MISTERZINE_JFPLAY_OWNER="+filepath.Clean(p.Binary),
		"JFPLAY_HEADERS=")
	s.mu.Lock()
	if ctx.Err() != nil || s.closing || s.stopRequested {
		s.mu.Unlock()
		return context.Canceled
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return err
	}
	launched = true
	s.cmd = cmd
	done := make(chan struct{})
	s.cmdDone = done
	s.mu.Unlock()
	go func() {
		err := cmd.Wait()
		output.Flush()
		close(done)
		s.opMu.Lock()
		defer s.opMu.Unlock()
		s.mu.Lock()
		current, closing := s.cmd == cmd, s.closing
		s.mu.Unlock()
		if current && !closing {
			if s.fallbackLocked(err) {
				return
			}
			s.finishLocked(s.openTimeoutError(err))
		}
	}()
	return nil
}

func mediaSourceSurround(src jellyfin.MediaSource, item *jellyfin.Item, selected string) bool {
	index := -1
	if selected != "" {
		index, _ = strconv.Atoi(selected)
	} else if src.DefaultAudioStream != nil {
		index = *src.DefaultAudioStream
	}
	// PlaybackInfo may describe the planned stereo output. Item metadata
	// retains the source channel count used by the optional surround boost.
	if src.VideoCopy {
		for _, stream := range item.Audio {
			if stream.ID == strconv.Itoa(index) && stream.Channels > 0 {
				return stream.Channels > 2
			}
		}
	}
	for _, stream := range src.MediaStreams {
		if strings.EqualFold(stream.Type, "Audio") && stream.Index == index && stream.Channels > 0 {
			return stream.Channels > 2
		}
	}
	for _, stream := range item.Audio {
		if (selected != "" && stream.ID == selected) || (selected == "" && stream.Selected) {
			return stream.Channels > 2
		}
	}
	return false
}

func hasFullTimelineHLS(protocol, transcodeURL string) bool {
	if strings.EqualFold(strings.TrimSpace(protocol), "hls") {
		return true
	}
	u, err := url.Parse(transcodeURL)
	return err == nil && strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

// playbackPriority puts the calling thread at the ordinary priority for a
// playback and returns what puts it back. Run raises its locked thread
// (nice -5) for the menus, and the play loop runs on that thread: there it
// would outrank jfplay (nice 0), which feeds the decoder. A thread already
// at 0 (a test's, not locked to its goroutine) is left as it is.
func playbackPriority() (restore func()) {
	prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0) // 20 - nice
	if err != nil || prio == 20 || syscall.Setpriority(syscall.PRIO_PROCESS, 0, 0) != nil {
		return func() {}
	}
	return func() { syscall.Setpriority(syscall.PRIO_PROCESS, 0, 20-prio) }
}

// readPlayerStatus is the worker's status file as keys and values, nil
// when there is none. The map is shared (playerStatus): read it only.
func readPlayerStatus(path string) map[string]string {
	values, _, err := playerStatus.read(path)
	if err != nil {
		return nil
	}
	return values
}

// playerStatus is the worker's status file as last read. The play loop
// asks for it ten times a second and the session four times, but jfplay
// writes it every 250 ms, each time whole into a new file renamed over
// the old: the same file (inode, size and time) has the same contents.
var playerStatus statusFiles

// statusSettle is how long after its time a file is taken as written: one
// read sooner might share a timestamp with a write still to come, were
// the file ever rewritten in place.
const statusSettle = 50 * time.Millisecond

type statusFiles struct {
	mu    sync.Mutex
	files map[string]statusFile
}

type statusFile struct {
	ino     uint64
	size    int64
	written time.Time
	values  map[string]string
}

// read is the status file at path, parsed, and when it was written; the
// file is read and parsed again only when it has changed since.
func (c *statusFiles) read(path string) (map[string]string, time.Time, error) {
	st, err := os.Stat(path)
	if err != nil {
		c.mu.Lock()
		delete(c.files, path)
		c.mu.Unlock()
		return nil, time.Time{}, err
	}
	c.mu.Lock()
	cached, ok := c.files[path]
	c.mu.Unlock()
	if ok && cached.ino == statusInode(st) && cached.size == st.Size() && cached.written.Equal(st.ModTime()) {
		return cached.values, cached.written, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	if st, err = f.Stat(); err != nil {
		return nil, time.Time{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, time.Time{}, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			values[k] = v
		}
	}
	file := statusFile{ino: statusInode(st), size: st.Size(), written: st.ModTime(), values: values}
	c.mu.Lock()
	if time.Since(file.written) > statusSettle {
		if c.files == nil {
			c.files = map[string]statusFile{}
		}
		c.files[path] = file
	} else {
		delete(c.files, path)
	}
	c.mu.Unlock()
	return values, file.written, nil
}

func statusInode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

func (s *Session) update(report bool) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.updateLocked(report)
}

func (s *Session) updateLocked(report bool) {
	st := readPlayerStatus(s.p.Status)
	if st == nil {
		return
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	if p, e := strconv.ParseFloat(st["position"], 64); e == nil {
		s.state.Position = p
	}
	statusPaused := st["state"] == "paused"
	if s.pausePending {
		if statusPaused == s.state.Paused {
			s.pausePending = false // the worker has caught up with the last command
		}
	} else {
		s.state.Paused = statusPaused
	}
	state, started, ps := st["state"], s.started, s.state
	if state == "playing" && !s.started {
		s.started = true
	}
	s.mu.Unlock()
	if state == "playing" && !started {
		s.reportStart(ps)
	} else if report && started {
		s.reportProgress(ps)
	}
}

func (s *Session) reportStart(ps jellyfin.PlayState) {
	if s.async {
		s.p.reportAsync(func() { _ = s.p.Client.ReportStart(context.Background(), ps) }, false)
	} else {
		_ = s.p.Client.ReportStart(context.Background(), ps)
	}
}

func (s *Session) reportProgress(ps jellyfin.PlayState) {
	if s.async {
		s.p.reportAsync(func() { _ = s.p.Client.ReportProgress(context.Background(), ps) }, true)
	} else {
		_ = s.p.Client.ReportProgress(context.Background(), ps)
	}
}

func (s *Session) watch() {
	tick := time.NewTicker(250 * time.Millisecond)
	progress := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	defer progress.Stop()
	for {
		select {
		case <-tick.C:
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing {
				return
			}
			s.update(false)
		case <-progress.C:
			s.update(true)
		}
	}
}

func (s *Session) write(line string) {
	if s.writeCommand != nil {
		s.writeCommand(line)
		return
	}
	f, err := os.OpenFile(s.p.Fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		_ = f.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
		_, _ = fmt.Fprintln(f, line)
		_ = f.Close()
	}
}

func (s *Session) Send(line string) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	closing := s.closing || s.stopRequested
	s.mu.Unlock()
	if closing {
		return
	}
	fields := strings.Fields(line)
	if len(fields) == 2 && fields[0] == "boost" {
		if gain, err := strconv.Atoi(fields[1]); err == nil && gain >= 100 && gain <= 300 {
			s.write("boost " + strconv.Itoa(gain))
		}
		return
	}
	if len(fields) == 2 && fields[0] == "seek" {
		target, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || target < 0 || math.IsNaN(target) || math.IsInf(target, 0) {
			return
		}
		s.updateLocked(false)
		s.mu.Lock()
		s.state.Position = target
		ps := s.state
		s.mu.Unlock()
		s.reportProgress(ps)
		if s.tryWorkerSeek(target) {
			return
		}
		if s.sessionContext().Err() != nil {
			return
		}
		s.stopProcessLocked()
		_ = os.Remove(s.p.Status) // retire the old position before a slow restart.
		s.mu.Lock()
		reuse := s.direct
		s.started = false
		s.mu.Unlock()
		err = s.startContext(int(target), reuse, s.sessionContext())
		if err != nil {
			s.finishLocked(s.suppressStopCancellation(err))
		}
		return
	}
	if len(fields) == 3 && fields[0] == "bitrate" {
		// More > Video bitrate: a new PlaybackInfo with the new cap, at
		// the position the UI had, the tracks as they are
		kbps, e1 := strconv.Atoi(fields[1])
		target, e2 := strconv.ParseFloat(fields[2], 64)
		if e1 != nil || e2 != nil || kbps <= 0 || !validPosition(target) {
			return
		}
		s.mu.Lock()
		s.kbps = kbps
		s.mu.Unlock()
		s.redecideLocked(target)
		return
	}
	if len(fields) == 2 && fields[0] == "restart" {
		// More > Direct play: a new PlaybackInfo under the settings as they
		// are now, at the position the UI had
		target, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || !validPosition(target) {
			return
		}
		s.redecideLocked(target)
		return
	}
	if len(fields) == 4 && fields[0] == "tracks" {
		target, err := strconv.ParseFloat(fields[3], 64)
		if err != nil || target < 0 || math.IsNaN(target) || math.IsInf(target, 0) || !validStreamID(fields[1]) || !validStreamID(fields[2]) {
			return
		}
		s.updateLocked(false)
		s.item.Audio = setStreamID(s.item.Audio, fields[1])
		s.item.Subs = setStreamID(s.item.Subs, fields[2])
		audio := fields[1]
		if audio == "-1" {
			audio = ""
		}
		burn := s.burnedSubtitle(fields[2])
		s.mu.Lock()
		// The stream as it runs serves the new choice when the sound is the
		// same and so is what it has burned in: only a subtitle the app
		// draws changed (or one it draws went off).
		keep := s.cmd != nil && audio == s.state.AudioStream && burn == s.burned
		if !keep {
			s.state.Position = target
		}
		s.state.AudioStream = audio
		s.state.SubtitleStream = fields[2]
		ps := s.state
		s.mu.Unlock()
		s.reportProgress(ps)
		if keep {
			return
		}
		s.stopProcessLocked()
		_ = os.Remove(s.p.Status)
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		// A track choice needs a new PlaybackInfo response even for direct
		// streams; that response selects the server stream and may change URL.
		if err := s.startContext(int(target), false, s.sessionContext()); err != nil {
			s.finishLocked(s.suppressStopCancellation(err))
		}
		return
	}
	if line == "toggle" || line == "pause" || line == "resume" || line == "stop" || strings.HasPrefix(line, "aspect ") {
		if line == "stop" || strings.HasPrefix(line, "aspect ") {
			s.write(line)
			return
		}
		// Read the current status before sending toggle: the child may update
		// its status as soon as it receives the command.
		s.updateLocked(false)
		s.mu.Lock()
		if line == "pause" {
			s.state.Paused = true
		} else if line == "resume" {
			s.state.Paused = false
		} else {
			s.state.Paused = !s.state.Paused
		}
		s.pausePending = true
		ps := s.state
		s.mu.Unlock()
		s.write(line)
		s.reportProgress(ps)
	}
}

// tryWorkerSeek keeps the open media session when the worker advertises support.
// A bounded acknowledgement wait runs on the command worker, never the UI loop.
// Old workers and failed seeks use the existing process-restart path.
func (s *Session) tryWorkerSeek(target float64) bool {
	if !s.direct && !s.fullTimeline {
		return false // an offset transcode cannot seek outside its source window.
	}
	status := readPlayerStatus(s.p.Status)
	if status["seek_supported"] != "1" {
		return false
	}
	s.seekID++
	id := strconv.FormatUint(s.seekID, 10)
	s.write("seek " + id + " " + strconv.FormatFloat(target, 'f', 3, 64))
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	s.mu.Lock()
	done := s.cmdDone
	s.mu.Unlock()
	for {
		select {
		case <-s.sessionContext().Done():
			return false
		case <-done:
			return false
		case <-timer.C:
			return false
		case <-poll.C:
			status = readPlayerStatus(s.p.Status)
			if status["seek_id"] != id {
				continue
			}
			switch status["seek_state"] {
			case "ready":
				return true
			case "error":
				return false
			}
		}
	}
}

func validStreamID(id string) bool {
	if id == "-1" {
		return true
	}
	index, err := strconv.Atoi(id)
	return err == nil && index >= 0
}

func setStreamID(streams []jellyfin.Stream, id string) []jellyfin.Stream {
	found := id == "-1"
	for i := range streams {
		streams[i].Selected = id != "-1" && streams[i].ID == id
		found = found || streams[i].Selected
	}
	if id != "-1" && !found {
		streams = append(streams, jellyfin.Stream{ID: id, Selected: true})
	}
	return streams
}

func (s *Session) sessionContext() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// StopUI cancels startup and replacement requests. Treat only that explicit
// cancellation as a clean end; other startup errors still reach the caller.
func (s *Session) suppressStopCancellation(err error) error {
	if !errors.Is(err, context.Canceled) {
		return err
	}
	s.mu.Lock()
	stopRequested := s.stopRequested
	s.mu.Unlock()
	if stopRequested {
		return nil
	}
	return err
}

// ReportsDone closes after this session's stopped report has been sent.
// UI refreshes can wait in the background before reading resume metadata.
func (s *Session) ReportsDone() <-chan struct{} { return s.reported }

// FinalPosition is where the playback stood when it was last read, in
// seconds: once Done has fired, the position its stopped report carries.
// False when the playback never started playing.
func (s *Session) FinalPosition() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Position, s.started
}

// directPlay is the DirectPlay setting; nil allows it.
func (p *Player) directPlay() bool { return p.DirectPlay == nil || p.DirectPlay() }

func validPosition(target float64) bool {
	return target >= 0 && !math.IsNaN(target) && !math.IsInf(target, 0)
}

// redecideLocked restarts the stream at target with a new PlaybackInfo (the
// cap, the Direct play setting and the tracks as they are now); opMu is
// held. A session past its end is left alone.
func (s *Session) redecideLocked(target float64) {
	s.logf("restarting at %.0f s for a new decision (direct play %s)", target, map[bool]string{true: "on", false: "off"}[s.p.directPlay()])
	s.updateLocked(false)
	s.mu.Lock()
	s.state.Position = target
	ps := s.state
	s.mu.Unlock()
	s.reportProgress(ps)
	if s.sessionContext().Err() != nil {
		return
	}
	s.stopProcessLocked()
	_ = os.Remove(s.p.Status)
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
	if err := s.startContext(int(target), false, s.sessionContext()); err != nil {
		s.finishLocked(s.suppressStopCancellation(err))
	}
}

// Direct reports whether the running stream is the stored file, untranscoded:
// then the player's duration is exact. It changes on a fallback to a transcode.
func (s *Session) Direct() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.direct
}

// SendUI queues a command for the session worker and returns immediately.
// Repeated seeks coalesce to the newest target, keeping rapid input bounded.
func (s *Session) SendUI(line string) {
	if !s.async {
		s.Send(line)
		return
	}
	fields := strings.Fields(line)
	seek := len(fields) == 2 && fields[0] == "seek"
	s.uiMu.Lock()
	if s.uiStop {
		s.uiMu.Unlock()
		return
	}
	if seek && len(s.uiQueue) > 0 && strings.HasPrefix(s.uiQueue[len(s.uiQueue)-1], "seek ") {
		s.uiQueue[len(s.uiQueue)-1] = line
		s.uiMu.Unlock()
		s.signalUI()
		return
	}
	if len(s.uiQueue) >= 8 {
		if seek {
			s.uiMu.Unlock()
			return
		}
		for i, queued := range s.uiQueue {
			if strings.HasPrefix(queued, "seek ") {
				s.uiQueue = append(s.uiQueue[:i], s.uiQueue[i+1:]...)
				break
			}
		}
		if len(s.uiQueue) >= 8 {
			s.uiMu.Unlock()
			return
		}
	}
	s.uiQueue = append(s.uiQueue, line)
	s.uiMu.Unlock()
	s.signalUI()
}

func (s *Session) StopUI() {
	if !s.async {
		s.Stop()
		return
	}
	s.mu.Lock()
	s.stopRequested = true // prevents a pending seek from starting a replacement.
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.uiMu.Lock()
	s.uiStop = true
	s.uiQueue = nil
	s.uiMu.Unlock()
	s.signalUI()
}

func (s *Session) signalUI() {
	select {
	case s.uiWake <- struct{}{}:
	default:
	}
}

func (s *Session) runUICommands() {
	for range s.uiWake {
		for {
			s.uiMu.Lock()
			if s.uiStop {
				s.uiMu.Unlock()
				s.Stop()
				return
			}
			if len(s.uiQueue) == 0 {
				s.uiMu.Unlock()
				break
			}
			line := s.uiQueue[0]
			s.uiQueue = s.uiQueue[1:]
			s.uiMu.Unlock()
			s.Send(line)
		}
	}
}

func (s *Session) stopProcessLocked() {
	if s.gateway != nil {
		s.gateway.Close()
		s.gateway = nil
	}
	s.mu.Lock()
	cmd := s.cmd
	done := s.cmdDone
	s.cmd = nil
	s.cmdDone = nil
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	s.write("stop")
	if done == nil {
		return
	}
	grace := s.p.stopGrace
	if grace <= 0 {
		grace = 3 * time.Second
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}
	_ = cmd.Process.Kill()
	<-done
}

func (s *Session) finishLocked(err error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.updateLocked(false)
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	ps := s.state
	s.mu.Unlock()
	if s.async {
		s.uiMu.Lock()
		s.uiStop = true
		s.uiQueue = nil
		s.uiMu.Unlock()
		s.signalUI() // retire the command worker on natural exit and startup failure.
	}
	if s.async {
		// Queue the terminal report before releasing Done so a subsequent
		// session's start report cannot overtake it.
		if ps.PlaySessionID != "" {
			s.p.reportAsync(func() {
				_ = s.p.Client.ReportStopped(context.Background(), ps)
				close(s.reported)
			}, false)
		} else {
			close(s.reported)
		}
	} else {
		_ = s.p.Client.ReportStopped(context.Background(), ps)
		close(s.reported)
	}
	if s.gateway != nil {
		s.gateway.Close()
		s.gateway = nil
	}
	s.mu.Lock()
	stopped := s.stopRequested
	s.mu.Unlock()
	if err != nil && !stopped {
		s.preserveFailureLog()
	}
	_ = s.lf.Close()
	s.doneOnce.Do(func() { s.Done <- err })
}

// Keep a bounded snapshot before the next playback truncates the live log.
// Read through the session's descriptor so another start cannot substitute a
// different file. The worker has exited and flushed its output at this point.
func (s *Session) preserveFailureLog() {
	if s.lf == nil {
		return
	}
	const limit = 64 * 1024
	info, err := s.lf.Stat()
	if err != nil {
		return
	}
	start := info.Size() - limit
	if start < 0 {
		start = 0
	}
	data := make([]byte, info.Size()-start)
	n, err := s.lf.ReadAt(data, start)
	if err != nil && err != io.EOF {
		return
	}
	_ = os.WriteFile(s.p.LogTo+".failed", data[:n], 0o600)
}

func (s *Session) Stop() {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return
	}
	s.stopProcessLocked()
	s.finishLocked(nil)
}

// StreamModes keeps video and audio conversion separate, even in a remuxed stream.
func (s *Session) StreamModes() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.direct {
		return "Original", "Original"
	}
	video, audio := conversionLabel(s.videoDecision), conversionLabel(s.audioDecision)
	if s.videoCopy {
		video = "Original"
	}
	return video, audio
}

func conversionLabel(decision string) string {
	switch strings.ToLower(decision) {
	case "copy", "directplay", "directstream":
		return "Original"
	case "transcode":
		return "Transcoded"
	}
	return ""
}

// Read only the codec choices; never expose a playback URL or its credentials.
func streamDecisions(raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	choices := map[string]string{}
	for key, values := range u.Query() {
		if len(values) > 0 {
			choices[strings.ToLower(key)] = values[0]
		}
	}
	mode := func(codec string) string {
		if codec == "" {
			return ""
		}
		if strings.EqualFold(codec, "copy") {
			return "copy"
		}
		return "transcode"
	}
	return mode(choices["videocodec"]), mode(choices["audiocodec"])
}

func jellyfinConversionReason(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	reasons := ""
	for key, values := range u.Query() {
		if strings.EqualFold(key, "TranscodeReasons") && len(values) > 0 {
			reasons = values[0]
		}
	}
	names := map[string]string{
		"VideoCodecNotSupported":       "Video codec",
		"AudioCodecNotSupported":       "Audio codec",
		"AudioChannelsNotSupported":    "Audio channels",
		"VideoResolutionNotSupported":  "Video resolution",
		"VideoBitrateNotSupported":     "Video bitrate",
		"ContainerBitrateExceedsLimit": "Stream bitrate",
		"VideoLevelNotSupported":       "Video level",
		"VideoProfileNotSupported":     "Video profile",
		"VideoBitDepthNotSupported":    "Video bit depth",
		"SubtitleCodecNotSupported":    "Subtitle burn-in",
	}
	var found []string
	for _, reason := range strings.Split(reasons, ",") {
		if name := names[strings.TrimSpace(reason)]; name != "" {
			duplicate := false
			for _, old := range found {
				if old == name {
					duplicate = true
				}
			}
			if !duplicate {
				found = append(found, name)
			}
		}
	}
	if len(found) > 0 {
		return strings.Join(found, "; ")
	}
	if raw != "" {
		return "Server policy"
	}
	return ""
}

func (s *Session) ConversionReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.direct {
		return ""
	}
	return s.conversionReason
}
