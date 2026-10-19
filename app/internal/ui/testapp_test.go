package ui

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"plexcrt/internal/gfx"
	"plexcrt/internal/jellyfin"
	"plexcrt/internal/updates"
)

// betaTestApp is an app on a beta build, v0.2.0-beta.3 build "fixture".
func betaTestApp(t *testing.T) *App {
	t.Helper()
	ch := updates.Channel
	t.Cleanup(func() { updates.Channel = ch })
	updates.Channel = "beta"
	a := &App{Cfg: &Config{path: filepath.Join(t.TempDir(), "settings.json"), NoTheme: true}, Version: "v0.2.0-beta.3", Build: "fixture", Log: log.New(io.Discard, "", 0)}
	a.F = Fonts{Title: gfx.Load("med22"), Body: gfx.Load("med18"), Small: gfx.Load("reg16"), SmallBold: gfx.Load("med16"), Big: gfx.Load("bold28")}
	a.Mark = NewWordmark()
	a.T = gfx.NewTextCache(1, nil)
	a.stack = []Screen{&splashTestScreen{}}
	return a
}

// testClient is a client of the test server at host, signed in with token
// as user "u" (an empty token: signed out).
func testClient(host, token, cacheDir string) *jellyfin.Client {
	c := jellyfin.New(host, cacheDir, "test-device", nil)
	// the client retries a 5xx itself (Retries, Backoff); the tests count the
	// requests the UI makes and time its own retries, so one ask is one request
	c.Retries = 0
	if token != "" {
		c.Store.SetCredentials(jellyfin.Credentials{UserID: "u", Token: token})
	}
	return c
}

// jfPage answers a listing request with the items (BaseItemDto JSON
// objects) as Jellyfin pages them: startIndex and limit applied, the total
// the whole listing's.
func jfPage(w io.Writer, r *http.Request, items ...string) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		limit = len(items)
	}
	start = min(max(start, 0), len(items))
	end := min(len(items), start+max(limit, 0))
	fmt.Fprintf(w, `{"Items":[%s],"TotalRecordCount":%d}`, strings.Join(items[start:end], ","), len(items))
}
