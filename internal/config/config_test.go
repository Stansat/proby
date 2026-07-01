package config

import (
	"testing"
	"time"
)

func TestParseDefaultsAndMerge(t *testing.T) {
	data := []byte(`
instance: "test-01"
targets:
  - name: "A"
    host: "1.1.1.1"
  - name: "B"
    host: "8.8.8.8"
    ping: { interval: "2s", dscp: 46 }
    traceroute: { max_hops: 20 }
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Instance != "test-01" {
		t.Errorf("instance = %q", cfg.Instance)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets = %d", len(cfg.Targets))
	}

	// Target A inherits all ping defaults.
	a := cfg.Targets[0]
	if a.Ping.Interval.Std() != time.Second {
		t.Errorf("A ping interval = %v, want 1s", a.Ping.Interval.Std())
	}
	if a.Ping.PayloadSize != 56 {
		t.Errorf("A payload = %d, want 56", a.Ping.PayloadSize)
	}
	if a.Traceroute.MaxHops != 30 {
		t.Errorf("A max_hops = %d, want 30", a.Traceroute.MaxHops)
	}

	// Target B overrides only the specified keys.
	b := cfg.Targets[1]
	if b.Ping.Interval.Std() != 2*time.Second {
		t.Errorf("B ping interval = %v, want 2s", b.Ping.Interval.Std())
	}
	if b.Ping.DSCP != 46 {
		t.Errorf("B dscp = %d, want 46", b.Ping.DSCP)
	}
	if b.Ping.PayloadSize != 56 {
		t.Errorf("B payload = %d, want inherited 56", b.Ping.PayloadSize)
	}
	if b.Traceroute.MaxHops != 20 {
		t.Errorf("B max_hops = %d, want 20", b.Traceroute.MaxHops)
	}
	if b.Traceroute.Queries != 3 {
		t.Errorf("B queries = %d, want inherited 3", b.Traceroute.Queries)
	}
}

func TestValidateRejectsEmpty(t *testing.T) {
	_, err := Parse([]byte(`connectivity: { check_hosts: [] }`))
	if err == nil {
		t.Fatal("expected error for config with no targets or check hosts")
	}
}

func TestPushgatewayDualAuthRejected(t *testing.T) {
	data := []byte(`
targets: [{host: "1.1.1.1"}]
pushgateway:
  url: "https://x"
  auth:
    basic: { username: "u", password: "p" }
    cloudflare_access: { client_id: "id", client_secret: "s" }
`)
	if _, err := Parse(data); err == nil {
		t.Fatal("expected error for dual pushgateway auth")
	}
}

func TestByteSizeParsing(t *testing.T) {
	cases := map[string]int64{"64MB": 64 << 20, "512KB": 512 << 10, "1GB": 1 << 30, "1024": 1024}
	for in, want := range cases {
		got, err := parseByteSize(in)
		if err != nil {
			t.Errorf("parseByteSize(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseByteSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestResolveInstancePrecedence(t *testing.T) {
	cfg := &Config{Instance: "from-config"}
	if got, fb := ResolveInstance(cfg, "from-flag"); got != "from-flag" || fb {
		t.Errorf("flag precedence: got %q fallback=%v", got, fb)
	}
	if got, fb := ResolveInstance(cfg, ""); got != "from-config" || fb {
		t.Errorf("config precedence: got %q fallback=%v", got, fb)
	}
	if got, _ := ResolveInstance(&Config{Instance: "a b:c"}, ""); got != "a_b:c" {
		t.Errorf("sanitize: got %q, want a_b:c", got)
	}
}
