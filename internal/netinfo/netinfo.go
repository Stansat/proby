// Package netinfo performs the step-1 local network detection. Every field is
// collected independently and soft-fails: a failure on one field is recorded as an
// Unavailable status and never blocks the others. Platform specifics live in
// netinfo_linux.go, netinfo_windows.go and netinfo_other.go.
package netinfo

import (
	"net"
	"time"

	"github.com/stansat/proby/internal/icmp"
)

// LinkType is the physical medium of the egress interface.
type LinkType string

const (
	LinkWired   LinkType = "wired"
	LinkWiFi    LinkType = "wifi"
	LinkUnknown LinkType = "unknown"
)

// Status records whether a field was collected and, if not, why.
type Status struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Route is one routing-table entry.
type Route struct {
	Destination string `json:"destination"` // CIDR, or "default"
	Gateway     string `json:"gateway,omitempty"`
	Iface       string `json:"iface,omitempty"`
	Metric      int    `json:"metric"`
}

// ARPEntry is one ARP / neighbour-cache entry.
type ARPEntry struct {
	IP    string `json:"ip"`
	MAC   string `json:"mac"`
	Iface string `json:"iface,omitempty"`
}

// IfaceSummary is a brief description of a network interface.
type IfaceSummary struct {
	Name string `json:"name"`
	MAC  string `json:"mac,omitempty"`
	MTU  int    `json:"mtu"`
	Up   bool   `json:"up"`
}

// NetInfo is the collected step-1 snapshot.
type NetInfo struct {
	Hostname       string            `json:"hostname"`
	DefaultGateway string            `json:"default_gateway,omitempty"`
	EgressIP       string            `json:"egress_ip,omitempty"`
	EgressIface    string            `json:"egress_iface,omitempty"`
	LinkType       LinkType          `json:"link_type"`
	LocalMAC       string            `json:"local_mac,omitempty"`
	GatewayMAC     string            `json:"gateway_mac,omitempty"`
	RoutingTable   []Route           `json:"routing_table,omitempty"`
	ARPTable       []ARPEntry        `json:"arp_table,omitempty"`
	Interfaces     []IfaceSummary    `json:"interfaces,omitempty"`
	Statuses       map[string]Status `json:"statuses"`
	CollectedAt    time.Time         `json:"collected_at"`
}

// Field name keys used in Statuses.
const (
	FieldDefaultRoute = "default_route"
	FieldEgressIP     = "egress_ip"
	FieldLinkType     = "link_type"
	FieldLocalMAC     = "local_mac"
	FieldGatewayMAC   = "gateway_mac"
	FieldRoutingTable = "routing_table"
	FieldARPTable     = "arp_table"
	FieldInterfaces   = "interfaces"
)

// Options tune collection.
type Options struct {
	// Conn, if set, is used to ping the default gateway so its MAC appears in the
	// neighbour cache before the ARP table is read.
	Conn icmp.Conn
}

// Collect gathers the snapshot, soft-failing each field.
func Collect(opts Options) *NetInfo {
	ni := &NetInfo{
		LinkType:    LinkUnknown,
		Statuses:    map[string]Status{},
		CollectedAt: time.Now(),
	}
	ni.Hostname, _ = osHostname()

	// Default route (platform): gateway IP + egress interface name.
	gw, iface, err := platformDefaultRoute()
	if err != nil {
		ni.setFail(FieldDefaultRoute, err)
	} else {
		ni.DefaultGateway = gw.String()
		ni.EgressIface = iface
		ni.setOK(FieldDefaultRoute)
	}

	// Egress IP: the source address the OS would use to reach the gateway.
	if gw != nil {
		if ip, err := egressIP(gw); err != nil {
			ni.setFail(FieldEgressIP, err)
		} else {
			ni.EgressIP = ip.String()
			ni.setOK(FieldEgressIP)
			if ni.EgressIface == "" {
				ni.EgressIface = ifaceForIP(ip)
			}
		}
	} else {
		ni.setFail(FieldEgressIP, errNoGateway)
	}

	// Link type (platform), given the egress interface.
	if ni.EgressIface != "" {
		if lt, err := platformLinkType(ni.EgressIface); err != nil {
			ni.setFail(FieldLinkType, err)
		} else {
			ni.LinkType = lt
			ni.setOK(FieldLinkType)
		}
	} else {
		ni.setFail(FieldLinkType, errNoEgressIface)
	}

	// Local MAC of the egress interface.
	if ni.EgressIface != "" {
		if mac, err := ifaceMAC(ni.EgressIface); err != nil {
			ni.setFail(FieldLocalMAC, err)
		} else {
			ni.LocalMAC = mac
			ni.setOK(FieldLocalMAC)
		}
	} else {
		ni.setFail(FieldLocalMAC, errNoEgressIface)
	}

	// Interfaces (neutral).
	if ifs, err := interfaceSummaries(); err != nil {
		ni.setFail(FieldInterfaces, err)
	} else {
		ni.Interfaces = ifs
		ni.setOK(FieldInterfaces)
	}

	// Routing table (platform).
	if rt, err := platformRoutingTable(); err != nil {
		ni.setFail(FieldRoutingTable, err)
	} else {
		ni.RoutingTable = rt
		ni.setOK(FieldRoutingTable)
	}

	// Populate the neighbour cache for the gateway before reading ARP.
	if gw != nil && opts.Conn != nil {
		_, _ = opts.Conn.Probe(gw, 64, 0, time.Second, 56)
	}

	// ARP table (platform).
	if arp, err := platformARPTable(); err != nil {
		ni.setFail(FieldARPTable, err)
	} else {
		ni.ARPTable = arp
		ni.setOK(FieldARPTable)
	}

	// Gateway MAC: look up the gateway IP in the ARP table.
	if ni.DefaultGateway != "" {
		if mac := lookupMAC(ni.ARPTable, ni.DefaultGateway); mac != "" {
			ni.GatewayMAC = mac
			ni.setOK(FieldGatewayMAC)
		} else {
			ni.setFail(FieldGatewayMAC, errGatewayMACNotFound)
		}
	} else {
		ni.setFail(FieldGatewayMAC, errNoGateway)
	}

	return ni
}

func (ni *NetInfo) setOK(field string) { ni.Statuses[field] = Status{OK: true} }
func (ni *NetInfo) setFail(field string, e error) {
	ni.Statuses[field] = Status{OK: false, Reason: e.Error()}
}

func lookupMAC(arp []ARPEntry, ip string) string {
	for _, e := range arp {
		if e.IP == ip {
			return e.MAC
		}
	}
	return ""
}

// ifaceForIP returns the interface name owning ip, or "".
func ifaceForIP(ip net.IP) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
				return ifc.Name
			}
		}
	}
	return ""
}
