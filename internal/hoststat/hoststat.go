// Package hoststat continuously collects host-level metrics: per-interface counters,
// WiFi signal quality, and CPU/memory pressure. Every metric soft-fails independently.
// Platform specifics live in hoststat_linux.go, hoststat_windows.go and hoststat_other.go.
package hoststat

import "time"

// Iface holds counters for one network interface.
type Iface struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxErrors  uint64 `json:"rx_errors"`
	TxErrors  uint64 `json:"tx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxDropped uint64 `json:"tx_dropped"`
	SpeedBps  uint64 `json:"speed_bps"`

	// Current throughput (bytes/second), derived from consecutive counter samples.
	RxBps uint64 `json:"rx_bps"`
	TxBps uint64 `json:"tx_bps"`

	// Link (physical carrier / media-connect) state, tracked across collections.
	LinkUp         bool      `json:"link_up"`
	LinkChanges    uint64    `json:"link_changes"`     // up<->down transitions this session
	LinkLastChange time.Time `json:"link_last_change"` // zero until the first transition
}

// WiFi holds wireless signal metrics for the egress interface. SSID/BSSID are collected
// for the local UI only and must never be exported as metric labels.
type WiFi struct {
	Iface       string `json:"iface"`
	SignalDBm   int    `json:"signal_dbm"`
	LinkRateBps uint64 `json:"link_rate_bps"`
	Channel     int    `json:"channel"`
	SSID        string `json:"ssid,omitempty"`
}

// Status records a soft-failed field.
type Status struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Stats is a host-stats snapshot.
type Stats struct {
	Interfaces  []Iface           `json:"interfaces,omitempty"`
	WiFi        *WiFi             `json:"wifi,omitempty"`
	CPULoad     float64           `json:"cpu_load"`
	MemUsed     float64           `json:"mem_used"`
	Statuses    map[string]Status `json:"statuses"`
	CollectedAt time.Time         `json:"collected_at"`
}

// Field keys used in Statuses.
const (
	FieldInterfaces = "interfaces"
	FieldWiFi       = "wifi"
	FieldCPU        = "cpu"
	FieldMem        = "mem"
)

// Config selects which groups to collect.
type Config struct {
	InterfaceCounters bool
	WiFi              bool
	ResourcePressure  bool
}

// Collect gathers a snapshot for the given egress interface, soft-failing each group.
func Collect(cfg Config, egressIface string) *Stats {
	s := &Stats{Statuses: map[string]Status{}, CollectedAt: time.Now()}

	if cfg.InterfaceCounters {
		if ifs, err := platformInterfaceCounters(); err != nil {
			s.Statuses[FieldInterfaces] = Status{Reason: err.Error()}
		} else {
			for i := range ifs {
				ifs[i].LinkLastChange, ifs[i].LinkChanges = trackLink(ifs[i].Name, ifs[i].LinkUp, s.CollectedAt)
				ifs[i].RxBps, ifs[i].TxBps = trackThroughput(ifs[i].Name, ifs[i].RxBytes, ifs[i].TxBytes, s.CollectedAt)
			}
			s.Interfaces = ifs
			s.Statuses[FieldInterfaces] = Status{OK: true}
		}
	}
	if cfg.WiFi {
		if w, err := platformWiFi(egressIface); err != nil {
			s.Statuses[FieldWiFi] = Status{Reason: err.Error()}
		} else {
			s.WiFi = w
			s.Statuses[FieldWiFi] = Status{OK: true}
		}
	}
	if cfg.ResourcePressure {
		if cpu, mem, err := platformResourcePressure(); err != nil {
			s.Statuses[FieldCPU] = Status{Reason: err.Error()}
			s.Statuses[FieldMem] = Status{Reason: err.Error()}
		} else {
			s.CPULoad = cpu
			s.MemUsed = mem
			s.Statuses[FieldCPU] = Status{OK: true}
			s.Statuses[FieldMem] = Status{OK: true}
		}
	}
	return s
}
