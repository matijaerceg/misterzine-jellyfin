package jellyfin

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Item is a movie, show, season, episode, collection or folder as the app
// sees it. Field names follow the plex package where the meaning carries
// over; the renames are listed in docs/JELLYFIN_API.md.
type Item struct {
	ID          string // Jellyfin item id (plex RatingKey)
	Key         string // children listing (path and query) for Items/Page; "" for a leaf
	Type        string // movie, show, season, episode, collection, folder
	Subtype     string // what a collection holds, when the server says: movie or show
	Title       string
	SortTitle   string
	Year        int
	Duration    int // seconds
	ViewOffset  int // seconds into it (resume point)
	ViewCount   int
	Played      bool
	Summary     string
	Thumb       string  // poster image ref (an episode's is its show's)
	Art         string  // backdrop image ref
	Theme       string  // theme song item id, when known
	SeriesTitle string  // show title for episodes and seasons (plex GrandTitle)
	SeriesID    string  // the show's id for episodes and seasons (plex GrandKey)
	ParentID    string  // the season's id for episodes, the show's for seasons (plex ParentKey)
	Children    int     // seasons of a show, items of a collection
	Leaves      int     // episodes of a show or season
	Viewed      int     // of which watched
	Still       string  // an episode's own picture ref
	Logo        string  // clear logo ref
	Aspect      float64 // picture aspect of the first video stream, 0 unknown
	Index       int     // episode number (season number for a season)
	Season      int     // season number of an episode (plex Parent)
	Rating      float64
	Tagline     string
	Genres      []string
	Colors      [4]uint32 // poster corner colours from its blurhash (TL, TR, BR, BL), 0 if none
	Markers     []Marker  // intro and credits (media segments), when fetched
	SourceID    string    // the first media source (plex PartID), for PlaybackInfo
	Audio       []Stream  // its audio streams
	Subs        []Stream  // its subtitle streams
	Library     string    // the library (view) id, when known
}

// Marker is a span the server has found: "intro" or "credits", in seconds.
type Marker struct {
	Type       string
	Start, End float64
}

// Stream is an audio or subtitle track of a media source. ID is the
// stream index Jellyfin takes as AudioStreamIndex/SubtitleStreamIndex.
type Stream struct {
	ID       string
	Title    string
	Selected bool
	Channels int // source audio channels; zero for subtitle tracks or unknown
	// Text marks a text subtitle track (SRT, ASS, WebVTT ...) the server
	// can hand over as a file (SubtitleText); picture subtitles (PGS,
	// VobSub) can only be burned into a transcode.
	Text bool
}

// Playable reports whether Enter should play rather than browse.
func (it *Item) Playable() bool { return it.Type == "movie" || it.Type == "episode" }

// IsFolder reports whether the item is a plain folder of a library.
func (it *Item) IsFolder() bool { return it.Type == "folder" }

// Label is the list title: "S1E3 Title" for episodes.
func (it *Item) Label() string {
	if it.Type == "episode" {
		return fmt.Sprintf("S%dE%d  %s", it.Season, it.Index, it.Title)
	}
	return it.Title
}

// SortLabel is the server's ordering title, with the display title as fallback.
func (it *Item) SortLabel() string {
	if it.SortTitle != "" {
		return it.SortTitle
	}
	return it.Title
}

// Hub is a home-screen row.
type Hub struct {
	Title string
	Key   string // a listing for the whole row, "" if none
	Ident string // home.continue, home.recent.<library id>
	Items []*Item
}

// IsContinueWatching reports whether the hub is the Continue Watching row.
func (h *Hub) IsContinueWatching() bool { return strings.HasPrefix(h.Ident, "home.continue") }

// Section is a library (a user view).
type Section struct {
	Key, Title, Type string // Type: movie or show
}

// dto is the part of BaseItemDto the client reads.
type dto struct {
	Name              string                       `json:"Name"`
	ID                string                       `json:"Id"`
	Type              string                       `json:"Type"`
	CollectionType    string                       `json:"CollectionType"`
	SortName          string                       `json:"SortName"`
	ProductionYear    int                          `json:"ProductionYear"`
	RunTimeTicks      int64                        `json:"RunTimeTicks"`
	Overview          string                       `json:"Overview"`
	Taglines          []string                     `json:"Taglines"`
	Genres            []string                     `json:"Genres"`
	CommunityRating   float64                      `json:"CommunityRating"`
	IndexNumber       int                          `json:"IndexNumber"`
	ParentIndexNumber int                          `json:"ParentIndexNumber"`
	IsFolder          bool                         `json:"IsFolder"`
	ParentID          string                       `json:"ParentId"`
	SeriesName        string                       `json:"SeriesName"`
	SeriesID          string                       `json:"SeriesId"`
	SeasonID          string                       `json:"SeasonId"`
	ChildCount        int                          `json:"ChildCount"`
	RecursiveCount    int                          `json:"RecursiveItemCount"`
	ImageTags         map[string]string            `json:"ImageTags"`
	BackdropImageTags []string                     `json:"BackdropImageTags"`
	ImageBlurHashes   map[string]map[string]string `json:"ImageBlurHashes"`

	SeriesPrimaryImageTag   string   `json:"SeriesPrimaryImageTag"`
	ParentBackdropItemID    string   `json:"ParentBackdropItemId"`
	ParentBackdropImageTags []string `json:"ParentBackdropImageTags"`
	ParentLogoItemID        string   `json:"ParentLogoItemId"`
	ParentLogoImageTag      string   `json:"ParentLogoImageTag"`
	ParentThumbItemID       string   `json:"ParentThumbItemId"`
	ParentThumbImageTag     string   `json:"ParentThumbImageTag"`

	UserData *struct {
		PlaybackPositionTicks int64   `json:"PlaybackPositionTicks"`
		PlayCount             int     `json:"PlayCount"`
		Played                bool    `json:"Played"`
		UnplayedItemCount     *int    `json:"UnplayedItemCount"`
		PlayedPercentage      float64 `json:"PlayedPercentage"`
	} `json:"UserData"`
	MediaSources []mediaSource `json:"MediaSources"`
	MediaStreams []MediaStream `json:"MediaStreams"`
	Width        int           `json:"Width"`
	Height       int           `json:"Height"`
}

type mediaSource struct {
	ID                         string        `json:"Id"`
	MediaStreams               []MediaStream `json:"MediaStreams"`
	DefaultAudioStreamIndex    *int          `json:"DefaultAudioStreamIndex"`
	DefaultSubtitleStreamIndex *int          `json:"DefaultSubtitleStreamIndex"`
}

// MediaStream is a source stream as Jellyfin describes it in item and
// PlaybackInfo responses. It exposes source channel counts and default
// selection for playback decisions.
type MediaStream struct {
	Type         string `json:"Type"` // Video, Audio, Subtitle
	Index        int    `json:"Index"`
	DisplayTitle string `json:"DisplayTitle"`
	Title        string `json:"Title"`
	Language     string `json:"Language"`
	Codec        string `json:"Codec"`
	IsDefault    bool   `json:"IsDefault"`
	AspectRatio  string `json:"AspectRatio"`
	Width        int    `json:"Width"`
	Height       int    `json:"Height"`
	Channels     int    `json:"Channels"`
	// Video details Jellyfin probed; zero when the server does not know.
	Profile          string  `json:"Profile"`
	Level            float64 `json:"Level"`
	BitDepth         int     `json:"BitDepth"`
	BitRate          int     `json:"BitRate"`
	RealFrameRate    float64 `json:"RealFrameRate"`
	AverageFrameRate float64 `json:"AverageFrameRate"`
	IsInterlaced     bool    `json:"IsInterlaced"`
	// A subtitle stream in a text format that the server can deliver on
	// its own (Subtitles/{index}/Stream.srt).
	IsTextSubtitleStream   bool `json:"IsTextSubtitleStream"`
	SupportsExternalStream bool `json:"SupportsExternalStream"`
}

// listing is a QueryResult; Latest answers with a bare array instead.
type listing struct {
	Items []dto `json:"Items"`
	Total int   `json:"TotalRecordCount"`
	Start int   `json:"StartIndex"`
}

func decodeListing(data []byte) (*listing, error) {
	var l listing
	trim := strings.TrimLeft(string(data[:min(len(data), 64)]), " \t\r\n")
	if strings.HasPrefix(trim, "[") {
		if err := json.Unmarshal(data, &l.Items); err != nil {
			return nil, err
		}
		l.Total = len(l.Items)
		return &l, nil
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

var typeNames = map[string]string{
	"Movie": "movie", "Series": "show", "Season": "season", "Episode": "episode",
	"BoxSet": "collection", "Folder": "folder", "CollectionFolder": "folder", "Video": "movie",
	"MusicVideo": "movie",
}

// ImageRef names one picture: item id, image type and tag. It is opaque to
// the UI (a string it caches by and hands back to Art and Logo).
func imageRef(id, kind, tag string) string {
	if id == "" || tag == "" {
		return ""
	}
	return id + "/" + kind + "/" + tag
}

func (x *dto) item() *Item {
	it := &Item{ID: x.ID, Type: typeNames[x.Type], Title: Fold(x.Name), SortTitle: sortTitle(x.SortName),
		Year: x.ProductionYear, Duration: Seconds(x.RunTimeTicks), Summary: Fold(x.Overview),
		Rating: x.CommunityRating, Index: x.IndexNumber, SeriesTitle: Fold(x.SeriesName), SeriesID: x.SeriesID,
		Children: x.ChildCount, Genres: x.Genres}
	if it.Type == "" && x.IsFolder {
		it.Type = "folder"
	}
	if len(x.Taglines) > 0 {
		it.Tagline = Fold(x.Taglines[0])
	}
	if u := x.UserData; u != nil {
		it.ViewOffset = Seconds(u.PlaybackPositionTicks)
		it.ViewCount = u.PlayCount
		it.Played = u.Played
		if u.UnplayedItemCount != nil && x.RecursiveCount > 0 {
			it.Viewed = max(x.RecursiveCount-*u.UnplayedItemCount, 0)
		}
	}
	tags := x.ImageTags
	it.Thumb = imageRef(x.ID, "Primary", tags["Primary"])
	it.Logo = imageRef(x.ID, "Logo", tags["Logo"])
	if len(x.BackdropImageTags) > 0 {
		it.Art = imageRef(x.ID, "Backdrop", x.BackdropImageTags[0])
	} else if len(x.ParentBackdropImageTags) > 0 {
		it.Art = imageRef(x.ParentBackdropItemID, "Backdrop", x.ParentBackdropImageTags[0])
	}
	if it.Logo == "" {
		it.Logo = imageRef(x.ParentLogoItemID, "Logo", x.ParentLogoImageTag)
	}
	if hashes := x.ImageBlurHashes["Primary"]; hashes != nil {
		if h := hashes[tags["Primary"]]; h != "" {
			it.Colors = blurCorners(h)
		}
	}
	switch it.Type {
	case "show":
		it.Key = "/Shows/" + x.ID + "/Seasons"
		it.Leaves = x.RecursiveCount
		if it.Leaves == 0 && x.UserData != nil && x.UserData.UnplayedItemCount != nil {
			it.Leaves = *x.UserData.UnplayedItemCount
		}
	case "season":
		it.Key = "/Shows/" + x.SeriesID + "/Episodes?seasonId=" + x.ID
		it.ParentID = x.SeriesID
		it.Leaves = x.ChildCount
		if it.Leaves == 0 {
			it.Leaves = x.RecursiveCount
		}
		if it.Thumb == "" {
			it.Thumb = imageRef(x.SeriesID, "Primary", x.SeriesPrimaryImageTag)
		}
	case "episode":
		it.Season = x.ParentIndexNumber
		it.ParentID = x.SeasonID
		// the poster wall wants the show's poster; the episode's own
		// picture is its still
		it.Still = it.Thumb
		if s := imageRef(x.SeriesID, "Primary", x.SeriesPrimaryImageTag); s != "" {
			it.Thumb = s
		}
	case "collection", "folder":
		it.Key = "/Items?parentId=" + x.ID
	}
	if it.Viewed == 0 && it.Leaves > 0 && x.UserData != nil && x.UserData.UnplayedItemCount != nil {
		it.Viewed = max(it.Leaves-*x.UserData.UnplayedItemCount, 0)
	}
	streams := x.MediaStreams
	if len(x.MediaSources) > 0 {
		ms := x.MediaSources[0]
		it.SourceID = ms.ID
		streams = ms.MediaStreams
	}
	for _, st := range streams {
		switch st.Type {
		case "Video":
			if it.Aspect == 0 {
				it.Aspect = aspect(st.AspectRatio, st.Width, st.Height)
			}
		case "Audio", "Subtitle":
			s := Stream{ID: strconv.Itoa(st.Index), Title: streamTitle(st), Selected: st.IsDefault, Channels: st.Channels,
				Text: st.Type == "Subtitle" && st.IsTextSubtitleStream && st.SupportsExternalStream}
			if st.Type == "Audio" {
				it.Audio = append(it.Audio, s)
			} else {
				it.Subs = append(it.Subs, s)
			}
		}
	}
	if it.Aspect == 0 && x.Width > 0 && x.Height > 0 && it.Playable() {
		it.Aspect = aspect("", x.Width, x.Height)
	}
	return it
}

// streamTitle is the server's display title, folded; a title in a script
// the atlases lack becomes the language's English name and the codec.
func streamTitle(st MediaStream) string {
	title := Fold(st.DisplayTitle)
	if strings.TrimSpace(strings.NewReplacer("-", "", "(", "", ")", "").Replace(title)) == "" {
		title = strings.TrimSpace(LangName(st.Language) + " " + strings.ToUpper(st.Codec))
	}
	return title
}

// sortTitle is the server's sort name without the zero padding Jellyfin
// gives numbers ("0000000003 body problem" is "3 body problem"): the
// alphabet jump reads its first letter.
func sortTitle(s string) string {
	s = Fold(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '0' && (i == 0 || s[i-1] < '0' || s[i-1] > '9') {
			j := i
			for j+1 < len(s) && s[j] == '0' && s[j+1] >= '0' && s[j+1] <= '9' {
				j++
			}
			i = j
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}

// aspect is a picture aspect from "16:9", else from the frame size, rounded
// to two places (1.78, 2.35).
func aspect(ratio string, w, h int) float64 {
	if a, b, ok := strings.Cut(ratio, ":"); ok {
		x, e1 := strconv.ParseFloat(a, 64)
		y, e2 := strconv.ParseFloat(b, 64)
		if e1 == nil && e2 == nil && y > 0 {
			return float64(int(x/y*100+0.5)) / 100
		}
	}
	if w > 0 && h > 0 {
		return float64(int(float64(w)/float64(h)*100+0.5)) / 100
	}
	return 0
}
