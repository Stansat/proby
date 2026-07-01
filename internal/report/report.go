// Package report renders a human-readable network diagnostic report from the step-1
// snapshot and step-2 connectivity results, suitable for copy-pasting to a network
// operator.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/stansat/proby/internal/connectivity"
	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/netinfo"
)

// Render builds the full text report. conn may be nil if connectivity was not run.
func Render(ni *netinfo.NetInfo, conn *connectivity.Result, tr *i18n.Translator, ver string) string {
	var b strings.Builder
	line := strings.Repeat("=", 66)

	fmt.Fprintln(&b, line)
	fmt.Fprintf(&b, "  %s\n", tr.T(i18n.MsgDiagHeader))
	fmt.Fprintln(&b, line)
	fmt.Fprintf(&b, "  proby version : %s\n", ver)
	fmt.Fprintf(&b, "  generated     : %s\n", ni.CollectedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "  hostname      : %s\n", orNA(ni.Hostname))
	b.WriteString("\n")

	section(&b, "LOCAL NETWORK")
	kv(&b, ni, "default gateway", ni.DefaultGateway, netinfo.FieldDefaultRoute)
	kv(&b, ni, "egress IP", ni.EgressIP, netinfo.FieldEgressIP)
	kv(&b, ni, "egress interface", ni.EgressIface, netinfo.FieldDefaultRoute)
	kv(&b, ni, "link type", string(ni.LinkType), netinfo.FieldLinkType)
	kv(&b, ni, "local MAC", ni.LocalMAC, netinfo.FieldLocalMAC)
	kv(&b, ni, "gateway MAC", ni.GatewayMAC, netinfo.FieldGatewayMAC)
	b.WriteString("\n")

	if conn != nil {
		section(&b, "CONNECTIVITY CHECK")
		for _, h := range conn.Hosts {
			if h.OK() {
				fmt.Fprintf(&b, "  OK    %-20s %d/%d replies, avg %s\n",
					h.Host, h.Received, h.Sent, h.AvgRTT.Round(10*time.Microsecond))
			} else {
				reason := "no reply"
				if h.Err != nil {
					reason = h.Err.Error()
				}
				fmt.Fprintf(&b, "  FAIL  %-20s %s\n", h.Host, reason)
			}
		}
		b.WriteString("\n")
	}

	section(&b, "ROUTING TABLE")
	if len(ni.RoutingTable) == 0 {
		fmt.Fprintf(&b, "  %s\n", statusNote(ni, netinfo.FieldRoutingTable, tr))
	} else {
		fmt.Fprintf(&b, "  %-20s %-16s %-14s %s\n", "destination", "gateway", "interface", "metric")
		for _, r := range ni.RoutingTable {
			fmt.Fprintf(&b, "  %-20s %-16s %-14s %d\n", r.Destination, orDash(r.Gateway), orDash(r.Iface), r.Metric)
		}
	}
	b.WriteString("\n")

	section(&b, "ARP / NEIGHBOUR TABLE")
	if len(ni.ARPTable) == 0 {
		fmt.Fprintf(&b, "  %s\n", statusNote(ni, netinfo.FieldARPTable, tr))
	} else {
		fmt.Fprintf(&b, "  %-18s %-20s %s\n", "ip", "mac", "interface")
		for _, e := range ni.ARPTable {
			fmt.Fprintf(&b, "  %-18s %-20s %s\n", e.IP, e.MAC, e.Iface)
		}
	}
	b.WriteString("\n")

	// Unavailable items with reasons.
	var unavailable []string
	for field, st := range ni.Statuses {
		if !st.OK {
			unavailable = append(unavailable, fmt.Sprintf("  %-16s %s", field, st.Reason))
		}
	}
	if len(unavailable) > 0 {
		sort.Strings(unavailable)
		section(&b, "UNAVAILABLE ITEMS")
		for _, u := range unavailable {
			fmt.Fprintln(&b, u)
		}
		b.WriteString("\n")
	}

	fmt.Fprintln(&b, line)
	return b.String()
}

func section(b *strings.Builder, title string) {
	fmt.Fprintf(b, "-- %s %s\n", title, strings.Repeat("-", 62-len(title)))
}

func kv(b *strings.Builder, ni *netinfo.NetInfo, label, value, field string) {
	if value == "" {
		if st, ok := ni.Statuses[field]; ok && !st.OK {
			fmt.Fprintf(b, "  %-18s (unavailable: %s)\n", label, st.Reason)
			return
		}
		value = "n/a"
	}
	fmt.Fprintf(b, "  %-18s %s\n", label, value)
}

func statusNote(ni *netinfo.NetInfo, field string, tr *i18n.Translator) string {
	if st, ok := ni.Statuses[field]; ok && !st.OK {
		return tr.T(i18n.MsgReportUnavailable, st.Reason)
	}
	return "(empty)"
}

func orNA(s string) string {
	if s == "" {
		return "n/a"
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
