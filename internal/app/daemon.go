package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/history"
	"github.com/stansat/proby/internal/hoststat"
	"github.com/stansat/proby/internal/icmp"
	"github.com/stansat/proby/internal/metrics"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/prober"
	"github.com/stansat/proby/internal/store"
	"github.com/stansat/proby/internal/web"
)

// daemon holds the long-lived daemon components.
type daemon struct {
	e       *env
	store   *store.Store
	prober  *prober.Prober
	metrics *metrics.Metrics
	web     *web.Server
	history *history.History
	conn    icmp.Conn
	ni      atomic.Pointer[netinfo.NetInfo]
	hs      atomic.Pointer[hoststat.Stats]
}

// startDaemon builds the store, prober, metrics and web server and starts them,
// running until the context is cancelled. conn may be nil if ICMP is unavailable.
// quit is invoked by the web UI's localhost-only quit endpoint.
func startDaemon(ctx context.Context, e *env, ni *netinfo.NetInfo, conn icmp.Conn, quit func()) *daemon {
	st := store.New(e.cfg.Defaults.Quality.GamingReady)
	d := &daemon{e: e, store: st, conn: conn}
	d.ni.Store(ni)

	// Unless disabled, monitor the detected default gateway as an extra target.
	if e.cfg.MonitorGateway {
		if gwTarget, ok := gatewayTarget(e.cfg, ni); ok {
			e.cfg.Targets = append(e.cfg.Targets, gwTarget)
			log.Printf("monitoring default gateway %s", gwTarget.Host)
		}
	}

	// Open the history ring (soft-fail) so we can pass it as the prober's sample sink.
	var sink prober.SampleSink
	if e.cfg.History.Enabled {
		names := make([]string, 0, len(e.cfg.Targets))
		for _, t := range e.cfg.Targets {
			names = append(names, t.DisplayName())
		}
		h, err := history.Open(e.cfg.History.Path, e.cfg.History.MaxSize.Bytes(), names)
		if err != nil {
			log.Printf("history disabled: %v", err)
		} else {
			d.history = h
			sink = h
		}
	}

	pr := prober.New(e.cfg, st, sink)
	d.prober = pr

	// Replay persisted samples into the store (targets are now registered).
	if d.history != nil {
		for _, rec := range d.history.Replay() {
			st.LoadHistorical(rec.Target, store.PingSample{
				T: rec.T, RTT: time.Duration(rec.RTTns), OK: rec.OK,
			})
		}
	}

	d.metrics = metrics.New(e.instance, e.cfg, st,
		func() *netinfo.NetInfo { return d.ni.Load() },
		func() *hoststat.Stats { return d.hs.Load() },
	)
	d.web = web.New(web.Deps{
		Cfg:            e.cfg,
		Tr:             e.tr,
		Instance:       e.instance,
		Version:        versionString(),
		Store:          st,
		MetricsHandler: d.metrics.Handler(),
		NetInfo:        func() *netinfo.NetInfo { return d.ni.Load() },
		HostStat:       func() *hoststat.Stats { return d.hs.Load() },
		Quit:           quit,
	})

	go pr.Run(ctx)
	go d.metrics.RunPush(ctx)
	go d.envRefresher(ctx)
	go d.hostStatLoop(ctx)
	if d.history != nil {
		go d.history.Run(ctx, e.cfg.History.FlushInterval.Std())
	}
	if e.cfg.Web.Enabled {
		go func() {
			if err := d.web.Run(ctx); err != nil {
				log.Printf("web server error: %v", err)
			}
		}()
	}
	return d
}

// gatewayTarget builds a synthetic target for the detected default gateway, using the
// per-probe defaults. It returns ok=false if no gateway was detected or the gateway is
// already an explicitly-configured target.
func gatewayTarget(cfg *config.Config, ni *netinfo.NetInfo) (config.Target, bool) {
	gw := ""
	if ni != nil {
		gw = ni.DefaultGateway
	}
	if gw == "" {
		return config.Target{}, false
	}
	for _, t := range cfg.Targets {
		if t.Host == gw {
			return config.Target{}, false
		}
	}
	return config.Target{
		Name:       "Default gateway",
		Host:       gw,
		Ping:       cfg.Defaults.Ping,
		Traceroute: cfg.Defaults.Traceroute,
	}, true
}

// hostStatLoop periodically collects host stats into the atomic snapshot.
func (d *daemon) hostStatLoop(ctx context.Context) {
	if !d.e.cfg.HostStats.Enabled {
		return
	}
	interval := d.e.cfg.HostStats.Interval.Std()
	if interval <= 0 {
		interval = 5 * time.Second
	}
	hcfg := hoststat.Config{
		InterfaceCounters: d.e.cfg.HostStats.InterfaceCounters,
		WiFi:              d.e.cfg.HostStats.WiFi,
		ResourcePressure:  d.e.cfg.HostStats.ResourcePressure,
	}
	collect := func() {
		egress := ""
		if ni := d.ni.Load(); ni != nil {
			egress = ni.EgressIface
		}
		d.hs.Store(hoststat.Collect(hcfg, egress))
	}
	collect()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

// Shutdown performs an ordered graceful shutdown: a final metrics push, then flush and
// close the history file. Callers must have cancelled the root context first.
func (d *daemon) Shutdown() {
	if d.metrics != nil {
		d.metrics.PushNow()
	}
	if d.history != nil {
		_ = d.history.Close()
	}
}

// envRefresher periodically re-collects the step-1 snapshot so the pushed env metrics
// track changes (gateway, link type, IP).
func (d *daemon) envRefresher(ctx context.Context) {
	interval := 30 * time.Second
	if d.e.cfg.Pushgateway != nil && d.e.cfg.Pushgateway.Interval.Std() > 0 {
		interval = d.e.cfg.Pushgateway.Interval.Std()
	}
	if interval < 15*time.Second {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.ni.Store(netinfo.Collect(netinfo.Options{Conn: d.conn}))
		}
	}
}

// rootContext returns a context cancelled on SIGINT/SIGTERM.
func rootContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		cancel()
	}()
	return ctx, cancel
}
