package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/store"
)

func testEnv() *netinfo.NetInfo {
	return &netinfo.NetInfo{
		Hostname:       "secret-host",
		EgressIP:       "10.9.8.7",
		EgressIface:    "eth0",
		LinkType:       netinfo.LinkWired,
		LocalMAC:       "de:ad:be:ef:00:01",
		DefaultGateway: "10.9.8.1",
		GatewayMAC:     "de:ad:be:ef:00:02",
		RoutingTable:   []netinfo.Route{{Destination: "default", Gateway: "10.9.8.1", Iface: "eth0"}},
		ARPTable:       []netinfo.ARPEntry{{IP: "10.9.8.1", MAC: "de:ad:be:ef:00:02", Iface: "eth0"}},
		Statuses:       map[string]netinfo.Status{"link_type": {OK: true}},
	}
}

func testStore() *store.Store {
	st := store.New(config.GamingThresholds{
		MaxRTT: config.Duration(60 * time.Millisecond), MaxJitter: config.Duration(10 * time.Millisecond), MaxLoss: 0.02,
	})
	st.Register("Cloudflare", "1.1.1.1", "1.1.1.1", 46)
	st.AddPing("Cloudflare", store.PingSample{T: time.Now(), RTT: 10 * time.Millisecond, OK: true})
	return st
}

func scrape(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestLocalMetricsExcludeEnvByDefault(t *testing.T) {
	cfg := &config.Config{}
	m := New("site-01", cfg, testStore(), testEnv, nil)
	out := scrape(t, m.Handler())

	// Base metrics present with instance label.
	if !strings.Contains(out, "proby_ping_rtt_seconds") {
		t.Error("missing proby_ping_rtt_seconds on local /metrics")
	}
	if !strings.Contains(out, `instance="site-01"`) {
		t.Error("instance label not applied")
	}
	// Sensitive env data MUST NOT appear on local /metrics by default.
	for _, forbidden := range []string{"proby_env_info", "secret-host", "de:ad:be:ef", "10.9.8.7"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("SECURITY: local /metrics leaked %q", forbidden)
		}
	}
}

func TestLocalMetricsIncludeEnvWhenOptedIn(t *testing.T) {
	cfg := &config.Config{Web: config.WebConfig{MetricsIncludeEnvironment: true}}
	m := New("site-01", cfg, testStore(), testEnv, nil)
	out := scrape(t, m.Handler())
	if !strings.Contains(out, "proby_env_info") || !strings.Contains(out, "secret-host") {
		t.Error("env metrics should appear on local /metrics when opted in")
	}
}

func TestPushIncludesEnvAndAuth(t *testing.T) {
	var gotAuth string
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{Pushgateway: &config.PushgatewayConfig{
		URL:                srv.URL,
		Job:                "proby",
		IncludeEnvironment: true,
		Auth:               config.PushAuth{Basic: &config.BasicAuth{Username: "u", Password: "p"}},
	}}
	m := New("site-01", cfg, testStore(), testEnv, nil)
	m.pushOnce(m.newPusher(cfg.Pushgateway))

	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected basic auth header, got %q", gotAuth)
	}
	if !strings.Contains(gotBody, "proby_env_info") {
		t.Error("push body should include env metrics")
	}
	if !strings.Contains(gotBody, "proby_ping_rtt_seconds") {
		t.Error("push body should include base metrics")
	}
	// Pushes use the protobuf format, so check the label name and value separately.
	if !strings.Contains(gotBody, "instance") || !strings.Contains(gotBody, "site-01") {
		t.Error("push body should carry the instance label")
	}
}

func TestCloudflareAuthHeaders(t *testing.T) {
	var id, secret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id = r.Header.Get("CF-Access-Client-Id")
		secret = r.Header.Get("CF-Access-Client-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{Pushgateway: &config.PushgatewayConfig{
		URL: srv.URL, Job: "proby",
		Auth: config.PushAuth{CloudflareAccess: &config.CloudflareAccess{ClientID: "cid", ClientSecret: "csec"}},
	}}
	m := New("s", cfg, testStore(), testEnv, nil)
	m.pushOnce(m.newPusher(cfg.Pushgateway))
	if id != "cid" || secret != "csec" {
		t.Errorf("CF headers = %q/%q, want cid/csec", id, secret)
	}
}
