// Package connectivity implements the step-2 reachability gate: it pings a set of
// check hosts and passes if at least one replies.
package connectivity

import (
	"fmt"
	"net"
	"time"

	"github.com/stansat/proby/internal/icmp"
)

// HostResult is the outcome for one check host.
type HostResult struct {
	Host     string
	IP       net.IP
	Sent     int
	Received int
	AvgRTT   time.Duration
	Err      error // set when the host could not be resolved/probed at all
}

// OK reports whether the host produced at least one reply.
func (h HostResult) OK() bool { return h.Received > 0 }

// Result aggregates all host results.
type Result struct {
	Hosts  []HostResult
	Passed bool // true if any host replied
}

// Check pings each host `count` times using conn and returns the aggregate result.
// Passed is true if at least one host replied.
func Check(conn icmp.Conn, hosts []string, count int, timeout time.Duration) Result {
	if count <= 0 {
		count = 1
	}
	res := Result{}
	for _, host := range hosts {
		hr := checkHost(conn, host, count, timeout)
		if hr.OK() {
			res.Passed = true
		}
		res.Hosts = append(res.Hosts, hr)
	}
	return res
}

func checkHost(conn icmp.Conn, host string, count int, timeout time.Duration) HostResult {
	hr := HostResult{Host: host, Sent: count}
	ip, err := resolveIPv4(host)
	if err != nil {
		hr.Err = err
		hr.Sent = 0
		return hr
	}
	hr.IP = ip

	var total time.Duration
	for i := 0; i < count; i++ {
		r, err := conn.Probe(ip, 64, 0, timeout, 56)
		if err != nil {
			hr.Err = err
			continue
		}
		if r.OK() {
			hr.Received++
			total += r.RTT
		}
	}
	if hr.Received > 0 {
		hr.AvgRTT = total / time.Duration(hr.Received)
	}
	return hr
}

// resolveIPv4 resolves host to its first IPv4 address.
func resolveIPv4(host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return nil, fmt.Errorf("%s is not an IPv4 address", host)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
	}
	return nil, fmt.Errorf("no IPv4 address for %s", host)
}
