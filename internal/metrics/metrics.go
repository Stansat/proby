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

	// Local registries carry the instance label (there is no pushgateway to add it).
	localBaseReg *prometheus.Registry
	localEnvReg  *prometheus.Registry
	// Push registries omit the instance label; the pushgateway attaches it from the
	// grouping key (see newPusher). A metric may not carry a label that is also a
	// grouping key, so it must NOT be present here.
	pushBaseReg *prometheus.Registry
	pushEnvReg  *prometheus.Registry

	pushSuccess  prometheus.Gauge
	pushFailures prometheus.Counter
}

// New builds the registries and registers all collectors. The same collector
// instances are registered in both the local registries (with an instance const
// label) and the push registries (without it — instance comes from the grouping key).
// host may be nil.
func New(instance string, cfg *config.Config, st *store.Store, env EnvProvider, host HostProvider) *Metrics {
	m := &Metrics{
		instance:     instance,
		cfg:          cfg,
		localBaseReg: prometheus.NewRegistry(),
		localEnvReg:  prometheus.NewRegistry(),
		pushBaseReg:  prometheus.NewRegistry(),
		pushEnvReg:   prometheus.NewRegistry(),
	}

	// Build info.
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "proby_build_info",
		Help: "proby build information.",
	}, []string{"version", "commit", "goversion"})
	buildInfo.WithLabelValues(version.Version, version.Commit, version.GoVersion()).Set(1)

	// Push health (visible locally even when pushes fail).
	m.pushSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "proby_push_last_success_timestamp",
		Help: "Unix timestamp of the last successful pushgateway push.",
	})
	m.pushFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "proby_push_failures_total",
		Help: "Total number of failed pushgateway pushes.",
	})

	base := []prometheus.Collector{buildInfo, m.pushSuccess, m.pushFailures, &storeCollector{store: st}}
	if host != nil {
		base = append(base, &hostCollector{provider: host})
	}
	env2 := []prometheus.Collector{&envCollector{provider: env}}

	// Local: wrap with the instance label.
	instanceLabels := prometheus.Labels{"instance": instance}
	prometheus.WrapRegistererWith(instanceLabels, m.localBaseReg).MustRegister(base...)
	prometheus.WrapRegistererWith(instanceLabels, m.localEnvReg).MustRegister(env2...)
	// Push: no instance label (grouping key supplies it).
	m.pushBaseReg.MustRegister(base...)
	m.pushEnvReg.MustRegister(env2...)

	return m
}

// Handler returns the HTTP handler for the local /metrics endpoint. It serves the
// base registry, plus the environment registry only if explicitly opted in.
func (m *Metrics) Handler() http.Handler {
	gatherers := prometheus.Gatherers{m.localBaseReg}
	if m.cfg.Web.MetricsIncludeEnvironment {
		gatherers = append(gatherers, m.localEnvReg)
	}
	return promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})
}

// pushGatherer returns the gatherer used for pushgateway pushes (no instance label).
func (m *Metrics) pushGatherer() prometheus.Gatherer {
	if m.cfg.Pushgateway != nil && m.cfg.Pushgateway.IncludeEnvironment {
		return prometheus.Gatherers{m.pushBaseReg, m.pushEnvReg}
	}
	return prometheus.Gatherers{m.pushBaseReg}
}
