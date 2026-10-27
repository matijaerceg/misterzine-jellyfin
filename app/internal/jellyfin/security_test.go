package jellyfin

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPRequiresApprovalAndLocalDestination(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{"Id":"server","Version":"1"}`)
	}))
	defer s.Close()
	c := New(s.URL, "", "device", nil)
	c.Retries = 0
	if _, err := c.PublicInfo(context.Background()); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("HTTP without consent: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("unapproved request escaped")
	}
	c.AllowLocalHTTP(true)
	if _, err := c.PublicInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.AllowLocalHTTP(false)
	if _, err := c.PublicInfo(context.Background()); !errors.Is(err, ErrPlainHTTP) {
		t.Fatalf("revoked consent: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatal("revocation reused an open connection")
	}
	for _, host := range []string{"http://192.0.2.1:8096", "http://[2001:db8::1]:8096"} {
		c := New(host, "", "device", nil)
		c.Retries = 0
		c.AllowLocalHTTP(true)
		if _, err := c.PublicInfo(context.Background()); !errors.Is(err, ErrPlainHTTP) {
			t.Fatalf("public HTTP %s: %v", host, err)
		}
	}
}

func TestTLSExpiredCertificateIsRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(-24 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	s.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	s.StartTLS()
	defer s.Close()
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer tr.CloseIdleConnections()
	c := New(s.URL, "", "device", nil)
	c.SetHTTPClient(&http.Client{Transport: tr})
	c.Retries = 0
	if _, err := c.SignIn(context.Background(), "user", "password"); err == nil {
		t.Fatal("expired certificate accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("password reached expired-certificate server")
	}
}

func TestLocalRanges(t *testing.T) {
	// RFC 1918 network address, generated to avoid resembling a device fixture.
	if !localIP(net.IPv4(192, 168, 0, 0)) {
		t.Fatal("private network rejected")
	}
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.2", "169.254.1.2", "::1", "fd00::1", "fe80::1"} {
		if !localIP(net.ParseIP(s)) {
			t.Errorf("local rejected: %s", s)
		}
	}
	for _, s := range []string{"0.0.0.0", "::", "224.0.0.1", "ff02::1", "192.0.2.1", "2001:db8::1", "172.32.0.1", "::ffff:192.0.2.1"} {
		if localIP(net.ParseIP(s)) {
			t.Errorf("non-local accepted: %s", s)
		}
	}
}

func TestIdentityGateNeverSendsCredentialsToWrongServer(t *testing.T) {
	var private atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), "Token=") {
			t.Error("token on identity probe")
		}
		if r.URL.Path != "/System/Info/Public" {
			private.Add(1)
		}
		io.WriteString(w, `{"Id":"wrong","Version":"1"}`)
	}))
	defer s.Close()
	store := &MemoryStore{}
	store.SetCredentials(Credentials{Token: "test-secret"})
	c := New(s.URL, "", "device", store)
	c.AllowLocalHTTP(true)
	c.ExpectServer("expected")
	if _, err := c.Me(context.Background()); !errors.Is(err, ErrServerIdentity) {
		t.Fatalf("token gate: %v", err)
	}
	if _, err := c.SignIn(context.Background(), "user", "password"); !errors.Is(err, ErrServerIdentity) {
		t.Fatalf("password gate: %v", err)
	}
	if private.Load() != 0 {
		t.Fatal("credentials sent before identity match")
	}
}

func TestRedirectsCannotMoveCredentials(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/steal", code) }))
		c := New(s.URL, "", "device", nil)
		c.SetHTTPClient(s.Client())
		c.AllowLocalHTTP(true)
		c.Retries = 0
		if _, err := c.SignIn(context.Background(), "user", "password"); err == nil {
			t.Errorf("redirect %d accepted", code)
		}
		s.Close()
	}
	if leaked.Load() != 0 {
		t.Fatal("redirect reached credential sink")
	}
}

func TestTLSValidationAndSameOriginRedirect(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/System/Info/Public" {
			http.Redirect(w, r, "/identity", 307)
			return
		}
		io.WriteString(w, `{"Id":"server","Version":"1"}`)
	}))
	defer s.Close()
	c := New(s.URL, "", "device", nil)
	c.Retries = 0
	if _, err := c.PublicInfo(context.Background()); err == nil {
		t.Fatal("untrusted certificate accepted")
	}
	if hits.Load() != 0 {
		t.Fatal("HTTP request reached untrusted TLS server")
	}
	c.SetHTTPClient(s.Client())
	if _, err := c.PublicInfo(context.Background()); err != nil {
		t.Fatal("trusted TLS / same-origin redirect:", err)
	}
	// Trust its CA but deliberately request a different DNS name. The custom
	// dial reaches the fixture; normal hostname verification must still fail.
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	u, _ := url.Parse(s.URL)
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, u.Host)
	}}
	defer tr.CloseIdleConnections()
	bad := New("https://wrong.example.test", "", "device", nil)
	bad.Retries = 0
	bad.SetHTTPClient(&http.Client{Transport: tr})
	before := hits.Load()
	if _, err := bad.PublicInfo(context.Background()); err == nil {
		t.Fatal("wrong hostname accepted")
	}
	if hits.Load() != before {
		t.Fatal("request reached wrong-host TLS server")
	}
}
