package netinfo

import "testing"

func TestLookupMAC(t *testing.T) {
	arp := []ARPEntry{
		{IP: "192.168.1.1", MAC: "aa:bb:cc:dd:ee:ff"},
		{IP: "192.168.1.2", MAC: "11:22:33:44:55:66"},
	}
	if got := lookupMAC(arp, "192.168.1.2"); got != "11:22:33:44:55:66" {
		t.Errorf("lookupMAC = %q", got)
	}
	if got := lookupMAC(arp, "10.0.0.1"); got != "" {
		t.Errorf("lookupMAC(miss) = %q, want empty", got)
	}
}

func TestCollectSoftFails(t *testing.T) {
	// Collect must never panic and must always return a snapshot with statuses,
	// even if some platform calls fail in the test environment.
	ni := Collect(Options{})
	if ni == nil {
		t.Fatal("Collect returned nil")
	}
	if ni.Statuses == nil {
		t.Fatal("Statuses is nil")
	}
	if ni.CollectedAt.IsZero() {
		t.Error("CollectedAt not set")
	}
}
