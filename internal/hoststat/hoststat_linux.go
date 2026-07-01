//go:build linux

package hoststat

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const sysNet = "/sys/class/net/"

// platformInterfaceCounters reads /sys/class/net/<if>/statistics.
func platformInterfaceCounters() ([]Iface, error) {
	entries, err := os.ReadDir(sysNet)
	if err != nil {
		return nil, err
	}
	var out []Iface
	for _, e := range entries {
		name := e.Name()
		if name == "lo" {
			continue
		}
		base := sysNet + name + "/statistics/"
		ic := Iface{
			Name:      name,
			RxBytes:   readUint(base + "rx_bytes"),
			TxBytes:   readUint(base + "tx_bytes"),
			RxErrors:  readUint(base + "rx_errors"),
			TxErrors:  readUint(base + "tx_errors"),
			RxDropped: readUint(base + "rx_dropped"),
			TxDropped: readUint(base + "tx_dropped"),
		}
		// speed is in Mbit/s; may be -1 or absent for virtual/down interfaces.
		if mbit := readInt(sysNet + name + "/speed"); mbit > 0 {
			ic.SpeedBps = uint64(mbit) * 1_000_000
		}
		ic.LinkUp = linkUp(name)
		out = append(out, ic)
	}
	return out, nil
}

// platformWiFi parses /proc/net/wireless for the egress interface's signal level.
func platformWiFi(egress string) (*WiFi, error) {
	f, err := os.Open("/proc/net/wireless")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		fields := strings.Fields(line)
		iface := strings.TrimSuffix(fields[0], ":")
		if egress != "" && iface != egress {
			continue
		}
		if len(fields) < 4 {
			continue
		}
		// Column 3 (index 3) is the signal level in dBm (may have a trailing '.').
		level := strings.TrimSuffix(fields[3], ".")
		dbm, err := strconv.Atoi(level)
		if err != nil {
			continue
		}
		return &WiFi{Iface: iface, SignalDBm: dbm}, nil
	}
	return nil, fmt.Errorf("no wireless interface found (egress %q not wireless)", egress)
}

var (
	cpuMu       sync.Mutex
	prevIdle    uint64
	prevTotal   uint64
	prevHasData bool
)

// platformResourcePressure computes CPU load (delta over calls) and memory usage.
func platformResourcePressure() (cpu, mem float64, err error) {
	cpu, err = cpuLoad()
	if err != nil {
		return 0, 0, err
	}
	mem, err = memUsed()
	if err != nil {
		return cpu, 0, err
	}
	return cpu, mem, nil
}

func cpuLoad() (float64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, fmt.Errorf("empty /proc/stat")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, fmt.Errorf("unexpected /proc/stat")
	}
	var total, idle uint64
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 { // idle
			idle = v
		}
	}
	cpuMu.Lock()
	defer cpuMu.Unlock()
	if !prevHasData {
		prevIdle, prevTotal, prevHasData = idle, total, true
		return 0, nil
	}
	dTotal := total - prevTotal
	dIdle := idle - prevIdle
	prevIdle, prevTotal = idle, total
	if dTotal == 0 {
		return 0, nil
	}
	return 1 - float64(dIdle)/float64(dTotal), nil
}

func memUsed() (float64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var total, avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			avail, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("MemTotal not found")
	}
	return 1 - float64(avail)/float64(total), nil
}

// linkUp reports the physical link state. It prefers carrier (1 = cable/link present),
// falling back to operstate when carrier is unreadable (e.g. an admin-down interface).
func linkUp(name string) bool {
	switch readInt(sysNet + name + "/carrier") {
	case 1:
		return true
	case 0:
		return false
	default:
		b, err := os.ReadFile(sysNet + name + "/operstate")
		return err == nil && strings.TrimSpace(string(b)) == "up"
	}
}

func readUint(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	return v
}

func readInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return -1
	}
	return v
}
