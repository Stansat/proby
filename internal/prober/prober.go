// Package prober schedules per-target ICMP ping and traceroute workers and writes
// their results into the store.
package prober

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/icmp"
	"github.com/stansat/proby/internal/store"
)

// SampleSink receives each ping sample for persistence (e.g. the history ring file).
type SampleSink interface {
	Append(target string, t time.Time, rttns int64, ok bool)
}

// Prober runs the ping/traceroute workers for all configured targets.
type Prober struct {
	cfg   *config.Config
	store *store.Store
	dns   *dnsCache
	sink  SampleSink
}

// New creates a Prober. Targets are registered in the store immediately so they
// appear (as "down") before the first sample arrives. sink may be nil.
func New(cfg *config.Config, st *store.Store, sink SampleSink) *Prober {
	p := &Prober{cfg: cfg, store: st, dns: newDNSCache(), sink: sink}
	for _, t := range cfg.Targets {
		ip := resolveTarget(t.Host)
		ipStr := ""
		if ip != nil {
			ipStr = ip.String()
		}
		st.Register(t.DisplayName(), t.Host, ipStr, t.Ping.DSCP)
	}
	return p
}

// Run starts all workers and blocks until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range p.cfg.Targets {
		t := p.cfg.Targets[i]
		wg.Add(2)
		go func() { defer wg.Done(); p.pingWorker(ctx, t) }()
		go func() { defer wg.Done(); p.traceWorker(ctx, t) }()
	}
	wg.Wait()
}

func (p *Prober) pingWorker(ctx context.Context, t config.Target) {
	conn, err := icmp.Open()
	if err != nil {
		log.Printf("ping %s: cannot open ICMP: %v", t.DisplayName(), err)
		return
	}
	defer conn.Close()

	interval := t.Ping.Interval.Std()
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ip := resolveTarget(t.Host)
			if ip == nil {
				p.store.AddPing(t.DisplayName(), store.PingSample{T: time.Now(), OK: false})
				continue
			}
			r, err := conn.Probe(ip, 64, t.Ping.DSCP, t.Ping.Timeout.Std(), t.Ping.PayloadSize)
			sample := store.PingSample{T: time.Now(), OK: err == nil && r.OK(), RTT: r.RTT}
			p.store.AddPing(t.DisplayName(), sample)
			if p.sink != nil {
				p.sink.Append(t.DisplayName(), sample.T, int64(sample.RTT), sample.OK)
			}
		}
	}
}

func (p *Prober) traceWorker(ctx context.Context, t config.Target) {
	conn, err := icmp.Open()
	if err != nil {
		log.Printf("trace %s: cannot open ICMP: %v", t.DisplayName(), err)
		return
	}
	defer conn.Close()

	interval := t.Traceroute.Interval.Std()
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run one cycle promptly so the UI has a hop table quickly.
	p.runTrace(t, conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runTrace(t, conn)
		}
	}
}

func (p *Prober) runTrace(t config.Target, conn icmp.Conn) {
	ip := resolveTarget(t.Host)
	if ip == nil {
		return
	}
	hops, err := icmp.Trace(conn, ip, t.Traceroute.MaxHops, t.Traceroute.Queries, t.Traceroute.DSCP, t.Traceroute.Timeout.Std())
	if err != nil {
		return
	}
	inputs := make([]store.TraceHopInput, 0, len(hops))
	for _, h := range hops {
		in := store.TraceHopInput{Hop: h.TTL, Queries: h.Queries, Recv: h.Recv, RTT: h.RTT}
		if h.Addr != nil {
			in.Addr = h.Addr.String()
			in.Host = p.dns.lookup(h.Addr.String())
		}
		inputs = append(inputs, in)
	}
	p.store.UpdateTraceroute(t.DisplayName(), inputs)
}

// resolveTarget resolves host to its first IPv4 address, or returns nil.
func resolveTarget(host string) net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return ip.To4()
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4
		}
	}
	return nil
}
