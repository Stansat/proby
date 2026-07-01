package icmp

import (
	"net"
	"time"
)

// TraceHop is the aggregated result for one TTL in a single traceroute cycle.
type TraceHop struct {
	TTL     int
	Addr    net.IP        // first responder for this hop (nil if none replied)
	RTT     time.Duration // average RTT of the replies that arrived
	Recv    int           // replies received
	Queries int           // probes sent
	Reached bool          // destination answered at this TTL
}

// Trace runs one MTR-style traceroute cycle to dst: for each TTL from 1..maxHops it
// sends `queries` probes and records responders. It stops after the TTL at which the
// destination replies.
func Trace(conn Conn, dst net.IP, maxHops, queries, dscp int, timeout time.Duration) ([]TraceHop, error) {
	if maxHops <= 0 {
		maxHops = 30
	}
	if queries <= 0 {
		queries = 3
	}
	hops := make([]TraceHop, 0, maxHops)

	for ttl := 1; ttl <= maxHops; ttl++ {
		hop := TraceHop{TTL: ttl, Queries: queries}
		var total time.Duration
		for q := 0; q < queries; q++ {
			r, err := conn.Probe(dst, ttl, dscp, timeout, 56)
			if err != nil {
				continue
			}
			switch r.Kind {
			case KindEchoReply:
				hop.Recv++
				total += r.RTT
				if hop.Addr == nil {
					hop.Addr = r.From
				}
				hop.Reached = true
			case KindTimeExceeded, KindDestUnreachable:
				hop.Recv++
				total += r.RTT
				if hop.Addr == nil {
					hop.Addr = r.From
				}
			}
		}
		if hop.Recv > 0 {
			hop.RTT = total / time.Duration(hop.Recv)
		}
		hops = append(hops, hop)
		if hop.Reached {
			break
		}
	}
	return hops, nil
}
