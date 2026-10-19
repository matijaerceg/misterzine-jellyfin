package ui

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"plexcrt/internal/input"
)

// signInServer is a Jellyfin server for the sign-in screens, answering
// with the jellyfin package's captured fixtures. Quick Connect requests
// are approved or expired by the test; the password is "secret".
type signInServer struct {
	*httptest.Server
	mu       sync.Mutex
	approved bool
	expired  bool
	quick    bool // Quick Connect is switched on
	views    int  // status for /UserViews (0: 200)
}

func fixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "jellyfin", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newSignInServer(t *testing.T) *signInServer {
	s := &signInServer{quick: true}
	public, auth, qc := fixtureFile(t, "public.json"), fixtureFile(t, "auth.json"), fixtureFile(t, "qc_connect.json")
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.URL.Path {
		case "/System/Info/Public":
			w.Write(public)
		case "/QuickConnect/Enabled":
			json.NewEncoder(w).Encode(s.quick)
		case "/QuickConnect/Initiate":
			if !s.quick {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write(qc)
		case "/QuickConnect/Connect":
			if r.URL.Query().Get("secret") != "SECRET" || s.expired {
				http.NotFound(w, r)
				return
			}
			var m map[string]any
			json.Unmarshal(qc, &m)
			m["Authenticated"] = s.approved
			json.NewEncoder(w).Encode(m)
		case "/Users/AuthenticateWithQuickConnect":
			if !s.approved {
				http.NotFound(w, r)
				return
			}
			w.Write(auth)
		case "/Users/AuthenticateByName":
			var body struct{ Username, Pw string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Username != "testuser" || body.Pw != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write(auth)
		case "/UserViews":
			if s.views != 0 {
				w.WriteHeader(s.views)
				return
			}
			io.WriteString(w, `{"Items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *signInServer) set(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
}

func signInApp(t *testing.T) *App {
	t.Helper()
	cfg := LoadConfig(filepath.Join(t.TempDir(), "settings.json"))
	if cfg.LoadError != nil {
		t.Fatal(cfg.LoadError)
	}
	return &App{Cfg: cfg, Log: log.New(io.Discard, "", 0), Wake: make(chan struct{}, 1),
		later: make(chan func(), 32), Connected: make(chan struct{}, 1), Art: NewArt(nil, 0, nil)}
}

// pump runs the UI thread's queued work until ok holds.
func pump(t *testing.T, a *App, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		select {
		case f := <-a.later:
			f()
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// openLogin puts a fresh sign-in screen on the app's stack.
func openLogin(a *App) *Login {
	l := NewLogin(a)
	a.stack = []Screen{l}
	return l
}

// probed types the server's address and waits for the method list.
func probed(t *testing.T, a *App, l *Login, address string) {
	t.Helper()
	l.pad.text = address
	l.typed()
	pump(t, a, "the probe", func() bool { return !l.busy })
	if l.step != stepMethod {
		t.Fatalf("after the probe the screen is at step %d: %q", l.step, l.err)
	}
}

func expectSignedIn(t *testing.T, a *App, s *signInServer) {
	t.Helper()
	select {
	case <-a.Connected:
	default:
		t.Fatal("the app was not told to connect")
	}
	if !a.Cfg.SignedIn() || a.Cfg.ServerToken != "TESTTOKEN" || a.Cfg.UserID != "0123456789abcdef0123456789abcdef" ||
		a.Cfg.UserName != "testuser" || a.Cfg.ServerURL != s.URL || a.Cfg.ServerName != "test-server" ||
		a.Cfg.ServerID != "ab2fcab83d844492a4ecc65a0fca36cd" {
		t.Fatalf("session not recorded: %+v", a.Cfg.Credentials())
	}
	saved := LoadConfig(a.Cfg.path)
	if saved.ServerToken != "TESTTOKEN" || saved.UserID != a.Cfg.UserID || saved.ServerURL != s.URL {
		t.Fatal("the session was not saved to the settings file")
	}
}

// The address must name a Jellyfin server: nonsense is refused at once, a
// server that does not answer as Jellyfin is refused after the probe, and
// a real one moves on to the ways to sign in, Quick Connect among them
// when the server offers it.
func TestSignInServerAddress(t *testing.T) {
	s := newSignInServer(t)
	a := signInApp(t)
	l := openLogin(a)
	if l.step != stepAddress || l.pad.text != "" {
		t.Fatal("a first sign-in does not start at an empty address")
	}
	l.pad.text = "   "
	l.typed()
	if l.err == "" || l.busy {
		t.Fatal("an empty address was probed")
	}
	other := httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	l.pad.text = other.URL
	l.typed()
	pump(t, a, "the failed probe", func() bool { return !l.busy })
	if l.step != stepAddress || !strings.Contains(l.err, "No Jellyfin server") {
		t.Fatalf("a server that is not Jellyfin: step %d, %q", l.step, l.err)
	}
	probed(t, a, l, s.URL+"/")
	if l.host != s.URL || l.info.ServerName != "test-server" || l.methods()[0] != "Quick Connect" {
		t.Fatalf("probed %q %+v, methods %q", l.host, l.info, l.methods())
	}
	if l.Back(); l.step != stepAddress || l.pad.text != s.URL {
		t.Fatal("Back from the methods does not return to the address, kept")
	}

	s.set(func() { s.quick = false })
	l = openLogin(a)
	probed(t, a, l, s.URL)
	if l.quick || l.methods()[0] != "User name and password" {
		t.Fatalf("Quick Connect offered by a server without it: %q", l.methods())
	}
}

// Quick Connect shows the server's code and polls until another app
// approves it; then the session is saved and the app connects.
func TestSignInQuickConnectApproved(t *testing.T) {
	old := QuickPoll
	QuickPoll = 10 * time.Millisecond
	t.Cleanup(func() { QuickPoll = old })
	s := newSignInServer(t)
	a := signInApp(t)
	l := openLogin(a)
	probed(t, a, l, s.URL)
	l.Key(input.Event{Key: input.Enter}, time.Now()) // Quick Connect, the first row
	if l.step != stepQuick {
		t.Fatalf("OK on Quick Connect went to step %d", l.step)
	}
	pump(t, a, "the code", func() bool { return l.code != "" })
	if l.code != "123456" || !l.busy {
		t.Fatalf("code %q, busy %v", l.code, l.busy)
	}
	time.Sleep(50 * time.Millisecond) // a few polls, not yet approved
	if len(a.Connected) != 0 || a.Cfg.SignedIn() {
		t.Fatal("signed in before the code was approved")
	}
	s.set(func() { s.approved = true })
	pump(t, a, "the approval", func() bool { return len(a.Connected) > 0 || l.err != "" })
	expectSignedIn(t, a, s)
}

// An expired code says so, stops polling, and OK asks for a new one; Back
// gives up on it.
func TestSignInQuickConnectExpired(t *testing.T) {
	old := QuickPoll
	QuickPoll = 10 * time.Millisecond
	t.Cleanup(func() { QuickPoll = old })
	s := newSignInServer(t)
	a := signInApp(t)
	l := openLogin(a)
	probed(t, a, l, s.URL)
	l.startQuick()
	pump(t, a, "the code", func() bool { return l.code != "" })
	s.set(func() { s.expired = true })
	pump(t, a, "the expiry", func() bool { return l.err != "" })
	if !strings.Contains(l.err, "expired") || l.busy || l.code != "" || a.Cfg.SignedIn() {
		t.Fatalf("expired: %q, busy %v, code %q", l.err, l.busy, l.code)
	}
	s.set(func() { s.expired = false })
	l.Key(input.Event{Key: input.Enter}, time.Now())
	pump(t, a, "a new code", func() bool { return l.code != "" })
	gen := l.gen.Load()
	l.Back()
	if l.step != stepMethod || l.current(gen) {
		t.Fatal("Back did not leave Quick Connect and stop its polling")
	}
	s.set(func() { s.approved = true })
	time.Sleep(50 * time.Millisecond)
	pump(t, a, "the queue", func() bool { return len(a.later) == 0 })
	if a.Cfg.SignedIn() || len(a.Connected) != 0 {
		t.Fatal("a code given up on still signed in")
	}
}

// User name and password: a wrong password is refused with the reason, the
// right one saves the session and connects.
func TestSignInUserAndPassword(t *testing.T) {
	s := newSignInServer(t)
	a := signInApp(t)
	l := openLogin(a)
	probed(t, a, l, s.URL)
	l.Key(input.Event{Key: input.Down}, time.Now())
	l.Key(input.Event{Key: input.Enter}, time.Now())
	if l.step != stepUser {
		t.Fatalf("OK on user name and password went to step %d", l.step)
	}
	l.typed()
	if l.step != stepUser || l.err == "" {
		t.Fatal("an empty user name was taken")
	}
	l.pad.text = "testuser"
	l.typed()
	if l.step != stepPassword || !l.pad.secret || l.pad.text != "" {
		t.Fatal("the password step does not start empty and hidden")
	}
	l.pad.text = "wrong"
	if l.pad.shown() != "*****" {
		t.Fatalf("the password shows as %q", l.pad.shown())
	}
	l.typed()
	pump(t, a, "the refusal", func() bool { return !l.busy })
	if l.err != "Wrong user name or password." || a.Cfg.SignedIn() || len(a.Connected) != 0 {
		t.Fatalf("wrong password: %q", l.err)
	}
	l.pad.text = "secret"
	l.typed()
	pump(t, a, "the sign-in", func() bool { return !l.busy })
	expectSignedIn(t, a, s)
}

// A token the server no longer takes (401) anywhere sends the app back to
// sign-in with the address kept and the session forgotten.
func TestUnauthorizedReturnsToSignIn(t *testing.T) {
	s := newSignInServer(t)
	s.set(func() { s.views = http.StatusUnauthorized })
	a := signInApp(t)
	a.Cfg.ServerURL, a.Cfg.ServerName = s.URL, "test-server"
	a.Cfg.ServerToken, a.Cfg.UserID = "revoked", "u"
	if err := a.Cfg.Save(); err != nil {
		t.Fatal(err)
	}
	a.Client = NewClient(a.Cfg, t.TempDir())
	a.stack = []Screen{&splashTestScreen{}}
	a.loadSections()
	pump(t, a, "the return to sign-in", func() bool { _, ok := a.top().(*Login); return ok })
	if a.Client != nil || a.Cfg.SignedIn() || a.Cfg.ServerURL != s.URL || a.Notice == "" {
		t.Fatalf("after a 401: client %v, signed in %v, address %q, notice %q", a.Client != nil, a.Cfg.SignedIn(), a.Cfg.ServerURL, a.Notice)
	}
	if l := a.top().(*Login); l.pad.text != s.URL {
		t.Fatal("the sign-in does not start from the server's address")
	}
	if saved := LoadConfig(a.Cfg.path); saved.ServerToken != "" {
		t.Fatal("the refused token is still saved")
	}
}

// The on-screen keyboard types lower and upper case and symbols, deletes,
// and OK on its wide key finishes.
func TestKeyPadTypes(t *testing.T) {
	k := newKeyPad("", "Done", false)
	ok := func(i int) padAction { k.key = i; return k.Key(input.Event{Key: input.Enter}) }
	ok(1)  // b
	ok(26) // shift
	ok(1)  // B
	ok(27) // digits and symbols
	ok(0)  // 0
	ok(27) // back to letters
	ok(28) // Delete
	ok(9)  // j
	if k.text != "bBj" {
		t.Fatalf("typed %q", k.text)
	}
	k.key = 0
	for range 5 {
		k.Key(input.Event{Key: input.Down})
	}
	if k.key != padKeys || k.Key(input.Event{Key: input.Enter}) != padDone {
		t.Fatal("OK on the wide key did not finish")
	}
	if took, _ := k.Keyboard(input.Event{Keyboard: true, Text: ':'}); !took || k.text != "bBj:" {
		t.Fatalf("a USB keyboard typed %q", k.text)
	}
	if k.Keyboard(input.Event{Keyboard: true, ScanCode: 0x66}); k.text != "bBj" {
		t.Fatal("Backspace did not delete")
	}
}
