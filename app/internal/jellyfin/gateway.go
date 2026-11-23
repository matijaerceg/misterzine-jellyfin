package jellyfin

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Gateway owns authenticated HTTP requests and validates every remote URL. The
// decoder receives only a short-lived loopback URL, never the Jellyfin token. Its nested
// playlist requests must also stay on this loopback origin (worker --gateway).
type Gateway struct {
	c              *Client
	base           *url.URL
	server         *http.Server
	origin, prefix string
	client         *http.Client
	closeIdle      func()
	mu             sync.Mutex
	onFailure      func(GatewayFailure)
	delivered      bool // a media request (not a playlist) has been answered
	// trailing counts the media requests that failed since the last one the
	// server answered; lastFailure is the newest of them.
	trailing    int
	lastFailure GatewayFailure
}

// Trailing is how many media requests have failed since the server last
// answered one, with the newest of those failures: above zero once a stream
// has ended, its last requests got no media.
func (g *Gateway) Trailing() (int, GatewayFailure) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.trailing, g.lastFailure
}

// GatewayFailure is a worker request the gateway answered with 502. It
// carries no address, path or credential, so it can go into the playback log.
type GatewayFailure struct {
	// Playlist tells a playlist request from one for media (a file or a
	// segment); judged from the request path when the server sent none.
	Playlist bool
	// Status is the server's HTTP status; 0 when there was no answer or the
	// answer was a playlist the gateway would not pass on.
	Status int
	// Reason says what happened, in words for the log.
	Reason string
	// BeforeMedia is true until the server has answered one media request:
	// the failure is of the stream's start, not of a stream that was playing.
	BeforeMedia bool
}

// Refused reports whether the server answered the request and said no,
// rather than the connection failing.
func (f GatewayFailure) Refused() bool { return f.Status != 0 }

func (f GatewayFailure) String() string {
	kind := "media"
	if f.Playlist {
		kind = "playlist"
	}
	when := "after media was delivered"
	if f.BeforeMedia {
		when = "before any media was delivered"
	}
	return kind + " request failed " + when + ": " + f.Reason
}

// OnFailure names the function told of every request answered with 502. It
// runs on the request's goroutine; set it before the worker has the URL.
func (g *Gateway) OnFailure(fn func(GatewayFailure)) {
	g.mu.Lock()
	g.onFailure = fn
	g.mu.Unlock()
}

// fail answers the worker with 502 and tells the session why. A request the
// worker itself gave up (it exited, or the gateway closed) is not a failure.
func (g *Gateway) fail(w http.ResponseWriter, r *http.Request, playlist bool, status int, reason, public string) {
	http.Error(w, public, 502)
	if r.Context().Err() != nil {
		return
	}
	g.mu.Lock()
	fn := g.onFailure
	f := GatewayFailure{Playlist: playlist, Status: status, Reason: reason, BeforeMedia: !g.delivered}
	if !playlist {
		g.trailing++
		g.lastFailure = f
	}
	g.mu.Unlock()
	if fn != nil {
		fn(f)
	}
}

// connectionReason words a failed request without the address it names.
func connectionReason(err error) string {
	var netErr net.Error
	var certErr *tls.CertificateVerificationError
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "the connection to the server timed out"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "the server refused the connection"
	case errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return "the server closed the connection without an answer"
	case errors.As(err, &certErr):
		return "the server's certificate was not accepted"
	}
	return "the connection to the server failed"
}

func playlistPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".m3u8")
}

func (c *Client) NewGateway(ctx context.Context, tlsWorker ...string) (*Gateway, error) {
	base, err := url.Parse(c.Host)
	if err != nil {
		return nil, errors.New("invalid server URL")
	}
	if err := c.checkURL(base); err != nil {
		return nil, err
	}
	if err := c.ensureIdentity(ctx); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		ln.Close()
		return nil, err
	}
	h := *c.slow
	h.Timeout = 0 // media streams can last hours; request cancellation closes them
	g := &Gateway{c: c, base: base, origin: "http://" + ln.Addr().String(), prefix: "/" + hex.EncodeToString(secret[:]), client: &h}
	if base.Scheme == "https" && len(tlsWorker) > 0 && tlsWorker[0] != "" {
		t := c.transport()
		t.ForceAttemptHTTP2 = false // the verified stdio connection speaks HTTP/1.1
		t.DialTLSContext = tlsBridgeDial(tlsWorker[0])
		h.Transport = checkedTransport{c, t}
		g.closeIdle = t.CloseIdleConnections
	}
	g.server = &http.Server{Handler: g, ReadHeaderTimeout: 10 * time.Second}
	go g.server.Serve(ln)
	return g, nil
}

func (g *Gateway) Close() {
	g.server.Close()
	if g.closeIdle != nil {
		g.closeIdle()
	}
}

func stripKeys(u *url.URL) {
	q := u.Query()
	for k := range q {
		if strings.EqualFold(k, "api_key") || strings.EqualFold(k, "apikey") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
}

func (g *Gateway) URL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid playback URL")
	}
	if err := g.c.checkURL(u); err != nil {
		return "", err
	}
	stripKeys(u)
	return g.origin + g.prefix + u.RequestURI(), nil
}

// HLS has URI attributes (keys, maps, alternate renditions) as well as URI
// lines. Rewrite both and reject every foreign origin before exposing a list.
var hlsURI = regexp.MustCompile(`URI="([^"]*)"`)

func (g *Gateway) playlist(data []byte, base *url.URL) ([]byte, error) {
	resolve := func(raw string) (string, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return "", errors.New("invalid playlist URL")
		}
		return g.URL(base.ResolveReference(u).String())
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		var err error
		if line != "" && !strings.HasPrefix(line, "#") {
			line, err = resolve(line)
		} else {
			line = hlsURI.ReplaceAllStringFunc(line, func(attr string) string {
				raw := hlsURI.FindStringSubmatch(attr)[1]
				value, e := resolve(raw)
				if e != nil {
					err = e
				}
				return `URI="` + value + `"`
			})
		}
		if err != nil {
			return nil, err
		}
		lines[i] = line
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (r.Method != "GET" && r.Method != "HEAD") || !strings.HasPrefix(r.URL.Path, g.prefix+"/") || r.Host != strings.TrimPrefix(g.origin, "http://") {
		http.Error(w, "invalid playback request", http.StatusForbidden)
		return
	}
	u := *g.base
	u.Path = strings.TrimPrefix(r.URL.Path, g.prefix)
	u.RawPath = strings.TrimPrefix(r.URL.RawPath, g.prefix)
	u.RawQuery = r.URL.RawQuery
	stripKeys(&u)
	request, err := http.NewRequestWithContext(r.Context(), r.Method, u.String(), nil)
	if err != nil {
		http.Error(w, "invalid playback request", 400)
		return
	}
	request.Header.Set("Authorization", g.c.AuthHeader())
	for _, name := range []string{"Range", "If-Range"} {
		if v := r.Header.Get(name); v != "" {
			request.Header.Set(name, v)
		}
	}
	resp, err := g.client.Do(request)
	if err != nil {
		g.fail(w, r, playlistPath(u.Path), 0, connectionReason(err), "secure playback connection failed")
		return
	}
	defer resp.Body.Close()
	// Never forward a redirect, cookie, or server-generated authentication URL.
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		g.fail(w, r, playlistPath(u.Path), resp.StatusCode, "the server answered HTTP "+strconv.Itoa(resp.StatusCode), "playback request refused")
		return
	}
	reader := bufio.NewReader(resp.Body)
	peek, _ := reader.Peek(7)
	playlist := string(peek) == "#EXTM3U" || strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".m3u8") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "mpegurl")
	if playlist && r.Method != "HEAD" {
		data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			g.fail(w, r, true, 0, "the server's playlist could not be read or is over 4 MB", "invalid playlist")
			return
		}
		data, err = g.playlist(data, resp.Request.URL)
		if err != nil {
			g.fail(w, r, true, 0, "the server's playlist names an address the app does not allow", "unsafe playlist refused")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Write(data)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(name); v != "" {
			w.Header().Set(name, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	g.mu.Lock()
	g.delivered = true
	g.trailing = 0
	g.mu.Unlock()
	if r.Method != "HEAD" {
		io.Copy(w, reader)
	}
}
