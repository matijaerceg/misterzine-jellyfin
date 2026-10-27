package jellyfin

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A tiny isolated DNS fixture lets us test mixed public/private answers and a
// name changing between the request check and the socket dial, without relying
// on external DNS. AAAA receives an empty answer; A uses the supplied sequence.
func dnsFixture(t *testing.T, answer func() []net.IP) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", conn.LocalAddr().String())
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			if len(q) < 17 {
				continue
			}
			i := 12
			for i < len(q) && q[i] != 0 {
				i += int(q[i]) + 1
			}
			i++
			if i+4 > len(q) {
				continue
			}
			typ := binary.BigEndian.Uint16(q[i : i+2])
			q = q[:i+4]
			q[2] = 0x81
			q[3] = 0x80
			q[6] = 0
			q[7] = 0
			q[8] = 0
			q[9] = 0
			q[10] = 0
			q[11] = 0
			if typ == 1 {
				for _, ip := range answer() {
					q[7]++
					q = append(q, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
					q = append(q, ip.To4()...)
				}
			}
			conn.WriteTo(q, addr)
		}
	}()
	t.Cleanup(func() { net.DefaultResolver = old; conn.Close(); <-done })
}

func TestHTTPRejectsMixedDNSAnswers(t *testing.T) {
	dnsFixture(t, func() []net.IP { return []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.0.2.1")} })
	c := New("http://mixed.example.test:8096", "", "device", nil)
	c.AllowLocalHTTP(true)
	c.Retries = 0
	if _, err := c.PublicInfo(context.Background()); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("mixed DNS accepted: %v", err)
	}
}

func TestHTTPRejectsDNSRebindingBeforeDial(t *testing.T) {
	var lookups, hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer s.Close()
	dnsFixture(t, func() []net.IP {
		if lookups.Add(1) == 1 {
			return []net.IP{net.ParseIP("127.0.0.1")}
		}
		return []net.IP{net.ParseIP("192.0.2.1")}
	})
	c := New(strings.Replace(s.URL, "127.0.0.1", "rebind.example.test", 1), "", "device", nil)
	c.AllowLocalHTTP(true)
	c.Retries = 0
	if _, err := c.SignIn(context.Background(), "user", "password"); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("rebound DNS accepted: %v", err)
	}
	if lookups.Load() < 2 || hits.Load() != 0 {
		t.Fatalf("dial did not recheck DNS: lookups=%d hits=%d", lookups.Load(), hits.Load())
	}
}
