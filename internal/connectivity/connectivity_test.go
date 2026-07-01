package connectivity

import (
	"net"
	"testing"
	"time"

	"github.com/stansat/proby/internal/icmp"
)

// fakeConn returns canned results keyed by destination IP string.
type fakeConn struct {
	replies map[string]icmp.ReplyKind
}

func (f *fakeConn) Mode() string { return "fake" }
func (f *fakeConn) Close() error { return nil }
func (f *fakeConn) Probe(dst net.IP, ttl, dscp int, timeout time.Duration, payloadSize int) (icmp.ProbeResult, error) {
	kind, ok := f.replies[dst.String()]
	if !ok {
		kind = icmp.KindTimeout
	}
	return icmp.ProbeResult{From: dst, RTT: 5 * time.Millisecond, Kind: kind}, nil
}

func TestCheckPassesIfAnyHostReplies(t *testing.T) {
	conn := &fakeConn{replies: map[string]icmp.ReplyKind{
		"1.1.1.1": icmp.KindEchoReply,
		"8.8.8.8": icmp.KindTimeout,
	}}
	res := Check(conn, []string{"1.1.1.1", "8.8.8.8"}, 2, time.Second)
	if !res.Passed {
		t.Fatal("expected Passed=true when one host replies")
	}
	if len(res.Hosts) != 2 {
		t.Fatalf("hosts = %d", len(res.Hosts))
	}
	if !res.Hosts[0].OK() || res.Hosts[0].Received != 2 {
		t.Errorf("host0 = %+v", res.Hosts[0])
	}
	if res.Hosts[1].OK() {
		t.Errorf("host1 should have failed: %+v", res.Hosts[1])
	}
}

func TestCheckFailsIfAllHostsFail(t *testing.T) {
	conn := &fakeConn{replies: map[string]icmp.ReplyKind{}}
	res := Check(conn, []string{"192.0.2.1", "192.0.2.2"}, 1, time.Second)
	if res.Passed {
		t.Fatal("expected Passed=false when all hosts fail")
	}
}
