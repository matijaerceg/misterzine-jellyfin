package jellyfin

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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
		http.Error(w, "secure playback connection failed", 502)
		return
	}
	defer resp.Body.Close()
	// Never forward a redirect, cookie, or server-generated authentication URL.
	if resp.StatusCode != 200 && resp.StatusCode != 206 {
		http.Error(w, "playback request refused", 502)
		return
	}
	reader := bufio.NewReader(resp.Body)
	peek, _ := reader.Peek(7)
	playlist := string(peek) == "#EXTM3U" || strings.HasSuffix(strings.ToLower(resp.Request.URL.Path), ".m3u8") || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "mpegurl")
	if playlist && r.Method != "HEAD" {
		data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			http.Error(w, "invalid playlist", 502)
			return
		}
		data, err = g.playlist(data, resp.Request.URL)
		if err != nil {
			http.Error(w, "unsafe playlist refused", 502)
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
	if r.Method != "HEAD" {
		io.Copy(w, reader)
	}
}
