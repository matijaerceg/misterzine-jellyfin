package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

func TestPlainHTTPConsentIsRequiredAndScoped(t *testing.T) {
	s := newSignInServer(t)
	a := signInApp(t)
	discovering(t, nil)
	l := openLogin(a)
	pump(t, a, "discovery", func() bool { return !l.busy })
	l.pad.text = s.URL
	l.typed()
	pump(t, a, "probe", func() bool { return !l.busy })
	if l.step != stepPlain || a.Cfg.PlainHTTPServer != "" || a.Cfg.SignedIn() {
		t.Fatal("HTTP did not stop for consent")
	}
	// Even an accidental call before the UI approval must fail closed.
	if _, err := l.client.SignIn(bg(), "testuser", "secret"); err == nil {
		t.Fatal("password sent before consent")
	}
	l.Back()
	if l.step != stepAddress || a.Cfg.PlainHTTPServer != "" {
		t.Fatal("cancel recorded approval")
	}
	l.pad.text = s.URL
	l.typed()
	pump(t, a, "probe", func() bool { return !l.busy })
	l.Key(input.Event{Key: input.Enter}, time.Now())
	if l.step != stepMethod || !l.plainAccepted {
		t.Fatal("explicit approval did not proceed")
	}
	cr, err := l.client.SignIn(bg(), "testuser", "secret")
	if err != nil {
		t.Fatal(err)
	}
	l.save(cr)
	if a.Cfg.PlainHTTPServer != s.URL || NewClient(a.Cfg, "") == nil {
		t.Fatal("approval not saved with session")
	}
	changed := a.Cfg.snapshot()
	changed.ServerURL = "http://127.0.0.1:1"
	if NewClient(&changed, "") != nil {
		t.Fatal("approval carried to another address")
	}
	if err := a.Cfg.SignOut(); err != nil {
		t.Fatal(err)
	}
	if a.Cfg.PlainHTTPServer != "" {
		t.Fatal("sign-out retained approval")
	}
}

func TestDiscoveryIdentityMismatchIsRejected(t *testing.T) {
	s := newSignInServer(t)
	a := signInApp(t)
	discovering(t, nil, jellyfin.FoundServer{Address: s.URL, ID: "different-server", Name: "Wrong server"})
	l := openLogin(a)
	pump(t, a, "discovery", func() bool { return !l.busy })
	l.Key(input.Event{Key: input.Enter}, time.Now())
	pump(t, a, "probe", func() bool { return !l.busy })
	if l.client != nil || l.step == stepMethod || !strings.Contains(l.err, "identity changed") {
		t.Fatalf("mismatch accepted: step=%d error=%q", l.step, l.err)
	}
}

func TestLegacyHTTPSessionRequiresConsent(t *testing.T) {
	cfg := &Config{ServerURL: "http://127.0.0.1:8096", ServerToken: "test-token", UserID: "test-user"}
	if NewClient(cfg, "") != nil {
		t.Fatal("legacy HTTP session bypassed warning")
	}
}

func TestHTTPWarningPreview(t *testing.T) {
	path := os.Getenv("HTTP_WARNING_PREVIEW")
	if path == "" {
		t.Skip("set HTTP_WARNING_PREVIEW to render the consent screen")
	}
	a := cropApp(t)
	l := &Login{app: a, step: stepPlain, host: "http://jellyfin.local:8096"}
	c := gfx.NewCanvas(720, 480)
	for i := 0; i < 15; i++ {
		l.Draw(c, time.Now())
		time.Sleep(20 * time.Millisecond)
	}
	out := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			p := (y*720 + x*720/640) * 4
			out.SetRGBA(x, y, color.RGBA{c.Pix[p+2], c.Pix[p+1], c.Pix[p], 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}
