package jellyfin

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Version is one of an item's versions: a media source of its record (a
// file). Jellyfin 12 and later keep each version's resume point on the
// version's own item, whose id is the source's, and list the last one
// played first while it has a point.
type Version struct {
	ID        string      // the source's id: the version the app records, "" in a listing's record
	Label     string      // the server's name for it (the folder's, or the file name's own part)
	Source    MediaSource // its streams and bitrate, as the direct-play check judges a source
	Aspect    float64
	Codec     string // video codec: h264, hevc, ...
	Width     int
	Height    int
	File      string // the file's name, for telling apart versions that read the same
	Audio     []Stream
	Subs      []Stream
	Trickplay *Trickplay
	// Offset is the version's own resume point in seconds where the server
	// keeps one per version (12 and later: Client.Item), -1 where it keeps
	// one for the item or the point is not known.
	Offset int
}

// Detailed reports whether the record carried the version (an item's own
// record does; a listing's only counts them): labels and picks need it.
func (v Version) Detailed() bool { return v.ID != "" }

// versions lists the record's versions, nil when it has fewer than two: in
// full from its media sources, else (a listing's record) as many blanks as
// the server counts, which only say there is a choice to make.
func (x *dto) versions() []Version {
	if len(x.MediaSources) < 2 {
		if len(x.MediaSources) == 0 && x.MediaSourceCount > 1 {
			out := make([]Version, x.MediaSourceCount)
			for i := range out {
				out[i].Offset = -1
			}
			return out
		}
		return nil
	}
	out := make([]Version, 0, len(x.MediaSources))
	for i, ms := range x.MediaSources {
		v := Version{ID: ms.ID, Label: Fold(ms.Name), File: baseName(ms.Path), Offset: -1,
			Source: MediaSource{ID: ms.ID, MediaStreams: ms.MediaStreams, Container: ms.Container, Bitrate: ms.Bitrate}}
		v.Aspect, v.Audio, v.Subs = tracks(ms.MediaStreams)
		if s := v.video(); s != nil {
			v.Codec, v.Width, v.Height = strings.ToLower(s.Codec), s.Width, s.Height
		}
		// the first takes the item's sheets, as the record has it in force;
		// another only its own
		if _, own := x.Trickplay[ms.ID]; own || i == 0 && len(x.Trickplay) > 0 {
			v.Trickplay = trickplay(x.Trickplay, ms.ID)
		}
		out = append(out, v)
	}
	return out
}

func (v Version) video() *MediaStream {
	for i := range v.Source.MediaStreams {
		if strings.EqualFold(v.Source.MediaStreams[i].Type, "Video") {
			return &v.Source.MediaStreams[i]
		}
	}
	return nil
}

// baseName is a server path's last element, for Windows servers too.
func baseName(p string) string {
	return p[strings.LastIndexAny(p, `/\`)+1:]
}

// Version is the chosen version, nil for an item with one.
func (it *Item) Version() *Version {
	if len(it.Versions) == 0 {
		return nil
	}
	for i := range it.Versions {
		if it.Versions[i].ID == it.VersionID {
			return &it.Versions[i]
		}
	}
	return &it.Versions[0]
}

// UseVersion makes the version with an ID the chosen one: the item's
// source, its streams, preview sheets, aspect and tracks become that
// version's (its server defaults marked), and where the version keeps its
// own resume point, so does the item. It reports whether the item has that
// version.
func (it *Item) UseVersion(id string) bool {
	for i := range it.Versions {
		v := &it.Versions[i]
		if id == "" || v.ID != id {
			continue
		}
		it.VersionID, it.SourceID, it.Trickplay = id, v.ID, v.Trickplay
		it.SourceStreams = append([]MediaStream(nil), v.Source.MediaStreams...)
		if v.Aspect != 0 {
			it.Aspect = v.Aspect
		}
		it.Audio = append([]Stream(nil), v.Audio...)
		it.Subs = append([]Stream(nil), v.Subs...)
		if v.Offset >= 0 {
			it.ViewOffset = v.Offset
		}
		return true
	}
	return false
}

// VersionIndex is the place of the version with an ID, -1 for none.
func VersionIndex(vs []Version, id string) int {
	for i := range vs {
		if id != "" && vs[i].ID == id {
			return i
		}
	}
	return -1
}

// ResumeVersion is the first version with its own resume point (the server
// lists the last one played first), -1 for none and where the server keeps
// one point for the item.
func ResumeVersion(vs []Version) int {
	for i := range vs {
		if vs[i].Offset > 0 {
			return i
		}
	}
	return -1
}

// class is the version's picture class in lines: a 1280x536 scope film is
// a 720 like a 1280x720 one.
func (v Version) class() int {
	return max(v.Height, v.Width*9/16)
}

func (v Version) pixels() int { return v.Width * v.Height }

// resolutionName is the class as people say it: 1080p, 720p, 4K.
func (v Version) resolutionName() string {
	switch c := v.class(); {
	case c >= 2000:
		return "4K"
	case c > 0:
		return strconv.Itoa(c) + "p"
	}
	return ""
}

// codecNames are the video codecs as the chooser names them.
var codecNames = map[string]string{"h264": "H.264", "hevc": "HEVC", "h265": "HEVC", "av1": "AV1", "vp9": "VP9",
	"mpeg2video": "MPEG-2", "mpeg4": "MPEG-4", "msmpeg4v3": "DivX", "vc1": "VC-1", "wmv3": "WMV"}

func (v Version) codecName() string {
	if n := codecNames[v.Codec]; n != "" {
		return n
	}
	return strings.ToUpper(v.Codec)
}

// Kind is what a show's episodes match a pick by: class and codec,
// "720p h264".
func (v Version) Kind() string {
	return strings.TrimSpace(v.resolutionName() + " " + v.Codec)
}

// Name is the version as its Version row names it: "720p H.264".
func (v Version) Name() string {
	return strings.TrimSpace(v.resolutionName() + " " + v.codecName())
}

// label is the version's line: "720p H.264, 1.2 Mbps, plays directly".
func (v Version) label(plays bool) string {
	var parts []string
	if s := v.Name(); s != "" {
		parts = append(parts, s)
	}
	if b := v.Source.Bitrate; b >= 1_000_000 {
		parts = append(parts, fmt.Sprintf("%.1f Mbps", float64(b)/1e6))
	} else if b >= 1000 {
		parts = append(parts, fmt.Sprintf("%d kbps", b/1000))
	}
	if plays {
		parts = append(parts, "plays directly")
	} else {
		parts = append(parts, "server converts")
	}
	return strings.Join(parts, ", ")
}

// VersionLabels are the chooser's lines for versions, plays saying which
// play as stored (the app's direct-play check at the user's cap), told
// apart where two read the same (apart).
func VersionLabels(vs []Version, plays func(Version) bool) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.label(plays(v))
	}
	return apart(vs, out)
}

// VersionNames are the versions' names (Name), told apart where two read
// the same (apart).
func VersionNames(vs []Version) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name()
	}
	return apart(vs, out)
}

// apart tells apart the versions' lines that read the same by their files'
// names (the part the names do not share: "Director's Cut"), or the
// server's names for them when it gives no paths, else by their place.
func apart(vs []Version, out []string) []string {
	count := map[string]int{}
	for _, s := range out {
		count[s]++
	}
	names := make([]string, len(vs))
	files := true
	for i, v := range vs {
		names[i] = strings.TrimSuffix(v.File, extension(v.File))
		files = files && names[i] != ""
	}
	if !files {
		for i, v := range vs {
			names[i] = v.Label
		}
	}
	pre, suf := commonEnds(names)
	for i := range vs {
		if count[out[i]] < 2 {
			continue
		}
		own := names[i]
		if len(own) >= pre+suf {
			own = own[pre : len(own)-suf]
		}
		own = ownPart(Fold(own))
		if own == "" {
			own = fmt.Sprintf("version %d", i+1)
		}
		if out[i] == "" {
			out[i] = own
		} else {
			out[i] += " - " + own
		}
	}
	return out
}

// ownPart is a name's own part without the separators around it and the
// brackets that enclose it: " - (Director's Cut)" is "Director's Cut".
func ownPart(s string) string {
	s = strings.Trim(s, " -_.")
	for len(s) >= 2 && (s[0] == '(' && s[len(s)-1] == ')' || s[0] == '[' && s[len(s)-1] == ']') {
		s = strings.Trim(s[1:len(s)-1], " -_.")
	}
	return s
}

func extension(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i:]
	}
	return ""
}

// commonEnds is the length of the prefix and of the suffix all names share
// (bytes; the names are told apart, not shown whole).
func commonEnds(names []string) (pre, suf int) {
	if len(names) == 0 {
		return 0, 0
	}
	first := names[0]
	pre, suf = len(first), len(first)
	for _, n := range names[1:] {
		p := 0
		for p < pre && p < len(n) && n[p] == first[p] {
			p++
		}
		pre = p
		s := 0
		for s < suf && s < len(n) && n[len(n)-1-s] == first[len(first)-1-s] {
			s++
		}
		suf = s
	}
	if pre+suf > len(first) {
		suf = len(first) - pre
	}
	return pre, suf
}

// PickVersion is the version that suits the cores best (the chooser's
// pre-selection, and the version played when nobody can be asked), plays
// saying which play as stored at the user's cap: the largest picture of
// those, the higher bitrate on a tie; with none, the smallest of at least
// 720 lines (the output tops out at 720p, so more only costs the server);
// with none of those, the largest.
func PickVersion(vs []Version, plays func(Version) bool) int {
	best := -1
	for i, v := range vs {
		if v.Detailed() && plays(v) && (best < 0 || larger(v, vs[best])) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	for i, v := range vs {
		if v.class() >= 720 && (best < 0 || smaller(v, vs[best])) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	for i, v := range vs {
		if best < 0 || larger(v, vs[best]) {
			best = i
		}
	}
	return max(best, 0)
}

func larger(a, b Version) bool {
	if a.pixels() != b.pixels() {
		return a.pixels() > b.pixels()
	}
	return a.Source.Bitrate > b.Source.Bitrate
}

func smaller(a, b Version) bool {
	if a.pixels() != b.pixels() {
		return a.pixels() < b.pixels()
	}
	return a.Source.Bitrate > b.Source.Bitrate
}

// MatchVersion is the first version of a Kind, -1 for none.
func MatchVersion(vs []Version, kind string) int {
	if kind == "" {
		return -1
	}
	for i := range vs {
		if vs[i].Kind() == kind {
			return i
		}
	}
	return -1
}

// ownResumePoints reports whether the server keeps each version's resume
// point on the version's own item (Jellyfin 12 and later, as measured), its
// version read once a session (PublicInfo); false while it cannot be read.
func (c *Client) ownResumePoints(ctx context.Context) bool {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.serverMajor == 0 {
		info, err := c.PublicInfo(ctx)
		if err != nil {
			return false
		}
		major, _, _ := strings.Cut(info.Version, ".")
		if n, err := strconv.Atoi(major); err == nil && n > 0 {
			c.serverMajor = n
		} else {
			c.serverMajor = -1
		}
	}
	return c.serverMajor >= 12
}

// versionOffsets reads each version's own resume point (GET
// /UserItems/{id}/UserData) and makes the item's the first version's, the
// one its record has in force. On a failure every point stays unknown.
func (c *Client) versionOffsets(ctx context.Context, it *Item) error {
	offsets := make([]int, len(it.Versions))
	for i, v := range it.Versions {
		if v.ID == it.ID {
			offsets[i] = it.ViewOffset // the item's own, in its record
			continue
		}
		var u struct {
			PlaybackPositionTicks int64 `json:"PlaybackPositionTicks"`
		}
		if err := c.getJSON(ctx, c.http, "/UserItems/"+url.PathEscape(v.ID)+"/UserData", c.userQuery(), &u); err != nil {
			return err
		}
		offsets[i] = Seconds(u.PlaybackPositionTicks)
	}
	for i := range it.Versions {
		it.Versions[i].Offset = offsets[i]
	}
	it.ViewOffset = offsets[0]
	return nil
}
