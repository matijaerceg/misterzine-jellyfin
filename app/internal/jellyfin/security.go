package jellyfin

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrPlainHTTP = errors.New("plain HTTP requires approval for a local server; use HTTPS for remote servers")
var ErrServerIdentity = errors.New("server identity changed; sign in again at the correct server address")

// AllowLocalHTTP records the caller's informed choice. It never permits public HTTP.
func (c *Client) AllowLocalHTTP(allow bool) { c.plainHTTP.Store(allow) }

// ExpectServer must be set before using a client concurrently. A saved identity
// is checked without credentials before the first authenticated request.
func (c *Client) ExpectServer(id string) {
	c.expectedID = id
	c.identityRequired = true
}

func localIP(ip net.IP) bool {
	return ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast())
}

func localAddresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, ErrPlainHTTP
	}
	for _, ip := range ips {
		if !localIP(ip.IP) {
			return nil, ErrPlainHTTP
		}
	}
	return ips, nil
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host) && b.User == nil
}

func (c *Client) checkURL(u *url.URL) error {
	base, err := url.Parse(c.Host)
	if err != nil || u == nil || u.Hostname() == "" || !sameOrigin(base, u) {
		return errors.New("connection to another server refused")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" || !c.plainHTTP.Load() {
		return ErrPlainHTTP
	}
	return nil
}

type checkedTransport struct {
	c    *Client
	next http.RoundTripper
}

func (t checkedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.c.checkURL(r.URL); err != nil {
		return nil, err
	}
	if r.URL.Scheme == "http" {
		if _, err := localAddresses(r.Context(), r.URL.Hostname()); err != nil {
			return nil, err
		}
	}
	return t.next.RoundTrip(r)
}

func (c *Client) transport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy could send an approved home-network request outside that network.
	t.Proxy = nil
	dial := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		base, _ := url.Parse(c.Host)
		if base != nil && base.Scheme == "http" {
			if !c.plainHTTP.Load() {
				return nil, ErrPlainHTTP
			}
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := localAddresses(ctx, host)
			if err != nil {
				return nil, err
			}
			// Dial the checked IP itself: a second DNS lookup could rebind it.
			var last error
			for _, ip := range ips {
				conn, err := dial.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		}
		return dial.DialContext(ctx, network, address)
	}
	return t
}

func (c *Client) checkedClient(h *http.Client) *http.Client {
	copy := *h
	transport := h.Transport
	if transport == nil {
		transport = c.transport()
	}
	copy.Transport = checkedTransport{c, transport}
	copy.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return c.checkURL(r.URL)
	}
	return &copy
}

func (c *Client) ensureIdentity(ctx context.Context) error {
	if !c.identityRequired {
		return nil
	}
	c.identityMu.Lock()
	defer c.identityMu.Unlock()
	if c.identityChecked {
		return nil
	}
	info, err := c.PublicInfo(ctx)
	if err != nil {
		return err
	}
	if c.expectedID != "" && info.ID != c.expectedID {
		return ErrServerIdentity
	}
	c.identityChecked = true
	return nil
}
