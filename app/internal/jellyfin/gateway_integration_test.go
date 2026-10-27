package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Optional real-demuxer coverage, in addition to the ordinary HTTP tests:
// MISTERZINE_TEST_FFMPEG=1 go test -run TestGatewayFFmpeg ./internal/jellyfin
func TestGatewayFFmpeg(t *testing.T) {
	if os.Getenv("MISTERZINE_TEST_FFMPEG") != "1" {
		t.Skip("requires host FFmpeg with libx264")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v\n%s", err, out)
		}
	}
	run("-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=64x64:r=5", "-t", "2", "-c:v", "libx264", "-threads", "1", "-g", "5", "-pix_fmt", "yuv420p", filepath.Join(dir, "video.mp4"))
	run("-v", "error", "-i", filepath.Join(dir, "video.mp4"), "-c", "copy", "-hls_time", "1", "-hls_list_size", "0", filepath.Join(dir, "main.m3u8"))
	var hits atomic.Int32
	files := http.FileServer(http.Dir(dir))
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="integration-token"`) {
			t.Error("missing media credential")
		}
		hits.Add(1)
		files.ServeHTTP(w, r)
	}))
	defer s.Close()
	store := &MemoryStore{}
	store.SetCredentials(Credentials{Token: "integration-token"})
	c := New(s.URL, "", "device", store)
	c.SetHTTPClient(s.Client())
	g := gatewayFor(t, c)
	for _, path := range []string{"/video.mp4", "/main.m3u8"} {
		u, err := g.URL(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"-v", "error", "-protocol_whitelist", "http,tcp"}
		if strings.HasSuffix(path, ".m3u8") {
			args = append(args, "-http_persistent", "0")
		}
		run(append(args, "-i", u, "-f", "null", "-")...)
		run(append(args, "-ss", "0.8", "-i", u, "-f", "null", "-")...)
	}
	if hits.Load() < 4 {
		t.Fatalf("did not fetch direct media and HLS segments: %d", hits.Load())
	}
}
