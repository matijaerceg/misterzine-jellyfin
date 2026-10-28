// Package jellyfin is the thin server client for a Jellyfin server: sign-in,
// libraries, item listings, shows, artwork, search, Continue Watching,
// watched state and playback reporting, all as JSON over the server's REST
// API. It is the UI's only server client (docs/JELLYFIN_API.md).
//
// Authenticated requests carry the token in the Authorization header
// (MediaBrowser ..., Token="..."), never in a URL: Jellyfin 12 refuses the
// ?api_key= form on its websocket, and a URL ends up in logs. Nothing here
// logs a token or a request query.
package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Product is what the server sees this client as (the Client= field).
const Product = "MisterZine"

// ClientVersion is the client version sent in the Authorization header.
var ClientVersion = "0.1"

// TicksPerSecond is Jellyfin's time unit: one tick is 100 ns.
const TicksPerSecond = 10_000_000

// Seconds converts ticks to whole seconds.
func Seconds(ticks int64) int { return int(ticks / TicksPerSecond) }

// Ticks converts seconds to ticks.
func Ticks(seconds float64) int64 { return int64(seconds * TicksPerSecond) }

// ErrUnauthorized is a 401 from the server: the token is missing, revoked
// or expired. The UI returns to sign-in on it (errors.Is).
var ErrUnauthorized = errors.New("jellyfin: not signed in")

// HTTPError is any other non-2xx answer. Only the path is kept, never the
// query, which can hold a Quick Connect secret.
type HTTPError struct {
	Method string
	Path   string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
}

// Is makes errors.Is(err, ErrUnauthorized) true for a 401.
func (e *HTTPError) Is(target error) bool {
	return target == ErrUnauthorized && e.Status == http.StatusUnauthorized
}

// Credentials is a signed-in session on one server.
type Credentials struct {
	UserID   string
	UserName string
	Token    string
	ServerID string
}

// TokenStore holds the session. The UI's config can implement it to persist
// the token; MemoryStore is the default.
type TokenStore interface {
	Credentials() Credentials
	SetCredentials(Credentials)
}

// MemoryStore is a TokenStore kept in memory only.
type MemoryStore struct {
	mu sync.Mutex
	c  Credentials
}

func (m *MemoryStore) Credentials() Credentials     { m.mu.Lock(); defer m.mu.Unlock(); return m.c }
func (m *MemoryStore) SetCredentials(c Credentials) { m.mu.Lock(); m.c = c; m.mu.Unlock() }

// Client talks to one server.
type Client struct {
	Host       string // https://jellyfin.example.com, no trailing slash
	CacheDir   string // artwork and theme cache
	DeviceID   string // stable per install; Jellyfin keys sessions on it
	DeviceName string
	Store      TokenStore
	Logf       func(string, ...any) // optional; never handed a token or a query

	plainHTTP        atomic.Bool
	expectedID       string
	identityMu       sync.Mutex
	identityChecked  bool
	identityRequired bool
	http             *http.Client
	slow             *http.Client // the home rows, which a large library answers slowly
	refused          atomic.Bool  // the server has answered this client 401 (Refused)
	versionMu        sync.Mutex
	serverMajor      int // the server's major version (ownResumePoints): 0 unread, -1 unknown

	// Retries and Backoff govern idempotent requests that fail on the
	// network or with a 5xx: Retries more attempts, waiting Backoff, then
	// three times as long, and so on.
	Retries int
	Backoff time.Duration
}

// New makes a client for host. The device id should be stable across runs
// (Config.ClientID); the token comes from the store, or is set by a
// sign-in.
func New(host, cacheDir, deviceID string, store TokenStore) *Client {
	if cacheDir != "" {
		os.MkdirAll(cacheDir, 0o755)
	}
	if deviceID == "" {
		deviceID = "mister-misterzine-0001"
	}
	if store == nil {
		store = &MemoryStore{}
	}
	c := &Client{Host: strings.TrimRight(host, "/"), CacheDir: cacheDir, DeviceID: deviceID,
		DeviceName: "MiSTer CRT", Store: store,
		http:    &http.Client{Timeout: 10 * time.Second},
		slow:    &http.Client{Timeout: 30 * time.Second},
		Retries: 2, Backoff: 250 * time.Millisecond}
	c.http = c.checkedClient(c.http)
	c.slow = c.checkedClient(c.slow)
	return c
}

// SetHTTPClient replaces both transports (tests, a custom TLS config).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http, c.slow = c.checkedClient(h), c.checkedClient(h)
}

// UserID is the signed-in user, "" before sign-in.
func (c *Client) UserID() string { return c.Store.Credentials().UserID }

// SignedIn reports whether there is a token to send.
func (c *Client) SignedIn() bool { return c.Store.Credentials().Token != "" }

// Refused reports whether the server has answered any of this client's
// requests 401: its session, not some other client's, has ended.
func (c *Client) Refused() bool { return c.refused.Load() }

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// quote makes a value safe inside the header's double quotes.
func quote(s string) string {
	return strings.NewReplacer(`"`, "'", "\r", " ", "\n", " ", ",", " ").Replace(s)
}

// AuthHeader is the Authorization header value, with the token when there
// is one. The playback gateway uses it; the decoder never receives it.
func (c *Client) AuthHeader() string {
	h := fmt.Sprintf(`MediaBrowser Client="%s", Device="%s", DeviceId="%s", Version="%s"`,
		quote(Product), quote(c.DeviceName), quote(c.DeviceID), quote(ClientVersion))
	if tok := c.Store.Credentials().Token; tok != "" {
		h += `, Token="` + quote(tok) + `"`
	}
	return h
}

// URL is the absolute URL of path (which may carry its own query) with q
// merged in. It never holds the token.
func (c *Client) URL(path string, q url.Values) string {
	if len(q) == 0 {
		return c.Host + path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return c.Host + path + sep + q.Encode()
}

// do sends one request and returns the body of a 2xx answer. GETs (and
// callers that pass retry) are retried on network errors and 5xx.
func (c *Client) do(ctx context.Context, cl *http.Client, method, path string, q url.Values, body any, retry bool, limit int64) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var payload []byte
	if body != nil {
		switch b := body.(type) {
		case json.RawMessage:
			payload = b
		default:
			var err error
			if payload, err = json.Marshal(body); err != nil {
				return nil, err
			}
		}
	}
	attempts := 1
	if retry {
		attempts += max(c.Retries, 0)
	}
	wait := c.Backoff
	var last error
	for n := range attempts {
		if n > 0 {
			c.logf("jellyfin: %s %s: retry %d after %v", method, pathOnly(path), n, last)
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, ctx.Err()
			case <-t.C:
			}
			wait *= 3
		}
		data, err, again := c.once(ctx, cl, method, path, q, payload, limit)
		if err == nil || !again {
			return data, err
		}
		last = err
	}
	return nil, last
}

func pathOnly(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i]
	}
	return p
}

// once is one attempt; again says whether a retry may help.
func (c *Client) once(ctx context.Context, cl *http.Client, method, path string, q url.Values, payload []byte, limit int64) ([]byte, error, bool) {
	// a POST always has a body, if empty: through nginx over HTTP/2 a
	// bodiless POST to /QuickConnect/Initiate is answered 400
	r, err := http.NewRequestWithContext(ctx, method, c.URL(path, q), bytes.NewReader(payload))
	if err != nil {
		return nil, err, false
	}
	if path == "/System/Info/Public" {
		public := Client{DeviceName: c.DeviceName, DeviceID: c.DeviceID, Store: &MemoryStore{}}
		r.Header.Set("Authorization", public.AuthHeader())
	} else {
		if err := c.ensureIdentity(ctx); err != nil {
			return nil, err, false
		}
		r.Header.Set("Authorization", c.AuthHeader())
	}
	r.Header.Set("Accept", "application/json")
	if len(payload) > 0 {
		r.Header.Set("Content-Type", "application/json")
	}
	resp, err := cl.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err(), false
		}
		return nil, fmt.Errorf("%s %s: %w", method, pathOnly(path), err), true
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		e := &HTTPError{Method: method, Path: pathOnly(path), Status: resp.StatusCode}
		if resp.StatusCode == http.StatusUnauthorized {
			c.refused.Store(true)
			return nil, e, false
		}
		return nil, e, resp.StatusCode >= 500
	}
	if limit <= 0 {
		limit = 32 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, pathOnly(path), err), true
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s %s: answer larger than %d bytes", method, pathOnly(path), limit), false
	}
	return data, nil, false
}

func (c *Client) getJSON(ctx context.Context, cl *http.Client, path string, q url.Values, v any) error {
	data, err := c.do(ctx, cl, "GET", path, q, nil, true, 0)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("GET %s: %w", pathOnly(path), err)
	}
	return nil
}

// post sends a JSON body (nil for none) and decodes the answer into v when
// v is non-nil. POSTs are not retried: most change state.
func (c *Client) post(ctx context.Context, path string, q url.Values, body, v any) error {
	if body == nil {
		body = json.RawMessage{}
	}
	data, err := c.do(ctx, c.http, "POST", path, q, body, false, 0)
	if err != nil {
		return err
	}
	if v == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("POST %s: %w", pathOnly(path), err)
	}
	return nil
}

func (c *Client) del(ctx context.Context, path string, q url.Values) error {
	_, err := c.do(ctx, c.http, "DELETE", path, q, nil, true, 0)
	return err
}

// userQuery is a query with the signed-in user's id, which the
// user-scoped endpoints take as userId.
func (c *Client) userQuery() url.Values {
	q := url.Values{}
	if id := c.UserID(); id != "" {
		q.Set("userId", id)
	}
	return q
}

func itoa(n int) string { return strconv.Itoa(n) }
