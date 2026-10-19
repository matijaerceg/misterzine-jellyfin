package ui

import (
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// Login is the sign-in: the server's address, typed on the on-screen
// keyboard and checked against the server's public info; then Quick
// Connect (a code to approve in another signed-in Jellyfin app) or a user
// name and password. The session is saved into the settings, which are the
// token store, and the app connects. Network work runs in goroutines whose
// answers come back on the UI thread (Later), tagged with gen so a step
// left since cannot be changed by them.
type Login struct {
	app   *App
	step  loginStep
	pad   *keyPad
	stat  string // what is going on, for the status line
	err   string
	gen   atomic.Int64 // bumped on every change of step so stale answers stand down
	at    time.Time
	host  string // the normalized address, once probed
	info  *jellyfin.PublicInfo
	quick bool // the server offers Quick Connect
	cur   int  // the method list's cursor
	user  string
	code  string // the Quick Connect code shown
	busy  bool
	// client is the sign-in's own client: its session goes into the
	// settings, and the app makes its own client from them.
	client *jellyfin.Client
}

type loginStep int

const (
	stepAddress  loginStep = iota // typing the server's address
	stepMethod                    // Quick Connect, or user name and password
	stepQuick                     // showing the code, waiting for approval
	stepUser                      // typing the user name
	stepPassword                  // typing the password
)

// QuickPoll is how often a Quick Connect code is checked for approval.
var QuickPoll = 2 * time.Second

// NewLogin starts at the server's address, the one on record filled in.
func NewLogin(app *App) *Login {
	l := &Login{app: app, at: time.Now()}
	addr := ""
	if app.Cfg != nil {
		addr = app.Cfg.ServerURL
	}
	l.pad = newKeyPad(addr, "Connect >", false)
	return l
}

// to changes step: answers still on their way for the old one are dropped.
func (l *Login) to(s loginStep) {
	l.gen.Add(1)
	l.step, l.err, l.stat, l.busy = s, "", "", false
	switch s {
	case stepAddress:
		l.pad.done, l.pad.secret = "Connect >", false
		if l.host != "" {
			l.pad.text = l.host
		}
	case stepUser:
		l.pad = newKeyPad(l.user, "Next >", false)
	case stepPassword:
		l.pad = newKeyPad("", "Sign in >", true)
	}
}

// later runs f on the UI thread if the step it was started for is current.
func (l *Login) later(gen int64, f func()) {
	l.app.Later(func() {
		if l.gen.Load() == gen && l.open() {
			f()
		}
	})
}

// open reports whether the screen is still on the stack.
func (l *Login) open() bool {
	for _, s := range l.app.stack {
		if s == l {
			return true
		}
	}
	return false
}

// probe checks the typed address: it must answer as a Jellyfin server.
func (l *Login) probe() {
	host, err := jellyfin.NormalizeHost(l.pad.text)
	if err != nil {
		l.err = "Enter the server's address, such as jellyfin.example.com or 192.0.2.10:8096."
		return
	}
	l.gen.Add(1)
	gen := l.gen.Load()
	l.err, l.busy, l.stat = "", true, "Connecting to "+host+"..."
	client := jellyfin.New(host, "", l.app.Cfg.ClientID, nil)
	go func() {
		info, err := client.PublicInfo(bg())
		quick := false
		if err == nil {
			quick, _ = client.QuickConnectEnabled(bg())
		}
		l.later(gen, func() {
			l.busy, l.stat = false, ""
			if err != nil {
				l.app.Log.Printf("sign-in: %s: %v", host, err)
				l.err = l.probeFailure(host, err)
				return
			}
			l.host, l.info, l.quick, l.client = host, info, quick, client
			l.cur = 0
			l.to(stepMethod)
		})
	}()
}

// probeFailure is the line for an address that gave no Jellyfin answer.
func (l *Login) probeFailure(host string, err error) string {
	var he *jellyfin.HTTPError
	switch {
	case errors.As(err, &he) && he.Status == http.StatusNotFound:
		return "No Jellyfin server at " + host + ". Check the address."
	case unreachable(err):
		if p := classify(err, time.Now()); p != problemServer {
			headline, advice := p.failure()
			return headline + " " + advice
		}
		return reachFailure(host, err)
	}
	return "No Jellyfin server at " + host + ". Check the address."
}

// methods are the method list's rows.
func (l *Login) methods() []string {
	var out []string
	if l.quick {
		out = append(out, "Quick Connect")
	}
	return append(out, "User name and password", "Change server")
}

// startQuick asks for a code and polls it until approved, then redeems it.
func (l *Login) startQuick() {
	l.to(stepQuick)
	gen, client := l.gen.Load(), l.client
	l.busy, l.stat, l.code = true, "Getting a code...", ""
	go func() {
		qc, err := client.QuickConnectStart(bg())
		if err != nil {
			l.later(gen, func() {
				l.busy, l.stat = false, ""
				l.err = "Quick Connect is not available: " + shortErr(err) + " OK: try again."
			})
			return
		}
		l.later(gen, func() { l.code, l.stat = qc.Code, "Waiting for approval..." })
		for {
			time.Sleep(QuickPoll)
			if !l.current(gen) {
				return
			}
			ok, err := client.QuickConnectCheck(bg(), qc.Secret)
			if err != nil {
				var he *jellyfin.HTTPError
				if errors.As(err, &he) && he.Status == http.StatusNotFound {
					l.later(gen, func() {
						l.busy, l.stat, l.code = false, "", ""
						l.err = "The code expired. Press OK for a new one."
					})
					return
				}
				continue // the network blinked: ask again
			}
			if !ok {
				continue
			}
			cr, err := client.QuickConnectFinish(bg(), qc.Secret)
			l.signedIn(gen, cr, err)
			return
		}
	}()
}

// current reports, from any goroutine, whether gen is still the step's.
func (l *Login) current(gen int64) bool { return l.gen.Load() == gen }

// signIn tries the user name and password.
func (l *Login) signIn(password string) {
	l.gen.Add(1)
	gen, client, user := l.gen.Load(), l.client, l.user
	l.err, l.busy, l.stat = "", true, "Signing in..."
	go func() {
		cr, err := client.SignIn(bg(), user, password)
		l.signedIn(gen, cr, err)
	}()
}

// signedIn takes a sign-in's answer back to the UI thread and saves it.
func (l *Login) signedIn(gen int64, cr jellyfin.Credentials, err error) {
	l.later(gen, func() {
		l.busy, l.stat = false, ""
		if err != nil {
			l.app.Log.Printf("sign-in: %v", err)
			switch {
			case errors.Is(err, jellyfin.ErrUnauthorized):
				l.err = "Wrong user name or password."
			case l.step == stepQuick:
				l.err = "Could not finish Quick Connect: " + shortErr(err) + " OK: try again."
			default:
				l.err = "Could not sign in: " + shortErr(err)
			}
			return
		}
		l.save(cr)
	})
}

// save commits the session into the settings (the token store) on the UI
// thread, then has the app connect.
func (l *Login) save(cr jellyfin.Credentials) {
	cfg := l.app.Cfg
	next := cfg.snapshot()
	// library and item ids are the server's own: another server's are
	// other libraries, so its hidden ones and track choices go
	if next.ServerID != "" && next.ServerID != l.info.ID || next.ServerURL != l.host && next.ServerID == "" {
		next.HiddenLibraries, next.Streams = nil, nil
	}
	if next.UserID != "" && next.UserID != cr.UserID {
		next.HiddenLibraries, next.Streams, next.RecentSearches = nil, nil, nil
	}
	next.ServerURL, next.ServerName = l.host, l.info.ServerName
	if cr.ServerID == "" {
		cr.ServerID = l.info.ID
	}
	next.SetCredentials(cr)
	if err := next.Save(); err != nil {
		l.app.Log.Printf("config: %v", err)
		l.err = "Could not save sign-in. Check free space and retry."
		return
	}
	*cfg = next
	l.stat = "Connected."
	l.app.Log.Printf("signed in: %q on %q at %s", cr.UserName, l.info.ServerName, l.host)
	select {
	case l.app.Connected <- struct{}{}:
	default:
	}
}

// Keyboard types into the address, the user name or the password.
func (l *Login) Keyboard(ev input.Event, now time.Time) bool {
	if l.busy || (l.step != stepAddress && l.step != stepUser && l.step != stepPassword) {
		return false
	}
	took, act := l.pad.Keyboard(ev)
	if took {
		l.err = ""
	}
	if act == padDone {
		l.typed()
	}
	return took
}

// typed acts on the keyboard's wide key.
func (l *Login) typed() {
	switch l.step {
	case stepAddress:
		l.probe()
	case stepUser:
		if l.pad.text == "" {
			l.err = "Enter your Jellyfin user name."
			return
		}
		l.user = l.pad.text
		l.to(stepPassword)
	case stepPassword:
		l.signIn(l.pad.text)
	}
}

func (l *Login) Key(ev input.Event, now time.Time) {
	if ev.Release || l.busy && l.step != stepQuick {
		return
	}
	switch l.step {
	case stepAddress, stepUser, stepPassword:
		if l.pad.Key(ev) == padDone {
			l.typed()
		} else if ev.Key == input.Enter {
			l.err = ""
		}
	case stepMethod:
		rows := l.methods()
		switch ev.Key {
		case input.Up:
			l.cur = max(0, l.cur-1)
		case input.Down:
			l.cur = min(len(rows)-1, l.cur+1)
		case input.Enter:
			if ev.Repeat {
				return
			}
			switch rows[l.cur] {
			case "Quick Connect":
				l.startQuick()
			case "User name and password":
				l.to(stepUser)
			case "Change server":
				l.to(stepAddress)
			}
		}
	case stepQuick:
		if ev.Key == input.Enter && !ev.Repeat && l.err != "" {
			l.startQuick()
		}
	}
}

// Back steps back: the password to the user name, the methods to the
// address, the address to Options.
func (l *Login) Back() bool {
	switch l.step {
	case stepAddress:
		l.gen.Add(1)
		l.busy, l.stat = false, ""
		l.app.Push(NewOptions(l.app))
	case stepMethod:
		l.to(stepAddress)
	case stepQuick, stepUser:
		l.to(stepMethod)
	case stepPassword:
		l.to(stepUser)
	}
	return true
}

// Draw paints the step, in the look of the screens it replaces.
func (l *Login) Draw(c *gfx.Canvas, now time.Time) bool {
	c.Fill(0, 0, c.W, c.H, gfx.Bg)
	f := l.app.F
	l.app.Mark.PlaceFlat(c, SafeX+16, SafeY-8)
	y := SafeY + 44
	anim := false
	switch l.step {
	case stepAddress:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, "Sign in to Jellyfin")
		y += f.Big.Height() + 8
		l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, "Your server's address")
		l.pad.Draw(l.app, c, y+f.Body.Height()+10, now)
	case stepUser, stepPassword:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, f.Big.Fit(l.serverName(), SafeW))
		y += f.Big.Height() + 8
		label := "User name"
		if l.step == stepPassword {
			label = "Password for " + l.user
		}
		l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, f.Body.Fit(label, SafeW))
		l.pad.Draw(l.app, c, y+f.Body.Height()+10, now)
	case stepMethod:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, f.Big.Fit(l.serverName(), SafeW))
		y += f.Big.Height() + 8
		l.app.text(c, SafeX, y, f.Body, gfx.GreyLo, f.Body.Fit(l.host, SafeW))
		y += f.Body.Height() + 24
		for i, row := range l.methods() {
			col := gfx.GreyHi
			if i == l.cur {
				col = gfx.White
				focusBar(c, SafeX, y, f.Body.Height())
			}
			l.app.text(c, SafeX, y, f.Body, col, row)
			y += ListRowH
		}
	case stepQuick:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, "Quick Connect")
		y += f.Big.Height() + 20
		l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, "In a Jellyfin app where you are signed in, open")
		y += f.Body.Height() + 10
		l.app.text(c, SafeX, y, f.Big, gfx.Purple, "Settings > Quick Connect")
		y += f.Big.Height() + 10
		l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, "and enter this code:")
		y += f.Body.Height() + 24
		if l.code != "" {
			// the code, letter-spaced
			x := SafeX
			for i := 0; i < len(l.code); i++ {
				l.app.text(c, x, y, f.Big, gfx.White, l.code[i:i+1])
				x += f.Big.Width(l.code[i:i+1]) + 18
			}
		}
	}
	back := map[loginStep]string{stepAddress: "Back: Options", stepMethod: "Back: change server",
		stepQuick: "Back: other ways to sign in", stepUser: "Back: other ways to sign in", stepPassword: "Back: user name"}[l.step]
	hintX := SafeX
	if l.step == stepAddress || l.step == stepUser || l.step == stepPassword {
		hintX = SafeX + 300 // right of the keyboard
	}
	l.app.text(c, hintX, SafeBottom-18, f.SmallBold, gfx.GreyLo, back)
	if l.err != "" || l.stat != "" {
		x, w := SafeX, SafeW
		y := SafeBottom - 2*f.Body.Height() - 10
		if hintX != SafeX {
			x, w, y = hintX, SafeX+SafeW-hintX, SafeY+130
		}
		if l.err != "" {
			for _, line := range wrap(f.Body, l.err, w, 4) {
				l.app.text(c, x, y, f.Body, gfx.GreyHi, line)
				y += f.Body.Height() + 4
			}
		} else {
			l.app.text(c, x, y, f.Body, gfx.GreyLo, f.Body.Fit(l.stat, w))
			if l.busy {
				sweep(c, x, y+f.Body.Height()+3, min(w, f.Body.Width(l.stat)), BarW, now.Sub(l.at))
				anim = true
			}
		}
	}
	return anim
}

// serverName is the probed server's name, its address without one.
func (l *Login) serverName() string {
	if l.info != nil && l.info.ServerName != "" {
		return l.info.ServerName
	}
	return l.host
}
