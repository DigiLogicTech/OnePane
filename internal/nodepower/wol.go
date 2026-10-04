package nodepower

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type WOLProvider struct {
	PacketsPerTarget int
	PacketSpacing    time.Duration
	Port             int
}

func (w WOLProvider) Method() WakeMethod { return WakeMethodWOL }

func (w WOLProvider) Wake(ctx context.Context, profile Profile) error {
	if len(profile.WakeTargets) == 0 {
		return errors.New("nodepower: WOL profile has no wake targets")
	}
	packets := w.PacketsPerTarget
	if packets <= 0 {
		packets = 3
	}
	spacing := w.PacketSpacing
	if spacing <= 0 {
		spacing = 150 * time.Millisecond
	}
	port := w.Port
	if port == 0 {
		port = 9
	}

	var successes int
	var lastErr error
	for _, target := range profile.WakeTargets {
		addr := target.Address
		if addr == "" {
			addr = "255.255.255.255"
		}
		packet, err := MagicPacket(target.MAC)
		if err != nil {
			lastErr = err
			continue
		}
		endpoint := fmt.Sprintf("%s:%d", addr, port)
		udpAddr, err := net.ResolveUDPAddr("udp4", endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		conn, err := net.DialUDP("udp4", nil, udpAddr)
		if err != nil {
			lastErr = err
			continue
		}
		ok := true
		for i := 0; i < packets; i++ {
			if err := ctx.Err(); err != nil {
				_ = conn.Close()
				return err
			}
			if _, err := conn.Write(packet); err != nil {
				ok = false
				lastErr = err
				break
			}
			if i+1 < packets {
				timer := time.NewTimer(spacing)
				select {
				case <-ctx.Done():
					timer.Stop()
					_ = conn.Close()
					return ctx.Err()
				case <-timer.C:
				}
			}
		}
		_ = conn.Close()
		if ok {
			successes++
		}
	}
	if successes == 0 {
		if lastErr == nil {
			lastErr = errors.New("no valid WOL target")
		}
		return fmt.Errorf("nodepower: WOL wake failed: %w", lastErr)
	}
	return nil
}

func MagicPacket(macText string) ([]byte, error) {
	mac, err := net.ParseMAC(macText)
	if err != nil {
		return nil, fmt.Errorf("invalid MAC %q: %w", macText, err)
	}
	if len(mac) != 6 {
		return nil, fmt.Errorf("invalid WOL MAC length %d for %q", len(mac), macText)
	}
	packet := make([]byte, 6+16*len(mac))
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	off := 6
	for i := 0; i < 16; i++ {
		copy(packet[off:off+len(mac)], mac)
		off += len(mac)
	}
	return packet, nil
}
