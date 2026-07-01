//go:build !windows

package icmp

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// protocolICMP is the IANA protocol number for ICMPv4.
const protocolICMP = 1

type posixConn struct {
	pc       *icmp.PacketConn
	p4       *ipv4.PacketConn
	mode     string
	datagram bool
	id       int

	mu  sync.Mutex
	seq int
}

func newConn() (Conn, error) {
	// Prefer the unprivileged datagram socket (Linux ping_group_range / macOS).
	if pc, err := icmp.ListenPacket("udp4", "0.0.0.0"); err == nil {
		return wrapPosix(pc, "unprivileged datagram socket (SOCK_DGRAM)", true)
	}
	// Fall back to a raw socket (needs root or CAP_NET_RAW).
	pc, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("no ICMP socket available (grant CAP_NET_RAW, run as root, "+
			"or widen net.ipv4.ping_group_range): %w", err)
	}
	return wrapPosix(pc, "raw socket (ip4:icmp)", false)
}

func wrapPosix(pc *icmp.PacketConn, mode string, datagram bool) (Conn, error) {
	p4 := pc.IPv4PacketConn()
	if p4 == nil {
		pc.Close()
		return nil, fmt.Errorf("icmp: no IPv4 packet conn")
	}
	return &posixConn{
		pc:       pc,
		p4:       p4,
		mode:     mode,
		datagram: datagram,
		id:       os.Getpid() & 0xffff,
	}, nil
}

func (c *posixConn) Mode() string { return c.mode }
func (c *posixConn) Close() error { return c.pc.Close() }

func (c *posixConn) Probe(dst net.IP, ttl, dscp int, timeout time.Duration, payloadSize int) (ProbeResult, error) {
	d4 := dst.To4()
	if d4 == nil {
		return ProbeResult{Kind: KindError}, fmt.Errorf("icmp: only IPv4 supported (got %v)", dst)
	}
	if ttl <= 0 {
		ttl = 64
	}

	c.mu.Lock()
	c.seq = (c.seq + 1) & 0xffff
	seq := c.seq
	c.mu.Unlock()

	if err := c.p4.SetTTL(ttl); err != nil {
		return ProbeResult{Kind: KindError}, fmt.Errorf("set ttl: %w", err)
	}
	// DSCP occupies the top 6 bits of the ToS byte; best-effort.
	_ = c.p4.SetTOS(dscp << 2)

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{ID: c.id, Seq: seq, Data: makePayload(payloadSize)},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return ProbeResult{Kind: KindError}, err
	}

	var dstAddr net.Addr
	if c.datagram {
		dstAddr = &net.UDPAddr{IP: d4}
	} else {
		dstAddr = &net.IPAddr{IP: d4}
	}

	start := time.Now()
	if _, err := c.pc.WriteTo(wb, dstAddr); err != nil {
		return ProbeResult{Kind: KindError}, fmt.Errorf("write: %w", err)
	}

	deadline := start.Add(timeout)
	rb := make([]byte, 1500)
	for {
		if err := c.pc.SetReadDeadline(deadline); err != nil {
			return ProbeResult{Kind: KindError}, err
		}
		n, peer, err := c.pc.ReadFrom(rb)
		if err != nil {
			return ProbeResult{Kind: KindTimeout}, nil
		}
		rm, err := icmp.ParseMessage(protocolICMP, rb[:n])
		if err != nil {
			continue
		}
		switch rm.Type {
		case ipv4.ICMPTypeEchoReply:
			echo, ok := rm.Body.(*icmp.Echo)
			if !ok || echo.Seq != seq {
				continue
			}
			return ProbeResult{From: addrIP(peer), RTT: time.Since(start), Kind: KindEchoReply}, nil
		case ipv4.ICMPTypeTimeExceeded:
			if body, ok := rm.Body.(*icmp.TimeExceeded); ok && quotedSeqMatches(body.Data, seq) {
				return ProbeResult{From: addrIP(peer), RTT: time.Since(start), Kind: KindTimeExceeded}, nil
			}
		case ipv4.ICMPTypeDestinationUnreachable:
			if body, ok := rm.Body.(*icmp.DstUnreach); ok && quotedSeqMatches(body.Data, seq) {
				return ProbeResult{From: addrIP(peer), RTT: time.Since(start), Kind: KindDestUnreachable}, nil
			}
		}
		// Not ours (raw socket may see unrelated traffic); keep waiting.
	}
}

func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.UDPAddr:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

// quotedSeqMatches parses the invoking packet embedded in an ICMP error and reports
// whether its echo sequence matches seq. If it cannot be parsed, it returns true so
// the caller (which only reads its own socket) still counts the hop.
func quotedSeqMatches(data []byte, seq int) bool {
	if len(data) < 20 {
		return true
	}
	ihl := int(data[0]&0x0f) * 4
	if ihl < 20 || len(data) < ihl+8 {
		return true
	}
	inner := data[ihl:]
	// inner[0] type, inner[1] code, [2:4] checksum, [4:6] id, [6:8] seq
	gotSeq := int(inner[6])<<8 | int(inner[7])
	return gotSeq == seq
}
