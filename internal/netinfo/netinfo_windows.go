//go:build windows

package netinfo

import (
	"encoding/binary"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modiphlpapi                  = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIpForwardTable        = modiphlpapi.NewProc("GetIpForwardTable")
	procGetIpNetTable            = modiphlpapi.NewProc("GetIpNetTable")
	ifTypeEthernet        uint32 = 6
	ifTypeWiFi            uint32 = 71
)

// platformDefaultRoute returns the lowest-metric default route.
func platformDefaultRoute() (net.IP, string, error) {
	routes, err := readForwardTable()
	if err != nil {
		return nil, "", err
	}
	var best *Route
	for i := range routes {
		if routes[i].Destination != "default" {
			continue
		}
		if best == nil || routes[i].Metric < best.Metric {
			best = &routes[i]
		}
	}
	if best == nil {
		return nil, "", errNoGateway
	}
	gw := net.ParseIP(best.Gateway)
	if gw == nil {
		return nil, best.Iface, fmt.Errorf("invalid gateway %q", best.Gateway)
	}
	return gw, best.Iface, nil
}

func platformRoutingTable() ([]Route, error) { return readForwardTable() }

// readForwardTable calls GetIpForwardTable and decodes MIB_IPFORWARDROW entries.
func readForwardTable() ([]Route, error) {
	buf, err := callSizedTable(procGetIpForwardTable)
	if err != nil {
		return nil, err
	}
	const rowSize = 56
	n := binary.LittleEndian.Uint32(buf[0:4])
	rows := buf[4:]
	out := make([]Route, 0, n)
	for i := 0; i < int(n); i++ {
		off := i * rowSize
		if off+rowSize > len(rows) {
			break
		}
		row := rows[off : off+rowSize]
		dest := net.IPv4(row[0], row[1], row[2], row[3])
		mask := net.IPv4(row[4], row[5], row[6], row[7])
		nextHop := net.IPv4(row[12], row[13], row[14], row[15])
		ifIndex := binary.LittleEndian.Uint32(row[16:20])
		metric := int(binary.LittleEndian.Uint32(row[36:40]))

		r := Route{Iface: ifaceName(ifIndex), Metric: metric}
		if dest.Equal(net.IPv4zero) && mask.Equal(net.IPv4zero) {
			r.Destination = "default"
		} else {
			ones, _ := net.IPMask(mask.To4()).Size()
			r.Destination = fmt.Sprintf("%s/%d", dest.String(), ones)
		}
		if !nextHop.Equal(net.IPv4zero) {
			r.Gateway = nextHop.String()
		}
		out = append(out, r)
	}
	return out, nil
}

// platformARPTable calls GetIpNetTable and decodes MIB_IPNETROW entries.
func platformARPTable() ([]ARPEntry, error) {
	buf, err := callSizedTable(procGetIpNetTable)
	if err != nil {
		return nil, err
	}
	const rowSize = 24
	n := binary.LittleEndian.Uint32(buf[0:4])
	rows := buf[4:]
	out := make([]ARPEntry, 0, n)
	for i := 0; i < int(n); i++ {
		off := i * rowSize
		if off+rowSize > len(rows) {
			break
		}
		row := rows[off : off+rowSize]
		ifIndex := binary.LittleEndian.Uint32(row[0:4])
		physLen := binary.LittleEndian.Uint32(row[4:8])
		if physLen == 0 || physLen > 8 {
			continue
		}
		mac := net.HardwareAddr(row[8 : 8+physLen])
		addr := net.IPv4(row[16], row[17], row[18], row[19])
		entryType := binary.LittleEndian.Uint32(row[20:24])
		if entryType == 2 { // MIB_IPNET_TYPE_INVALID
			continue
		}
		out = append(out, ARPEntry{IP: addr.String(), MAC: mac.String(), Iface: ifaceName(ifIndex)})
	}
	return out, nil
}

// callSizedTable performs the standard two-call (size, then fill) sequence.
func callSizedTable(proc *windows.LazyProc) ([]byte, error) {
	var size uint32
	proc.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return nil, fmt.Errorf("%s returned size 0", proc.Name)
	}
	buf := make([]byte, size)
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 1)
	if r != 0 {
		return nil, fmt.Errorf("%s failed: %d", proc.Name, r)
	}
	return buf, nil
}

// platformLinkType maps the interface to wired/wifi via GetAdaptersAddresses.
func platformLinkType(iface string) (LinkType, error) {
	types, err := adapterLinkTypes()
	if err != nil {
		return LinkUnknown, err
	}
	if lt, ok := types[iface]; ok {
		return lt, nil
	}
	return LinkUnknown, fmt.Errorf("interface %s not found among adapters", iface)
}

func adapterLinkTypes() (map[string]LinkType, error) {
	size := uint32(15000)
	var aa *windows.IpAdapterAddresses
	for i := 0; i < 4; i++ {
		buf := make([]byte, size)
		aa = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, 0, 0, aa, &size)
		if err == nil {
			break
		}
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		return nil, err
	}
	m := map[string]LinkType{}
	for p := aa; p != nil; p = p.Next {
		name := windows.UTF16PtrToString(p.FriendlyName)
		m[name] = ifTypeToLink(p.IfType)
	}
	return m, nil
}

func ifTypeToLink(t uint32) LinkType {
	switch t {
	case ifTypeWiFi:
		return LinkWiFi
	case ifTypeEthernet:
		return LinkWired
	default:
		return LinkUnknown
	}
}

// ifaceName resolves a Windows interface index to Go's interface name.
func ifaceName(index uint32) string {
	if ifc, err := net.InterfaceByIndex(int(index)); err == nil {
		return ifc.Name
	}
	return fmt.Sprintf("if%d", index)
}
