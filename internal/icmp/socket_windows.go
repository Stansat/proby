//go:build windows

package icmp

import (
	"fmt"
	"net"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")
)

// Windows IP_STATUS codes (subset) returned in ICMP_ECHO_REPLY.Status.
const (
	ipSuccess            = 0
	ipDestNetUnreachable = 11002
	ipDestHostUnreach    = 11003
	ipDestProtUnreach    = 11004
	ipDestPortUnreach    = 11005
	ipReqTimedOut        = 11010
	ipTTLExpiredTransit  = 11013
	ipTTLExpiredReassem  = 11014
	ipDestUnreachable    = 11040
)

// ipOptionInformation mirrors the C IP_OPTION_INFORMATION struct.
type ipOptionInformation struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData *uint8
}

// icmpEchoReply mirrors the C ICMP_ECHO_REPLY struct (64-bit layout).
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

type winConn struct {
	handle uintptr
	mu     sync.Mutex
}

func newConn() (Conn, error) {
	h, _, err := procIcmpCreateFile.Call()
	if h == 0 || h == uintptr(windows.InvalidHandle) {
		return nil, fmt.Errorf("IcmpCreateFile failed: %v", err)
	}
	return &winConn{handle: h}, nil
}

func (c *winConn) Mode() string { return "IcmpSendEcho (iphlpapi, no admin required)" }

func (c *winConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handle != 0 {
		procIcmpCloseHandle.Call(c.handle)
		c.handle = 0
	}
	return nil
}

func (c *winConn) Probe(dst net.IP, ttl, dscp int, timeout time.Duration, payloadSize int) (ProbeResult, error) {
	d4 := dst.To4()
	if d4 == nil {
		return ProbeResult{Kind: KindError}, fmt.Errorf("icmp windows: only IPv4 supported (got %v)", dst)
	}
	if ttl <= 0 {
		ttl = 64
	}

	req := makePayload(payloadSize)
	opt := ipOptionInformation{TTL: uint8(ttl), TOS: uint8(dscp << 2)}

	replySize := int(unsafe.Sizeof(icmpEchoReply{})) + len(req) + 8
	if replySize < 512 {
		replySize = 512
	}
	reply := make([]byte, replySize)

	dstAddr := uint32(d4[0]) | uint32(d4[1])<<8 | uint32(d4[2])<<16 | uint32(d4[3])<<24
	tmo := timeout.Milliseconds()
	if tmo <= 0 {
		tmo = 1000
	}

	c.mu.Lock()
	handle := c.handle
	c.mu.Unlock()
	if handle == 0 {
		return ProbeResult{Kind: KindError}, fmt.Errorf("icmp windows: closed")
	}

	start := time.Now()
	n, _, _ := procIcmpSendEcho.Call(
		handle,
		uintptr(dstAddr),
		uintptr(unsafe.Pointer(&req[0])),
		uintptr(uint16(len(req))),
		uintptr(unsafe.Pointer(&opt)),
		uintptr(unsafe.Pointer(&reply[0])),
		uintptr(uint32(len(reply))),
		uintptr(uint32(tmo)),
	)
	if n == 0 {
		// No reply structure written. Treat as timeout (the common case); other
		// errors also surface here but timeout is by far the most likely.
		return ProbeResult{Kind: KindTimeout}, nil
	}

	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	from := net.IPv4(byte(r.Address), byte(r.Address>>8), byte(r.Address>>16), byte(r.Address>>24))
	rtt := time.Duration(r.RoundTripTime) * time.Millisecond
	if rtt == 0 {
		rtt = time.Since(start)
	}
	return ProbeResult{From: from, RTT: rtt, Kind: statusToKind(r.Status)}, nil
}

func statusToKind(status uint32) ReplyKind {
	switch status {
	case ipSuccess:
		return KindEchoReply
	case ipTTLExpiredTransit, ipTTLExpiredReassem:
		return KindTimeExceeded
	case ipDestNetUnreachable, ipDestHostUnreach, ipDestProtUnreach,
		ipDestPortUnreach, ipDestUnreachable:
		return KindDestUnreachable
	case ipReqTimedOut:
		return KindTimeout
	default:
		return KindError
	}
}
