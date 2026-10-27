package jellyfin

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func gatewayFor(t *testing.T, c *Client) *Gateway {
	t.Helper()
	g, err := c.NewGateway(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

func TestGatewayTLSRangeAndCredentials(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.Contains(r.Header.Get("Authorization"), `Token="test-token"`) {
			t.Error("missing upstream token")
		}
		if strings.Contains(r.URL.RawQuery, "token") {
			t.Error("query credential sent")
		}
		if r.Header.Get("Range") != "bytes=2-5" {
			t.Error("range lost")
		}
		w.Header().Set("Content-Range", "bytes 2-5/10")
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Set-Cookie", "private=secret")
		w.WriteHeader(206)
		io.WriteString(w, "2345")
	}))
	defer s.Close()
	store := &MemoryStore{}
	store.SetCredentials(Credentials{Token: "test-token"})
	c := New(s.URL, "", "device", store)
	g := gatewayFor(t, c)
	local, err := g.URL(s.URL + "/video?api_key=test-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(local, "test-token") {
		t.Fatal("token in worker URL")
	}
	request, _ := http.NewRequest("GET", local, nil)
	request.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 502 || hits.Load() != 0 {
		t.Fatal("gateway accepted untrusted certificate")
	}
	g.Close()
	c.SetHTTPClient(s.Client())
	g = gatewayFor(t, c)
	local, _ = g.URL(s.URL + "/video?ApiKey=test-token")
	request, _ = http.NewRequest("GET", local, nil)
	request.Header.Set("Range", "bytes=2-5")
	resp, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(data) != "2345" || resp.Header.Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range response: %d %q", resp.StatusCode, data)
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Fatal("cookie forwarded to worker")
	}
	g.Close()
	if r, e := http.Get(local); e == nil {
		r.Body.Close()
		t.Fatal("closed gateway still accepts requests")
	}
}

func TestGatewayPlaylistAndRedirectBoundaries(t *testing.T) {
	var leaked atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer sink.Close()
	var upstream string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, sink.URL+"/stolen", 307)
		case "/bad.m3u8":
			io.WriteString(w, "#EXTM3U\n"+sink.URL+"/segment.ts\n")
		case "/good.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\""+upstream+"/key?api_key=old-token\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:1,\n/segment.ts\n")
		default:
			io.WriteString(w, "media bytes")
		}
	}))
	defer s.Close()
	upstream = s.URL
	c := New(s.URL, "", "device", nil)
	c.AllowLocalHTTP(true)
	g := gatewayFor(t, c)
	for _, path := range []string{"/redirect", "/bad.m3u8"} {
		local, _ := g.URL(s.URL + path)
		resp, err := http.Get(local)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 502 {
			t.Errorf("unsafe request accepted: %s", path)
		}
	}
	local, _ := g.URL(s.URL + "/good.m3u8")
	resp, err := http.Get(local)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(data)
	if resp.StatusCode != 200 || strings.Contains(body, "old-token") || strings.Contains(body, upstream+"/") {
		t.Fatalf("playlist: %d %s", resp.StatusCode, body)
	}
	for _, suffix := range []string{"/key", "/init.mp4", "/segment.ts"} {
		if !strings.Contains(body, g.origin+g.prefix+suffix) {
			t.Errorf("missing rewritten %s", suffix)
		}
	}
	base, _ := url.Parse(s.URL + "/main.m3u8")
	for _, ref := range []string{sink.URL + "/key", "file:///etc/passwd", "crypto:" + s.URL + "/key", "//evil.example.test/key", "http://user@" + strings.TrimPrefix(s.URL, "http://") + "/key"} {
		if _, err := g.playlist([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\""+ref+"\"\n"), base); err == nil {
			t.Errorf("unsafe key accepted: %s", ref)
		}
	}
	if leaked.Load() != 0 {
		t.Fatal("foreign server contacted")
	}
	resp, err = http.Get(g.origin + "/no-session/video")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("gateway accessible without session capability")
	}
}

func TestGatewayPreservesProxyPathAndCancelsDownloads(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/jellyfin/video%2Fpart" {
			t.Errorf("escaped reverse-proxy path changed: %s", r.URL.EscapedPath())
		}
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer s.Close()
	c := New(s.URL+"/jellyfin", "", "device", nil)
	c.AllowLocalHTTP(true)
	g := gatewayFor(t, c)
	u, err := g.URL(s.URL + "/jellyfin/video%2Fpart")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.Get(u); err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	g.Close()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway left upstream download running")
	}
	<-done
}
