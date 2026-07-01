// Package icmp implements our own ICMP echo (ping) and TTL-stepped probing
// (traceroute) on top of platform sockets. The platform layer is split into
// socket_posix.go (Linux/macOS datagram or raw sockets) and socket_windows.go
// (the iphlpapi IcmpSendEcho API, which needs no administrator rights).
package icmp

import (
	"net"
	"time"
)

// ReplyKind classifies the outcome of a single probe.
type ReplyKind int

const (
	// KindTimeout means no reply arrived within the timeout.
	KindTimeout ReplyKind = iota
	// KindEchoReply means the destination answered (Echo Reply).
	KindEchoReply
	// KindTimeExceeded means an intermediate hop returned TTL Exceeded.
	KindTimeExceeded
	// KindDestUnreachable means a router/host reported the destination unreachable.
	KindDestUnreachable
	// KindError means the probe could not be sent or a fatal socket error occurred.
	KindError
)

func (k ReplyKind) String() string {
	switch k {
	case KindTimeout:
		return "timeout"
	case KindEchoReply:
		return "echo-reply"
	case KindTimeExceeded:
		return "time-exceeded"
	case KindDestUnreachable:
		return "dest-unreachable"
	default:
		return "error"
	}
}

// ProbeResult is the outcome of a single probe.
type ProbeResult struct {
	From net.IP        // responder address (nil on timeout)
	RTT  time.Duration // round-trip time
	Kind ReplyKind
}

// OK reports whether the destination itself replied.
func (r ProbeResult) OK() bool { return r.Kind == KindEchoReply }

// Conn is a platform ICMP socket. Probe is synchronous: it sends one echo and
// waits for the matching reply. A Conn is intended to be used by a single
// goroutine at a time.
type Conn interface {
	// Probe sends one echo to dst with the given IP TTL and DSCP (0-63), waits up
	// to timeout, and returns the result. payloadSize is the ICMP data length.
	Probe(dst net.IP, ttl, dscp int, timeout time.Duration, payloadSize int) (ProbeResult, error)
	// Mode returns a human-readable description of the socket mode in use.
	Mode() string
	Close() error
}

// Open returns an ICMP connection for IPv4, choosing the best available mode for
// the platform. On failure it returns an error explaining the required privilege.
func Open() (Conn, error) { return newConn() }

// makePayload builds an ICMP data payload of the requested size with a recognisable
// pattern. A minimum of a few bytes keeps some stacks happy.
func makePayload(size int) []byte {
	if size < 8 {
		size = 8
	}
	b := make([]byte, size)
	const sig = "proby-icmp-probe"
	for i := range b {
		b[i] = sig[i%len(sig)]
	}
	return b
}
