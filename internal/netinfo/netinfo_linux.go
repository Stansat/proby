//go:build linux

package netinfo

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// platformDefaultRoute reads the default route from /proc/net/route.
func platformDefaultRoute() (net.IP, string, error) {
	routes, err := parseProcRoute()
	if err != nil {
		return nil, "", err
	}
	for _, r := range routes {
		if r.Destination == "default" {
			gw := net.ParseIP(r.Gateway)
			if gw == nil {
				return nil, r.Iface, fmt.Errorf("invalid gateway %q", r.Gateway)
			}
			return gw, r.Iface, nil
		}
	}
	return nil, "", errNoGateway
}

// platformRoutingTable returns the full IPv4 routing table.
func platformRoutingTable() ([]Route, error) {
	return parseProcRoute()
}

// parseProcRoute parses /proc/net/route (hex, little-endian fields).
func parseProcRoute() ([]Route, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var routes []Route
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first { // header
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 11 {
			continue
		}
		iface := fields[0]
		dest := hexToIP(fields[1])
		gw := hexToIP(fields[2])
		mask := hexToIP(fields[7])
		metric, _ := strconv.Atoi(fields[6])

		r := Route{Iface: iface, Metric: metric}
		if dest.Equal(net.IPv4zero) && mask.Equal(net.IPv4zero) {
			r.Destination = "default"
		} else {
			ones, _ := net.IPMask(mask.To4()).Size()
			r.Destination = fmt.Sprintf("%s/%d", dest.String(), ones)
		}
		if !gw.Equal(net.IPv4zero) {
			r.Gateway = gw.String()
		}
		routes = append(routes, r)
	}
	return routes, sc.Err()
}

// hexToIP decodes a little-endian hex IPv4 as used in /proc/net/route.
func hexToIP(hex string) net.IP {
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return net.IPv4zero
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	return net.IPv4(b[0], b[1], b[2], b[3])
}

// platformARPTable parses /proc/net/arp.
func platformARPTable() ([]ARPEntry, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []ARPEntry
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		// IP address, HW type, Flags, HW address, Mask, Device
		fields := strings.Fields(sc.Text())
		if len(fields) < 6 {
			continue
		}
		mac := fields[3]
		if mac == "00:00:00:00:00:00" {
			continue
		}
		entries = append(entries, ARPEntry{IP: fields[0], MAC: mac, Iface: fields[5]})
	}
	return entries, sc.Err()
}

// platformLinkType checks for a wireless directory under sysfs.
func platformLinkType(iface string) (LinkType, error) {
	if _, err := os.Stat("/sys/class/net/" + iface + "/wireless"); err == nil {
		return LinkWiFi, nil
	}
	if _, err := os.Stat("/sys/class/net/" + iface + "/phy80211"); err == nil {
		return LinkWiFi, nil
	}
	if _, err := os.Stat("/sys/class/net/" + iface); err == nil {
		return LinkWired, nil
	}
	return LinkUnknown, fmt.Errorf("interface %s not found in sysfs", iface)
}
