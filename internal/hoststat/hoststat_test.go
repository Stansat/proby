package hoststat

import "testing"

// TestCollectSoftFails ensures Collect never panics and always returns statuses,
// regardless of what the host platform supports.
func TestCollectSoftFails(t *testing.T) {
	s := Collect(Config{InterfaceCounters: true, WiFi: true, ResourcePressure: true}, "")
	if s == nil {
		t.Fatal("Collect returned nil")
	}
	if s.Statuses == nil {
		t.Fatal("Statuses nil")
	}
	if s.CollectedAt.IsZero() {
		t.Error("CollectedAt not set")
	}
	// Every requested field must have a status entry (ok or a reason).
	for _, f := range []string{FieldInterfaces, FieldWiFi, FieldCPU, FieldMem} {
		if _, ok := s.Statuses[f]; !ok {
			t.Errorf("missing status for %q", f)
		}
	}
}
