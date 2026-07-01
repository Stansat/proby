//go:build !linux && !windows

package hoststat

import (
	"fmt"
	"runtime"
)

// macOS and other platforms are stubbed until M8; every group soft-fails.

func platformInterfaceCounters() ([]Iface, error) {
	return nil, fmt.Errorf("interface counters not implemented on %s", runtime.GOOS)
}

func platformWiFi(string) (*WiFi, error) {
	return nil, fmt.Errorf("wifi metrics not implemented on %s", runtime.GOOS)
}

func platformResourcePressure() (float64, float64, error) {
	return 0, 0, fmt.Errorf("resource pressure not implemented on %s", runtime.GOOS)
}
