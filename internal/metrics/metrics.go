// Package metrics exposes proby's Prometheus metrics over two registries: a base
// registry (safe, served on local /metrics) and an environment registry (sensitive
// local-network detail, pushed only via the authenticated Pushgateway by default).
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/hoststat"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/store"
	"github.com/stansat/proby/internal/version"
)

// EnvProvider returns the most recent step-1 snapshot for the env collector.
type EnvProvider func() *netinfo.NetInfo

// HostProvider returns the most recent host-stats snapshot.
type HostProvider func() *hoststat.Stats

// Metrics owns the registries and collectors.
type Metrics struct {
	instance string
	cfg      *config.Config

	baseReg *prometheus.Registry
	envReg  *prometheus.Registry

	pushSuccess  prometheus.Gauge
	pushFailures prometheus.Counter
}

// New builds the registries and registers all collectors, applying the instance
// label to every metric. host may be nil.
func New(instance string, cfg *config.Config, st *store.Store, env EnvProvider, host HostProvider) *Metrics {
	m := &Metrics{
		instance: instance,
		cfg:      cfg,
		baseReg:  prometheus.NewRegistry(),
		envReg:   prometheus.NewRegistry(),
	}

	labels := prometheus.Labels{"instance": instance}
	baseWrap := prometheus.WrapRegistererWith(labels, m.baseReg)
	envWrap := prometheus.WrapRegistererWith(labels, m.envReg)

	// Build info.
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proby_build_info",
		Help: "proby build information.",
	}, []string{"version", "commit", "goversion"})
	buildInfo.WithLabelValues(version.Version, version.Commit, version.GoVersion()).Set(1)
	baseWrap.MustRegister(buildInfo)

	// Push health (visible locally even when pushes fail).
	m.pushSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "proby_push_last_success_timestamp",
		Help: "Unix timestamp of the last successful pushgateway push.",
	})
	m.pushFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "proby_push_failures_total",
		Help: "Total number of failed pushgateway pushes.",
	})
	baseWrap.MustRegister(m.pushSuccess, m.pushFailures)

	// Store-backed collector (ping / traceroute / quality).
	baseWrap.MustRegister(&storeCollector{store: st})

	// Host-stats collector (base registry; non-sensitive values only).
	if host != nil {
		baseWrap.MustRegister(&hostCollector{provider: host})
	}

	// Environment collector (sensitive; env registry only).
	envWrap.MustRegister(&envCollector{provider: env})

	return m
}

// Handler returns the HTTP handler for the local /metrics endpoint. It serves the
// base registry, plus the environment registry only if explicitly opted in.
func (m *Metrics) Handler() http.Handler {
	gatherers := prometheus.Gatherers{m.baseReg}
	if m.cfg.Web.MetricsIncludeEnvironment {
		gatherers = append(gatherers, m.envReg)
	}
	return promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})
}

// pushGatherer returns the gatherer used for pushgateway pushes.
func (m *Metrics) pushGatherer() prometheus.Gatherer {
	if m.cfg.Pushgateway != nil && m.cfg.Pushgateway.IncludeEnvironment {
		return prometheus.Gatherers{m.baseReg, m.envReg}
	}
	return prometheus.Gatherers{m.baseReg}
}
