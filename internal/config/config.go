// Package config loads, validates and normalises the proby.yml configuration.
package config

import (
	"gopkg.in/yaml.v3"
)

// Config is the top-level proby configuration.
type Config struct {
	Instance     string             `yaml:"instance"`
	Language     string             `yaml:"language"`
	ReportURL    string             `yaml:"report_url"`
	Web          WebConfig          `yaml:"web"`
	Connectivity ConnectivityConfig `yaml:"connectivity"`
	Pushgateway  *PushgatewayConfig `yaml:"pushgateway"`
	History      HistoryConfig      `yaml:"history"`
	HostStats    HostStatsConfig    `yaml:"host_stats"`
	Defaults     Defaults           `yaml:"defaults"`
	Targets      []Target           `yaml:"targets"`
}

// WebConfig controls the embedded web UI and local /metrics endpoint.
type WebConfig struct {
	Enabled                   bool   `yaml:"enabled"`
	Listen                    string `yaml:"listen"`
	WebSocket                 bool   `yaml:"websocket"`
	MetricsIncludeEnvironment bool   `yaml:"metrics_include_environment"`
}

// ConnectivityConfig drives the step-2 connectivity gate.
type ConnectivityConfig struct {
	CheckHosts []string `yaml:"check_hosts"`
	Count      int      `yaml:"count"`
	Timeout    Duration `yaml:"timeout"`
}

// PushgatewayConfig configures the primary metrics push path.
type PushgatewayConfig struct {
	URL                   string   `yaml:"url"`
	Job                   string   `yaml:"job"`
	Interval              Duration `yaml:"interval"`
	IncludeEnvironment    bool     `yaml:"include_environment"`
	Timeout               Duration `yaml:"timeout"`
	TLSInsecureSkipVerify bool     `yaml:"tls_insecure_skip_verify"`
	Auth                  PushAuth `yaml:"auth"`
}

// PushAuth selects at most one authentication scheme for the pushgateway.
type PushAuth struct {
	Basic            *BasicAuth        `yaml:"basic"`
	CloudflareAccess *CloudflareAccess `yaml:"cloudflare_access"`
}

// BasicAuth is HTTP basic authentication.
type BasicAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// CloudflareAccess is a Cloudflare Access service token.
type CloudflareAccess struct {
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// Enabled reports whether either auth scheme carries credentials.
func (a PushAuth) Enabled() bool {
	if a.Basic != nil && (a.Basic.Username != "" || a.Basic.Password != "") {
		return true
	}
	if a.CloudflareAccess != nil && (a.CloudflareAccess.ClientID != "" || a.CloudflareAccess.ClientSecret != "") {
		return true
	}
	return false
}

// HistoryConfig configures the on-disk append-only ring file.
type HistoryConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Path          string   `yaml:"path"`
	MaxSize       ByteSize `yaml:"max_size"`
	FlushInterval Duration `yaml:"flush_interval"`
}

// HostStatsConfig configures the continuous host-metrics collector.
type HostStatsConfig struct {
	Enabled           bool     `yaml:"enabled"`
	Interval          Duration `yaml:"interval"`
	InterfaceCounters bool     `yaml:"interface_counters"`
	WiFi              bool     `yaml:"wifi"`
	ResourcePressure  bool     `yaml:"resource_pressure"`
}

// Defaults holds the per-probe defaults applied to every target.
type Defaults struct {
	Ping       ProbeConfig   `yaml:"ping"`
	Traceroute ProbeConfig   `yaml:"traceroute"`
	Quality    QualityConfig `yaml:"quality"`
}

// ProbeConfig is shared by ping and traceroute; each uses the relevant subset.
type ProbeConfig struct {
	Interval    Duration `yaml:"interval"`
	Timeout     Duration `yaml:"timeout"`
	PayloadSize int      `yaml:"payload_size"`
	MaxHops     int      `yaml:"max_hops"`
	Queries     int      `yaml:"queries"`
	Protocol    string   `yaml:"protocol"`
	DSCP        int      `yaml:"dscp"`
}

// QualityConfig holds the thresholds that drive the "gaming ready" verdict.
type QualityConfig struct {
	GamingReady GamingThresholds `yaml:"gaming_ready"`
}

// GamingThresholds are the maximum latency/jitter/loss for a "gaming ready" verdict.
type GamingThresholds struct {
	MaxRTT    Duration `yaml:"max_rtt"`
	MaxJitter Duration `yaml:"max_jitter"`
	MaxLoss   float64  `yaml:"max_loss"`
}

// Target is a monitored host. Ping/Traceroute are resolved from raw YAML nodes so
// per-target keys override the defaults field-by-field.
type Target struct {
	Name      string    `yaml:"name"`
	Host      string    `yaml:"host"`
	PingNode  yaml.Node `yaml:"ping"`
	TraceNode yaml.Node `yaml:"traceroute"`

	// Resolved after merge; not read from YAML directly.
	Ping       ProbeConfig `yaml:"-"`
	Traceroute ProbeConfig `yaml:"-"`
}
