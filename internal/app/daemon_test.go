package app

import (
	"testing"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/netinfo"
)

func TestGatewayTarget(t *testing.T) {
	cfg := config.Default()
	cfg.Targets = []config.Target{{Name: "cf", Host: "1.1.1.1"}}

	// Gateway detected and not already configured -> added.
	ni := &netinfo.NetInfo{DefaultGateway: "192.168.1.1"}
	gw, ok := gatewayTarget(&cfg, ni)
	if !ok {
		t.Fatal("expected gateway target to be added")
	}
	if gw.Host != "192.168.1.1" || gw.Name != "Default gateway" {
		t.Errorf("gateway target = %+v", gw)
	}
	// Inherits the default ping interval.
	if gw.Ping.Interval != cfg.Defaults.Ping.Interval {
		t.Errorf("gateway ping interval = %v, want default %v", gw.Ping.Interval, cfg.Defaults.Ping.Interval)
	}

	// No gateway detected -> not added.
	if _, ok := gatewayTarget(&cfg, &netinfo.NetInfo{}); ok {
		t.Error("expected no gateway target when none detected")
	}

	// Gateway already an explicit target -> not added (no duplicate).
	cfg.Targets = append(cfg.Targets, config.Target{Name: "gw", Host: "192.168.1.1"})
	if _, ok := gatewayTarget(&cfg, ni); ok {
		t.Error("expected no gateway target when already configured")
	}
}
