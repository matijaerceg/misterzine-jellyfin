package jellyfin

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestParseDiscovery(t *testing.T) {
	s, ok := ParseDiscovery([]byte(`{"Address":"http://192.0.2.10:8096/","Id":"ab2fcab83d844492a4ecc65a0fca36cd","Name":"Café","EndpointAddress":null}`))
	if !ok || s.Address != "http://192.0.2.10:8096" || s.ID != "ab2fcab83d844492a4ecc65a0fca36cd" || s.Name != "Cafe" {
		t.Fatalf("parsed %+v %v", s, ok)
	}
	for _, bad := range []string{
		`Who is JellyfinServer?`,
		`{"Address":"http://192.0.2.10:8096","Name":"no id"}`,
		`{"Address":"","Id":"x"}`,
		`{"Address":"ftp://192.0.2.10","Id":"x"}`,
	} {
		if s, ok := ParseDiscovery([]byte(bad)); ok {
			t.Errorf("%s taken for %+v", bad, s)
		}
	}
}

// responder is a fake server's discovery port on loopback: it answers the
// question, and only the question, with each of answers.
func responder(t *testing.T, answers ...string) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1024)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) != discoveryAsk {
				continue
			}
			for _, a := range answers {
				conn.WriteToUDP([]byte(a), from)
			}
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr)
}

// Discovery lists each server that answers once, whatever else arrives:
// asked twice, a server answers twice.
func TestDiscover(t *testing.T) {
	one := responder(t, `{"Address":"http://192.0.2.10:8096","Id":"one","Name":"Den"}`, `not json`)
	two := responder(t, `{"Address":"https://jellyfin.example.com","Id":"two","Name":"Attic"}`)
	start := time.Now()
	found, err := discover(context.Background(), []*net.UDPAddr{one, one, two}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]FoundServer{}
	for _, s := range found {
		byID[s.ID] = s
	}
	if len(found) != 2 || byID["one"].Name != "Den" || byID["two"].Address != "https://jellyfin.example.com" {
		t.Fatalf("found %+v", found)
	}
	if took := time.Since(start); took < 300*time.Millisecond || took > 2*time.Second {
		t.Fatalf("discovery took %v for a wait of 300ms", took)
	}

	// nothing answers: an empty list once the wait is up, or at once when
	// the context ends
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start = time.Now()
	silent := responder(t)
	if found, err := discover(ctx, []*net.UDPAddr{silent}, 5*time.Second); err != nil || len(found) != 0 || time.Since(start) > 2*time.Second {
		t.Fatalf("silence: %+v %v after %v", found, err, time.Since(start))
	}
}

// The question goes to the limited broadcast address first, on Jellyfin's
// discovery port, and never to loopback.
func TestBroadcastTargets(t *testing.T) {
	targets := broadcastTargets()
	if len(targets) == 0 || !targets[0].IP.Equal(net.IPv4bcast) {
		t.Fatalf("targets %v", targets)
	}
	for _, a := range targets {
		if a.Port != DiscoveryPort || a.IP.IsLoopback() || a.IP.To4() == nil {
			t.Errorf("target %v", a)
		}
	}
}
