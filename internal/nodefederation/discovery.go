package nodefederation

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type Discovery struct {
	svc       *Service
	multicast string
	port      int
}

func NewDiscovery(svc *Service, multicast string, port int) *Discovery {
	return &Discovery{svc: svc, multicast: multicast, port: port}
}
func (d *Discovery) Run(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp4", d.multicast)
	if err != nil {
		return err
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("listen OnePane LAN discovery: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(64 << 10)
	go d.announce(ctx, addr)
	buf := make([]byte, 8192)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, src, err := conn.ReadFromUDP(buf)
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				continue
			}
		}
		if err != nil {
			return err
		}
		var a DiscoveryAnnouncement
		if json.Unmarshal(buf[:n], &a) != nil || a.Protocol != ProtocolVersion {
			continue
		}
		_ = d.svc.ObserveDiscovery(ctx, a, src.IP.String())
	}
}
func (d *Discovery) announce(ctx context.Context, addr *net.UDPAddr) {
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return
	}
	defer conn.Close()
	send := func() {
		a := DiscoveryAnnouncement{Protocol: ProtocolVersion, NodeID: d.svc.local.ID, Name: d.svc.local.Name, IdentityFingerprint: d.svc.local.IdentityFingerprint, TLSFingerprint: d.svc.identity.Fingerprint, FederationPort: d.port, SentAt: d.svc.clock.UnixMilli()}
		b, _ := json.Marshal(a)
		_, _ = conn.Write(b)
	}
	send()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}
func ParseMulticast(raw string) (string, int, error) {
	a, err := net.ResolveUDPAddr("udp4", strings.TrimSpace(raw))
	if err != nil {
		return "", 0, err
	}
	if a.IP == nil || !a.IP.IsMulticast() {
		return "", 0, fmt.Errorf("discovery address must be multicast")
	}
	p := a.Port
	if p < 1 {
		return "", 0, fmt.Errorf("invalid multicast port")
	}
	return net.JoinHostPort(a.IP.String(), strconv.Itoa(p)), p, nil
}
