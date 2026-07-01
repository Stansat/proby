//go:build linux

package netinfo

import "testing"

func TestHexToIP(t *testing.T) {
	// /proc/net/route stores IPs little-endian hex. 0100A8C0 => 192.168.0.1.
	if got := hexToIP("0100A8C0").String(); got != "192.168.0.1" {
		t.Errorf("hexToIP = %q, want 192.168.0.1", got)
	}
	// 00000000 => 0.0.0.0 (default route destination).
	if got := hexToIP("00000000").String(); got != "0.0.0.0" {
		t.Errorf("hexToIP zero = %q", got)
	}
}
