package netinfo

import (
	"errors"
	"fmt"
	"net"
	"os"
)

var (
	errNoGateway          = errors.New("no default gateway detected")
	errNoEgressIface      = errors.New("egress interface unknown")
	errGatewayMACNotFound = errors.New("gateway not present in ARP/neighbour cache")
)

func osHostname() (string, error) { return os.Hostname() }

// egressIP returns the local source address the OS would use to reach gw. It opens a
// UDP socket but sends no packets.
func egressIP(gw net.IP) (net.IP, error) {
	conn, err := net.Dial("udp", net.JoinHostPort(gw.String(), "9"))
	if err != nil {
		return nil, fmt.Errorf("determine egress ip: %w", err)
	}
	defer conn.Close()
	la, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || la.IP == nil {
		return nil, errors.New("could not determine egress ip")
	}
	return la.IP, nil
}

// ifaceMAC returns the hardware address of the named interface.
func ifaceMAC(name string) (string, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("interface %s: %w", name, err)
	}
	if len(ifc.HardwareAddr) == 0 {
		return "", fmt.Errorf("interface %s has no hardware address", name)
	}
	return ifc.HardwareAddr.String(), nil
}

// interfaceSummaries lists all interfaces with basic attributes.
func interfaceSummaries() ([]IfaceSummary, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]IfaceSummary, 0, len(ifaces))
	for _, ifc := range ifaces {
		out = append(out, IfaceSummary{
			Name: ifc.Name,
			MAC:  ifc.HardwareAddr.String(),
			MTU:  ifc.MTU,
			Up:   ifc.Flags&net.FlagUp != 0,
		})
	}
	return out, nil
}
