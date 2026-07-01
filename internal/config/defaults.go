package config

import "time"

// Default returns a Config pre-filled with the documented defaults. YAML is then
// unmarshalled on top, so any key omitted by the user keeps its default here.
func Default() Config {
	return Config{
		Language: "auto",
		Web: WebConfig{
			Enabled:                   true,
			Listen:                    "0.0.0.0:8080",
			WebSocket:                 true,
			MetricsIncludeEnvironment: false,
		},
		Connectivity: ConnectivityConfig{
			CheckHosts: []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"},
			Count:      3,
			Timeout:    Duration(2 * time.Second),
		},
		History: HistoryConfig{
			Enabled:       true,
			Path:          "proby.history",
			MaxSize:       ByteSize(64 << 20),
			FlushInterval: Duration(5 * time.Second),
		},
		HostStats: HostStatsConfig{
			Enabled:           true,
			Interval:          Duration(5 * time.Second),
			InterfaceCounters: true,
			WiFi:              true,
			ResourcePressure:  true,
		},
		Defaults: Defaults{
			Ping: ProbeConfig{
				Interval:    Duration(1 * time.Second),
				Timeout:     Duration(2 * time.Second),
				PayloadSize: 56,
				DSCP:        0,
			},
			Traceroute: ProbeConfig{
				Interval: Duration(10 * time.Second),
				Timeout:  Duration(2 * time.Second),
				MaxHops:  30,
				Queries:  3,
				Protocol: "icmp",
				DSCP:     0,
			},
			Quality: QualityConfig{
				GamingReady: GamingThresholds{
					MaxRTT:    Duration(60 * time.Millisecond),
					MaxJitter: Duration(10 * time.Millisecond),
					MaxLoss:   0.02,
				},
			},
		},
	}
}
