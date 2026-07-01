//go:build !linux && !windows

package netinfo

import (
	"fmt"
	"net"
	"runtime"
)

// The non-Linux, non-Windows platforms (notably macOS) are stubbed until M8. Every
// function soft-fails with a clear reason so the rest of the snapshot still works.

func platformDefaultRoute() (net.IP, string, error) {
	return nil, "", fmt.Errorf("default route detection not implemented on %s", runtime.GOOS)
}

func platformRoutingTable() ([]Route, error) {
	return nil, fmt.Errorf("routing table not implemented on %s", runtime.GOOS)
}

func platformARPTable() ([]ARPEntry, error) {
	return nil, fmt.Errorf("arp table not implemented on %s", runtime.GOOS)
}

func platformLinkType(string) (LinkType, error) {
	return LinkUnknown, fmt.Errorf("link type not implemented on %s", runtime.GOOS)
}
