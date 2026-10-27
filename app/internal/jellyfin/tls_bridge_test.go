package jellyfin

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Run on the host with test_tls_bridge, or on ARM with the actual jfplay.
func TestNativeTLSBridge(t *testing.T) {
	binary := os.Getenv("MISTERZINE_TEST_TLS_BRIDGE")
	if binary == "" {
		t.Skip("requires native TLS bridge executable")
	}
	for _, mode := range []string{"valid", "valid-dns", "untrusted", "wrong-name", "expired"} {
		t.Run(mode, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
			if mode == "wrong-name" {
				tmpl.IPAddresses = nil
				tmpl.DNSNames = []string{"other.invalid"}
			}
			if mode == "valid-dns" {
				tmpl.IPAddresses = nil
				tmpl.DNSNames = []string{"localhost"}
			}
			if mode == "expired" {
				tmpl.NotBefore = time.Now().Add(-48 * time.Hour)
				tmpl.NotAfter = time.Now().Add(-24 * time.Hour)
			}
			der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			ca := filepath.Join(t.TempDir(), "ca.pem")
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
				t.Fatal(err)
			}
			if mode != "untrusted" {
				t.Setenv("SSL_CERT_FILE", ca)
			}
			var hits, foreign atomic.Int32
			canceled := make(chan struct{})
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreign.Add(1) }))
			defer other.Close()
			payload := bytes.Repeat([]byte("verified-media-"), 200000)
			s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if mode == "valid-dns" && r.TLS.ServerName != "localhost" {
					t.Error("missing server name indication")
				}
				if !strings.Contains(r.Header.Get("Authorization"), `Token="native-test-token"`) {
					t.Error("missing credential")
				}
				switch r.URL.Path {
				case "/blocked":
					w.Header().Set("Content-Length", "100000")
					w.Write(bytes.Repeat([]byte("x"), 4096))
					w.(http.Flusher).Flush()
					<-r.Context().Done()
					close(canceled)
				case "/redirect":
					http.Redirect(w, r, other.URL, http.StatusFound)
				case "/unsafe.m3u8":
					io.WriteString(w, "#EXTM3U\n"+other.URL+"/segment.ts\n")
				default:
					http.ServeContent(w, r, "video.ts", time.Time{}, bytes.NewReader(payload))
				}
			}))
			s.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
			s.StartTLS()
			defer s.Close()
			store := &MemoryStore{}
			store.SetCredentials(Credentials{Token: "native-test-token"})
			host := s.URL
			if mode == "valid-dns" {
				host = strings.Replace(host, "127.0.0.1", "localhost", 1)
			}
			c := New(host, "", "device", store)
			g, err := c.NewGateway(context.Background(), binary)
			if err != nil {
				t.Fatal(err)
			}
			defer g.Close()
			h := &http.Client{Timeout: 10 * time.Second}
			for i := 0; i < 2; i++ {
				u, _ := g.URL(host + "/media.ts")
				req, _ := http.NewRequest("GET", u, nil)
				if i == 1 {
					req.Header.Set("Range", "bytes=123-456")
				}
				resp, err := h.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(mode, "valid") {
					if resp.StatusCode != 502 || hits.Load() != 0 {
						t.Fatalf("unverified connection: status %d hits %d", resp.StatusCode, hits.Load())
					}
					break
				}
				want := payload
				if i == 1 {
					want = payload[123:457]
				}
				if !bytes.Equal(data, want) {
					t.Fatalf("media mismatch: %d bytes", len(data))
				}
			}
			if strings.HasPrefix(mode, "valid") {
				for _, path := range []string{"/redirect", "/unsafe.m3u8"} {
					u, _ := g.URL(host + path)
					resp, err := h.Get(u)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != 502 {
						t.Fatalf("unsafe response %d", resp.StatusCode)
					}
				}
				if foreign.Load() != 0 {
					t.Fatal("credential escaped to foreign server")
				}
				u, _ := g.URL(host + "/blocked")
				resp, err := h.Get(u)
				if err != nil {
					t.Fatal(err)
				}
				g.Close()
				resp.Body.Close()
				select {
				case <-canceled:
				case <-time.After(2 * time.Second):
					t.Fatal("upstream helper survived gateway close")
				}
			}
		})
	}
}

func TestTLSBridgeCloseReapsProcess(t *testing.T) {
	binary := os.Getenv("MISTERZINE_TEST_TLS_BRIDGE")
	if binary == "" {
		t.Skip("requires native TLS bridge executable")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			<-done
		}
	}()
	conn, err := tlsBridgeDial(binary)(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p := conn.(*tlsPipe)
	if err := conn.SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("read did not time out")
	}
	conn.Close()
	conn.Close()
	if p.cmd.ProcessState == nil {
		t.Fatal("helper was not reaped")
	}
}
