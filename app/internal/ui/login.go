package ui

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"plexcrt/internal/gfx"
	"plexcrt/internal/input"
	"plexcrt/internal/jellyfin"
)

// Login is the sign-in: a server found on the local network, or the
// server's address typed on the on-screen keyboard, checked against the
// server's public info; then one of the
// users the server lists (a password typed only for those that have one),
// Quick Connect (a code to approve in another signed-in Jellyfin app) or a
// user name and password. The session is saved into the settings, which
// are the token store, and the app connects. Network work runs in
// goroutines whose answers come back on the UI thread (Later), tagged with
// gen so a step left since cannot be changed by them. Options > Switch user
// opens it at the users of the server on record (NewSwitchUser).
type Login struct {
	app           *App
	step          loginStep
	pad           *keyPad
	stat          string // what is going on, for the status line
	err           string
	gen           atomic.Int64 // bumped on every change of step so stale answers stand down
	at            time.Time
	expectedID    string
	plainAccepted bool
	host          string // the normalized address, once probed
	info          *jellyfin.PublicInfo
	quick         bool                  // the server offers Quick Connect
	users         []jellyfin.PublicUser // the users the server lists, often none
	cur           int                   // the method list's cursor
	user          string                // as the server has it: folded only to draw
	// picked is a user taken from the server's list: only the password is
	// typed, and Back from it returns to the list
	picked bool
	// switching is Options > Switch user: the session on record stays
	// until another is saved, and Back from the list returns to it
	switching bool
	servers   []jellyfin.FoundServer // the servers found on the local network
	scur      int                    // the server list's cursor
	code      string                 // the Quick Connect code shown
	busy      bool
	// client is the sign-in's own client: its session goes into the
	// settings, and the app makes its own client from them.
	client *jellyfin.Client
}

type loginStep int

const (
	stepAddress  loginStep = iota // typing the server's address
	stepMethod                    // the listed users, Quick Connect, or user name and password
	stepQuick                     // showing the code, waiting for approval
	stepUser                      // typing the user name
	stepPassword                  // typing the password
	stepFind                      // looking for servers on the local network
	stepPlain                     // consent before credentials go over local HTTP
	stepServers                   // the servers found, or typing an address
)

// QuickPoll is how often a Quick Connect code is checked for approval.
var QuickPoll = 2 * time.Second

// Discover looks for servers on the local network (jellyfin.Discover); nil
// skips the search. The tests put nil or their own in its place.
var Discover = jellyfin.Discover

// DiscoverWait is how long the sign-in waits for servers to answer.
var DiscoverWait = 1500 * time.Millisecond

// NewLogin starts at the servers found on the local network, or at the
// server's address when none answer, the one on record filled in.
func NewLogin(app *App) *Login {
	l := &Login{app: app, at: time.Now()}
	addr := ""
	if app.Cfg != nil {
		addr = app.Cfg.ServerURL
	}
	l.pad = newKeyPad(addr, "Connect >", false)
	if Discover != nil {
		l.find()
	}
	return l
}

// find asks the local network for Jellyfin servers. Those that answer are
// offered above typing an address, the one on record chosen; with none the
// address is typed as before.
func (l *Login) find() {
	l.to(stepFind)
	gen := l.gen.Load()
	l.busy, l.stat = true, "Looking for servers on your network..."
	discover, wait := Discover, DiscoverWait
	go func() {
		found, err := discover(bg(), wait)
		if err != nil {
			l.app.Log.Printf("sign-in: discovery: %v", err)
		}
		l.later(gen, func() {
			l.servers = found
			if len(found) == 0 {
				l.to(stepAddress)
				return
			}
			l.scur = 0
			for i, s := range found {
				if l.app.Cfg != nil && s.ID != "" && s.ID == l.app.Cfg.ServerID {
					l.scur = i
				}
			}
			l.to(stepServers)
		})
	}()
}

// serverRows are the server list's rows: the servers found, then typing
// an address.
func (l *Login) serverRows() []string {
	var out []string
	for _, s := range l.servers {
		name := s.Name
		if name == "" {
			name = s.Address
		}
		out = append(out, name)
	}
	return append(out, "Enter address")
}

// otherServer returns to the servers found, or to the address without any.
func (l *Login) otherServer() {
	if len(l.servers) > 0 {
		l.to(stepServers)
		return
	}
	l.to(stepAddress)
}

// NewSwitchUser is the sign-in for Options > Switch user: the server on
// record's users and other ways to sign in, its address not typed again.
// Back returns to the session as it was; signing in replaces it.
func NewSwitchUser(app *App) *Login {
	l := &Login{app: app, at: time.Now(), step: stepMethod, host: app.Cfg.ServerURL, switching: true}
	l.pad = newKeyPad(l.host, "Connect >", false)
	l.expectedID = app.Cfg.ServerID
	l.connect([]string{l.host})
	return l
}

// SwitchUser opens the user list of the server on record over Options.
func (a *App) SwitchUser() {
	if a.Client == nil || a.Cfg.ServerURL == "" {
		return
	}
	a.Push(NewSwitchUser(a))
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
// A bare name also tries Jellyfin's plain HTTP port when the first answer
// fails.
func (l *Login) probe() {
	if _, err := jellyfin.NormalizeHost(l.pad.text); err != nil {
		l.err = "Enter the server's address, such as jellyfin.example.com or 192.0.2.10:8096."
		return
	}
	l.gen.Add(1)
	gen, address := l.gen.Load(), l.pad.text
	l.busy, l.err, l.stat = true, "", "Checking the server address..."
	go func() {
		hosts, err := jellyfin.HostCandidates(address)
		l.later(gen, func() {
			l.busy = false
			if err != nil {
				l.err = "Enter the server's address, such as jellyfin.example.com or 192.0.2.10:8096."
				return
			}
			l.expectedID = ""
			l.connect(hosts)
		})
	}()
}

// connect tries the addresses in turn until one answers as a Jellyfin
// server, then asks it for Quick Connect and the users it lists, and moves
// on to the ways to sign in. A failure names the first address tried.
func (l *Login) connect(hosts []string) {
	l.gen.Add(1)
	gen := l.gen.Load()
	expectedID := l.expectedID
	l.err, l.busy, l.stat = "", true, "Connecting to "+hosts[0]+"..."
	go func() {
		var (
			host, first string
			client      *jellyfin.Client
			info        *jellyfin.PublicInfo
			ferr        error
		)
		for _, h := range hosts {
			c := jellyfin.New(h, "", l.app.Cfg.ClientID, nil)
			c.AllowLocalHTTP(true) // public, credential-free probe; dial still enforces local addresses
			i, err := c.PublicInfo(bg())
			if err == nil && expectedID != "" && i.ID != expectedID {
				err = jellyfin.ErrServerIdentity
			}
			if err == nil {
				c.ExpectServer(i.ID)
				host, client, info = h, c, i
				break
			}
			l.app.Log.Printf("sign-in: %s: %v", h, err)
			if ferr == nil || errors.Is(err, jellyfin.ErrPlainHTTP) || errors.Is(err, jellyfin.ErrServerIdentity) {
				first, ferr = h, err
			}
		}
		quick := false
		var users []jellyfin.PublicUser
		if client != nil {
			quick, _ = client.QuickConnectEnabled(bg())
			var err error
			if users, err = client.PublicUsers(bg()); err != nil {
				l.app.Log.Printf("sign-in: users: %v", err) // typing the name still works
			}
		}
		l.later(gen, func() {
			l.busy, l.stat = false, ""
			if client == nil {
				l.err = l.probeFailure(first, ferr)
				if l.switching {
					l.err += " OK: try again."
				}
				return
			}
			l.host, l.info, l.quick, l.users, l.client = host, info, quick, users, client
			l.cur = 0
			l.plainAccepted = l.app.Cfg.PlainHTTPServer == host
			client.AllowLocalHTTP(l.plainAccepted)
			if strings.HasPrefix(host, "http://") && !l.plainAccepted {
				l.to(stepPlain)
			} else {
				l.to(stepMethod)
			}
		})
	}()
}

// probeFailure is the line for an address that gave no Jellyfin answer.
func (l *Login) probeFailure(host string, err error) string {
	var he *jellyfin.HTTPError
	switch {
	case errors.Is(err, jellyfin.ErrPlainHTTP), errors.Is(err, jellyfin.ErrServerIdentity):
		return err.Error()
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

// method is what a row of the method list does.
type method int

const (
	methodUser   method = iota // sign in as a user the server lists
	methodQuick                // Quick Connect
	methodTyped                // type a user name, then its password
	methodServer               // back to the address
)

type loginRow struct {
	label string
	do    method
	user  jellyfin.PublicUser // methodUser's
}

// rows are the method list: the users the server lists, Quick Connect when
// it offers it, typing a user name (Other user under a list of users), and
// another server, except when switching user. None before the server has
// answered.
func (l *Login) rows() []loginRow {
	if l.info == nil {
		return nil
	}
	var out []loginRow
	for _, u := range l.users {
		out = append(out, loginRow{label: jellyfin.Fold(u.Name), do: methodUser, user: u})
	}
	if l.quick {
		out = append(out, loginRow{label: "Quick Connect", do: methodQuick})
	}
	typed := "User name and password"
	if len(l.users) > 0 {
		typed = "Other user"
	}
	out = append(out, loginRow{label: typed, do: methodTyped})
	if !l.switching {
		out = append(out, loginRow{label: "Change server", do: methodServer})
	}
	return out
}

// methods are the method list's labels.
func (l *Login) methods() []string {
	var out []string
	for _, r := range l.rows() {
		out = append(out, r.label)
	}
	return out
}

// pick signs in as a user from the server's list: at once when the user
// has no password, after typing it otherwise.
func (l *Login) pick(u jellyfin.PublicUser) {
	l.user, l.picked = u.Name, true
	if u.Protected() {
		l.to(stepPassword)
		return
	}
	l.signIn("")
}

// startQuick asks for a code and polls it until approved, then redeems it.
func (l *Login) startQuick() {
	l.to(stepQuick)
	gen, client := l.gen.Load(), l.client
	poll := QuickPoll // read here: the goroutine may outlive a test that changes it
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
			time.Sleep(poll)
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
	l.err, l.busy, l.stat = "", true, "Signing in as "+jellyfin.Fold(user)+"..."
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
			case errors.Is(err, jellyfin.ErrUnauthorized) && l.step == stepMethod:
				// listed without a password, yet the server wants one
				l.to(stepPassword)
				l.err = "Enter the password for " + jellyfin.Fold(l.user) + "."
			case errors.Is(err, jellyfin.ErrUnauthorized) && l.picked:
				l.err = "Wrong password."
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
	next.PlainHTTPServer = ""
	if l.plainAccepted && strings.HasPrefix(l.host, "http://") {
		next.PlainHTTPServer = l.host
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
	if l.step == stepFind && ev.Text != 0 && !ev.Release {
		l.to(stepAddress) // typing an address gives up the search
	}
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
	if ev.Release || l.busy && l.step != stepQuick && l.step != stepFind {
		return
	}
	switch l.step {
	case stepPlain:
		if ev.Key == input.Enter && !ev.Repeat {
			l.plainAccepted = true
			l.client.AllowLocalHTTP(true)
			l.app.Log.Printf("sign-in: local HTTP approved; password and session token are unencrypted")
			l.to(stepMethod)
		}
	case stepFind:
		if ev.Key == input.Enter && !ev.Repeat {
			l.to(stepAddress) // no waiting: the address is typed
		}
	case stepServers:
		rows := l.serverRows()
		switch ev.Key {
		case input.Up:
			l.scur = max(0, l.scur-1)
		case input.Down:
			l.scur = min(len(rows)-1, l.scur+1)
		case input.Enter:
			if ev.Repeat {
				return
			}
			if l.scur == len(l.servers) {
				l.to(stepAddress)
				return
			}
			// Prefer TLS, but never rewrite an explicitly advertised HTTPS URL.
			found := l.servers[l.scur]
			l.expectedID = found.ID
			hosts := []string{found.Address}
			if u, err := url.Parse(found.Address); err == nil && u.Scheme == "http" {
				u.Scheme = "https"
				if u.Port() == "8096" {
					u.Host = net.JoinHostPort(u.Hostname(), "8920")
				}
				hosts = append([]string{u.String()}, hosts...)
			}
			l.connect(hosts)
		}
	case stepAddress, stepUser, stepPassword:
		if l.pad.Key(ev) == padDone {
			l.typed()
		} else if ev.Key == input.Enter {
			l.err = ""
		}
	case stepMethod:
		rows := l.rows()
		if len(rows) == 0 {
			// switching user, and the server did not answer: OK asks again
			if ev.Key == input.Enter && !ev.Repeat {
				l.connect([]string{l.host})
			}
			return
		}
		switch ev.Key {
		case input.Up:
			l.cur = max(0, l.cur-1)
		case input.Down:
			l.cur = min(len(rows)-1, l.cur+1)
		case input.Enter:
			if ev.Repeat {
				return
			}
			switch r := rows[l.cur]; r.do {
			case methodUser:
				l.pick(r.user)
			case methodQuick:
				l.startQuick()
			case methodTyped:
				if l.picked {
					l.user, l.picked = "", false
				}
				l.to(stepUser)
			case methodServer:
				l.otherServer()
			}
		}
	case stepQuick:
		if ev.Key == input.Enter && !ev.Repeat && l.err != "" {
			l.startQuick()
		}
	}
}

// Back steps back: the password to the user name (or to the users, for
// one picked from them), the methods to the servers found or the address,
// the address to the servers found or Options, the servers to Options.
// Switching user, the methods return to the session untouched.
func (l *Login) Back() bool {
	switch l.step {
	case stepPlain:
		l.to(stepAddress)
	case stepFind:
		l.app.Push(NewOptions(l.app)) // the search goes on behind
	case stepAddress, stepServers:
		if l.step == stepAddress && len(l.servers) > 0 {
			l.to(stepServers)
			return true
		}
		l.gen.Add(1)
		l.busy, l.stat = false, ""
		l.app.Push(NewOptions(l.app))
	case stepMethod:
		if l.switching {
			l.gen.Add(1) // nothing on its way may sign in now
			l.app.Pop()
			return true
		}
		l.otherServer()
	case stepQuick, stepUser:
		l.to(stepMethod)
	case stepPassword:
		if l.picked {
			l.to(stepMethod)
		} else {
			l.to(stepUser)
		}
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
	case stepPlain:
		l.app.text(c, SafeX, y, f.Big, gfx.White, "Unencrypted local connection")
		y += 34
		l.app.text(c, SafeX, y, f.Small, gfx.GreyLo, f.Small.Fit(l.host, SafeW))
		y += 30
		for _, line := range wrapAll(f.Body, "This server uses plain HTTP. People able to monitor your network can read your password and session token. Quick Connect also exposes its session token. Continue only on a network you trust. Use HTTPS for protection.", SafeW) {
			l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, line)
			y += f.Body.Height() + 6
		}
		l.app.text(c, SafeX, y+16, f.Body, gfx.White, "OK: allow for this server   Back: cancel")
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
			label = "Password for " + jellyfin.Fold(l.user)
		}
		l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, f.Body.Fit(label, SafeW))
		l.pad.Draw(l.app, c, y+f.Body.Height()+10, now)
	case stepMethod:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, f.Big.Fit(l.serverName(), SafeW))
		y += f.Big.Height() + 8
		l.app.text(c, SafeX, y, f.Body, gfx.GreyLo, f.Body.Fit(l.host, SafeW))
		l.list(c, y+f.Body.Height()+24, l.methods(), l.cur, nil)
	case stepFind, stepServers:
		l.app.text(c, SafeX, y, f.Big, gfx.Grey, "Sign in to Jellyfin")
		y += f.Big.Height() + 8
		if l.step == stepServers {
			l.app.text(c, SafeX, y, f.Body, gfx.GreyHi, "Servers on your network")
			l.list(c, y+f.Body.Height()+24, l.serverRows(), l.scur, func(i int) string {
				if i < len(l.servers) && l.servers[i].Name != "" {
					return l.servers[i].Address
				}
				return ""
			})
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
	switch {
	case l.step == stepFind:
		back = "OK: enter the address   Back: Options"
	case l.step == stepServers:
		back = "Back: Options"
	case l.step == stepAddress && len(l.servers) > 0:
		back = "Back: servers found"
	case l.step == stepMethod && l.switching:
		back = "Back: Options" // signed in as before
	case l.step == stepPassword && l.picked:
		back = "Back: other ways to sign in"
	}
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

// list draws rows from y, row cur focused, each with note (when given)
// small on its right. A long list scrolls, clear of the status lines.
func (l *Login) list(c *gfx.Canvas, y int, rows []string, cur int, note func(int) string) {
	f := l.app.F
	shown := max(1, (SafeBottom-2*f.Body.Height()-10-y)/ListRowH)
	first := centredFirst(cur, shown, len(rows))
	menuScrollbar(c, y, (shown-1)*ListRowH+f.Body.Height(), first, shown, len(rows))
	for i := first; i < min(len(rows), first+shown); i++ {
		col := gfx.GreyHi
		if i == cur {
			col = gfx.White
			focusBar(c, SafeX, y, f.Body.Height())
		}
		w := SafeW - 24
		if note != nil {
			if n := note(i); n != "" {
				n = f.Small.Fit(n, w/2)
				l.app.textRight(c, SafeX+w, y+f.Body.Height()-f.Small.Height(), f.Small, gfx.GreyLo, n)
				w -= f.Small.Width(n) + 16
			}
		}
		l.app.text(c, SafeX, y, f.Body, col, f.Body.Fit(rows[i], w))
		y += ListRowH
	}
}

// serverName is the probed server's name, its address without one.
func (l *Login) serverName() string {
	if l.info != nil && l.info.ServerName != "" {
		return l.info.ServerName
	}
	return l.host
}
