package ui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"

	"plexcrt/internal/jellyfin"
)

// Config is the app's settings and sign-in, kept as JSON on the SD card.
// The token lives only here and in memory; nothing logs them.
type Config struct {
	EarlyAccessUpdates bool     `json:"early_access_updates"`
	RecentSearches     []string `json:"recent_searches,omitempty"`
	NoTaps             bool     `json:"no_taps"`
	NoTheme            bool     `json:"no_theme"`
	Progressive        bool     `json:"progressive"`             // confirmed HDMI 480p preference
	DeFlicker          bool     `json:"de_flicker"`              // soften fine detail to reduce 480i flicker
	FourThree          bool     `json:"only_4x3"`                // hide media wider than 4:3
	NoAutoplay         bool     `json:"no_autoplay"`             // do not run on to the next episode
	NoSkipButtons      bool     `json:"no_skip_buttons"`         // no Skip intro / Skip credits button over the markers
	Bitrate            int      `json:"bitrate"`                 // transcode cap in kbit/s; 0 is the default
	AudioBoost         int      `json:"audio_boost,omitempty"`   // surround downmix gain; 0 is the default
	SubtitleSize       int      `json:"subtitle_size,omitempty"` // Plex burn-in size; 0 is the original size
	Geometry           Geometry `json:"geometry"`                // where video goes on the raster (menus ignore it)
	Crop               Crop     `json:"crop,omitempty"`          // what each playback starts cropping; nothing saved is off
	ShowBeta           bool     `json:"show_beta_features"`      // show (and run) the features still in beta
	// HiddenLibraries are the keys of the chosen server's libraries left
	// off Home, the menu and search (Options > Libraries).
	HiddenLibraries []string `json:"hidden_libraries,omitempty"`
	// SeenMemos are the ids of the start-up memos already dismissed.
	SeenMemos []string `json:"seen_memos,omitempty"`
	// Premium is the supporter extras' own settings, by key; the public
	// build keeps them without using them.
	Premium map[string]string `json:"premium,omitempty"`
	// ClientID is this install's device id, stable across runs: Jellyfin
	// keys its sessions and Quick Connect on it.
	ClientID string `json:"client_id"`
	// The server and the session on it (Credentials). The address stays
	// after a sign-out, so signing in again starts from it.
	ServerURL   string `json:"server_url"`   // the server's base address
	ServerToken string `json:"server_token"` // the session's access token
	ServerName  string `json:"server_name"`
	ServerID    string `json:"server_id,omitempty"` // the server's id, from its public info
	UserID      string `json:"user_id,omitempty"`
	UserName    string `json:"user_name,omitempty"`
	// Streams are the audio and subtitle tracks chosen, by movie or show
	// id: Jellyfin keeps no per-file default, so the app keeps the choice
	// and passes it with each play (streamChoiceKey).
	Streams     map[string]StreamChoice `json:"streams,omitempty"`
	LoadWarning string                  `json:"-"`
	LoadError   error                   `json:"-"`
	path        string
}

// StreamChoice is a chosen audio track and subtitle track, as Jellyfin
// stream indexes; "" is the server's default, and Subtitle "-1" is off.
type StreamChoice struct {
	Audio    string `json:"audio,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

// snapshot is a copy to put back when a save fails. The extras' settings
// map is copied too, so a failed save leaves them as they were as well.
func (c *Config) snapshot() Config {
	s := *c
	s.Premium = maps.Clone(c.Premium)
	s.Streams = maps.Clone(c.Streams)
	return s
}

// LoadConfig reads the config file; a missing file gives the defaults. A
// client identifier is made on first run and kept.
func LoadConfig(path string) *Config {
	c := &Config{path: path}
	data, err := os.ReadFile(path)
	needsSave := err != nil
	if err == nil {
		if json.Unmarshal(data, c) != nil {
			// Preserve damaged input before recovering; never silently overwrite it.
			if err := os.Rename(path, path+".corrupt-"+time.Now().Format("20060102-150405")); err != nil {
				c.LoadError = fmt.Errorf("cannot preserve damaged settings: %w", err)
				return c
			}
			c = &Config{path: path, LoadWarning: "Settings were damaged. Please sign in again."}
			if backup, e := os.ReadFile(path + ".bak"); e == nil {
				var recovered Config
				if json.Unmarshal(backup, &recovered) == nil && recovered.ClientID != "" {
					recovered.path = path
					recovered.LoadWarning = "Recovered settings from backup."
					c = &recovered
				}
			}
		}
	} else if !os.IsNotExist(err) {
		c.LoadError = fmt.Errorf("cannot read settings: %w", err)
		return c
	}
	if c.ClientID == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			c.LoadError = err
			return c
		}
		c.ClientID = "misterzine-" + hex.EncodeToString(b[:])
		needsSave = true
	}
	if needsSave || c.LoadWarning != "" {
		if e := c.Save(); e != nil {
			c.LoadError = e
		}
	}
	return c
}

// DefaultBitrate is the core's own limit (MaxVideoKbps): the video cap a
// wired connection needs nothing below.
const DefaultBitrate = 8000

// Bitrates are the video caps offered, in kbit/s. The steps below 3 Mbps are
// for a weak Wi-Fi link to the server; their transcodes are also made
// smaller (transcodeSize), and a direct stream above the cap is transcoded.
var Bitrates = []struct {
	Kbps  int
	Label string
}{{400, "0.4 Mbps, low res"}, {1000, "1 Mbps"}, {2000, "2 Mbps"}, {3000, "3 Mbps"}, {4000, "4 Mbps"}, {6000, "6 Mbps"}, {8000, "8 Mbps (max)"}}

// BitrateKbps is the cap in force: the offered one at or below the saved
// value, so the request is always the step Options shows. Every step older
// builds offered is still offered, so a saved choice loads unchanged.
func (c *Config) BitrateKbps() int {
	return Bitrates[c.bitrateIndex()].Kbps
}

// bitrateIndex is the offered cap at or below the saved one (the lowest for
// anything smaller, the default when nothing is saved).
func (c *Config) bitrateIndex() int {
	saved := c.Bitrate
	if saved <= 0 {
		saved = DefaultBitrate
	}
	i := 0
	for j, b := range Bitrates {
		if b.Kbps <= saved {
			i = j
		}
	}
	return i
}

// AudioBoosts are the surround downmix gains, expressed as percentages.
// The playback worker limits boosted peaks; stereo sources stay unchanged.
var AudioBoosts = []struct {
	Label string
	Value int
}{{"Off", 100}, {"Small", 175}, {"Large", 300}}

const DefaultAudioBoost = 300

// SubtitleSizes are Plex's percentage sizes for burned-in subtitles.
var SubtitleSizes = []struct {
	Label string
	Value int
}{{"Normal", 100}, {"Large", 150}, {"Extra large", 200}}

func (c *Config) SubtitleSizeValue() int {
	if c.SubtitleSize <= 0 {
		return 100
	}
	for _, s := range SubtitleSizes {
		if s.Value == c.SubtitleSize {
			return c.SubtitleSize
		}
	}
	return 100
}

func (c *Config) subtitleSizeIndex() int {
	for i, s := range SubtitleSizes {
		if s.Value == c.SubtitleSizeValue() {
			return i
		}
	}
	return 0
}

// AudioBoostValue is the downmix gain in force.
func (c *Config) AudioBoostValue() int {
	if c.AudioBoost <= 0 {
		return DefaultAudioBoost
	}
	return max(100, min(300, c.AudioBoost))
}

// audioBoostIndex is the place in AudioBoosts of the gain in force, -1 for
// a gain they do not offer (set by hand).
func (c *Config) audioBoostIndex() int {
	for i, b := range AudioBoosts {
		if b.Value == c.AudioBoostValue() {
			return i
		}
	}
	return -1
}

// SignedIn reports whether a server and a session on it are on record.
func (c *Config) SignedIn() bool { return c.ServerURL != "" && c.ServerToken != "" }

// Credentials is the session on record, as the jellyfin client keeps it.
func (c *Config) Credentials() jellyfin.Credentials {
	return jellyfin.Credentials{UserID: c.UserID, UserName: c.UserName, Token: c.ServerToken, ServerID: c.ServerID}
}

// SetCredentials records a session; Save keeps it. Only the UI thread
// calls it: the running client holds its own copy (NewClient).
func (c *Config) SetCredentials(cr jellyfin.Credentials) {
	c.UserID, c.UserName, c.ServerToken = cr.UserID, cr.UserName, cr.Token
	if cr.ServerID != "" {
		c.ServerID = cr.ServerID
	}
}

// SignOut forgets the session and what belongs to it. The server's
// address and name stay, for the next sign-in to start from.
func (c *Config) SignOut() error {
	next := c.snapshot()
	next.RecentSearches = nil
	next.HiddenLibraries = nil // the keys belong to the session forgotten here
	next.Streams = nil         // so do the item ids
	next.ServerToken, next.UserID, next.UserName = "", "", ""
	data, _ := json.MarshalIndent(&next, "", "  ")
	// Clear recovery credentials first so a later corrupt primary cannot undo sign-out.
	if err := atomicConfig(c.path+".bak", data); err != nil {
		return err
	}
	if err := atomicConfig(c.path, data); err != nil {
		return err
	}
	*c = next
	return nil
}

func atomicConfig(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Save atomically replaces settings and retains the last valid configuration.
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if old, e := os.ReadFile(c.path); e == nil && json.Valid(old) {
		if e = atomicConfig(c.path+".bak", old); e != nil {
			return e
		}
	}
	return atomicConfig(c.path, data)
}
