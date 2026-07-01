package report

import (
	"strings"
	"testing"
	"time"

	"github.com/stansat/proby/internal/connectivity"
	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/netinfo"
)

func TestRenderContainsKeyData(t *testing.T) {
	ni := &netinfo.NetInfo{
		Hostname:       "probe1",
		DefaultGateway: "192.168.1.1",
		EgressIP:       "192.168.1.50",
		EgressIface:    "eth0",
		LinkType:       netinfo.LinkWired,
		LocalMAC:       "aa:bb:cc:dd:ee:ff",
		GatewayMAC:     "11:22:33:44:55:66",
		RoutingTable:   []netinfo.Route{{Destination: "default", Gateway: "192.168.1.1", Iface: "eth0", Metric: 100}},
		ARPTable:       []netinfo.ARPEntry{{IP: "192.168.1.1", MAC: "11:22:33:44:55:66", Iface: "eth0"}},
		CollectedAt:    time.Now(),
		Statuses: map[string]netinfo.Status{
			netinfo.FieldDefaultRoute: {OK: true},
			netinfo.FieldGatewayMAC:   {OK: false, Reason: "cache miss"},
		},
	}
	conn := &connectivity.Result{
		Passed: false,
		Hosts: []connectivity.HostResult{
			{Host: "1.1.1.1", Sent: 3, Received: 0},
		},
	}
	out := Render(ni, conn, i18n.New(i18n.EN), "test-ver")

	for _, want := range []string{
		"192.168.1.1", "192.168.1.50", "aa:bb:cc:dd:ee:ff", "eth0",
		"CONNECTIVITY CHECK", "FAIL", "ROUTING TABLE", "ARP", "UNAVAILABLE ITEMS", "cache miss",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderHandlesMissingFields(t *testing.T) {
	ni := &netinfo.NetInfo{
		LinkType:    netinfo.LinkUnknown,
		CollectedAt: time.Now(),
		Statuses: map[string]netinfo.Status{
			netinfo.FieldDefaultRoute: {OK: false, Reason: "no default gateway"},
		},
	}
	out := Render(ni, nil, i18n.New(i18n.PL), "v")
	if !strings.Contains(out, "no default gateway") {
		t.Errorf("expected unavailable reason in report:\n%s", out)
	}
}
