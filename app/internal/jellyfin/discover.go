package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

// Discovery is Jellyfin's own way for a client to find servers on the local
// network, as the official clients do: a UDP broadcast of "Who is
// JellyfinServer?" to port 7359, which every server answers, to the
// sender, with its address as JSON.

// DiscoveryPort is the UDP port Jellyfin servers listen on for discovery.
const DiscoveryPort = 7359

const discoveryAsk = "Who is JellyfinServer?"

// FoundServer is a server that answered discovery.
type FoundServer struct {
	Address  string `json:"Address"` // its base URL, as HostCandidates takes it
	ID       string `json:"Id"`
	Name     string `json:"Name"`
	Endpoint string `json:"EndpointAddress"`
}

// ParseDiscovery reads a server's answer to discovery. One without an id
// or a usable address is no answer; the name is folded for drawing.
func ParseDiscovery(data []byte) (FoundServer, bool) {
	var s FoundServer
	if json.Unmarshal(data, &s) != nil || s.ID == "" {
		return FoundServer{}, false
	}
	host, err := NormalizeHost(s.Address)
	if err != nil {
		return FoundServer{}, false
	}
	s.Address, s.Name = host, Fold(s.Name)
	return s, true
}

// Discover asks the local network for Jellyfin servers and lists those that
// answer within wait, each once, in the order they answered. It asks on the
// limited broadcast address, which leaves only by the default route, and on
// each IPv4 interface's own broadcast address, for a network without a
// gateway or a second interface.
func Discover(ctx context.Context, wait time.Duration) ([]FoundServer, error) {
	return discover(ctx, broadcastTargets(), wait)
}

// broadcastTargets are the addresses discovery is sent to.
func broadcastTargets() []*net.UDPAddr {
	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: DiscoveryPort}}
	seen := map[string]bool{net.IPv4bcast.String(): true}
	ifaces, err := net.Interfaces()
	if err != nil {
		return targets
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, mask := n.IP.To4(), n.Mask
			if len(mask) == net.IPv6len {
				mask = mask[12:]
			}
			if ip == nil || len(mask) != net.IPv4len {
				continue
			}
			b := make(net.IP, net.IPv4len)
			for i := range b {
				b[i] = ip[i] | ^mask[i]
			}
			if !seen[b.String()] && !b.Equal(ip) {
				seen[b.String()] = true
				targets = append(targets, &net.UDPAddr{IP: b, Port: DiscoveryPort})
			}
		}
	}
	return targets
}

// discover sends the question to each target from one socket and gathers
// the answers until wait is up or ctx ends. An error only when the
// question could be sent nowhere: no network.
func discover(ctx context.Context, targets []*net.UDPAddr, wait time.Duration) ([]FoundServer, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	sent := 0
	serr := errors.New("nowhere to send discovery")
	for _, t := range targets {
		if _, err := conn.WriteToUDP([]byte(discoveryAsk), t); err != nil {
			serr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		return nil, serr
	}
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	defer context.AfterFunc(ctx, func() { conn.SetReadDeadline(time.Now()) })()
	var found []FoundServer
	seen := map[string]bool{}
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return found, nil // the wait is over
		}
		// each target can bring the same server's answer again
		if s, ok := ParseDiscovery(buf[:n]); ok && !seen[s.ID] {
			seen[s.ID] = true
			found = append(found, s)
		}
	}
}
