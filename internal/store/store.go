// Package store keeps per-target time-series of ping and traceroute samples and
// derives rollups (loss, percentiles, RFC-3550 jitter, MOS, and a "gaming ready"
// verdict). It is safe for concurrent use.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/stansat/proby/internal/config"
)

// ringCap is the number of ping samples retained per target (for graphs).
const ringCap = 3600

// rollupWindow is the time window used for the "current" rollups shown in the UI and
// metrics (avg/min/max/percentile RTT, loss). Kept short so the status reflects
// recent conditions rather than a long average.
const rollupWindow = 15 * time.Second

// PingSample is a single ping observation.
type PingSample struct {
	T   time.Time     `json:"t"`
	RTT time.Duration `json:"rtt"`
	OK  bool          `json:"ok"`
}

// HopStats is the rolling MTR-style aggregate for one traceroute hop.
type HopStats struct {
	Hop       int           `json:"hop"`
	Addr      string        `json:"addr"`
	Host      string        `json:"host,omitempty"`
	Sent      int           `json:"sent"`
	Recv      int           `json:"recv"`
	LossRatio float64       `json:"loss_ratio"`
	LastRTT   time.Duration `json:"last_rtt"`
	AvgRTT    time.Duration `json:"avg_rtt"`
	BestRTT   time.Duration `json:"best_rtt"`
	WorstRTT  time.Duration `json:"worst_rtt"`
	StdDevRTT time.Duration `json:"stddev_rtt"`
}

// TargetStats is a point-in-time snapshot of a target's derived metrics.
type TargetStats struct {
	Name        string        `json:"name"`
	Host        string        `json:"host"`
	IP          string        `json:"ip"`
	DSCP        int           `json:"dscp"`
	Up          bool          `json:"up"`
	Sent        uint64        `json:"sent"`
	Received    uint64        `json:"received"`
	LastRTT     time.Duration `json:"last_rtt"`
	MinRTT      time.Duration `json:"min_rtt"`
	AvgRTT      time.Duration `json:"avg_rtt"`
	MaxRTT      time.Duration `json:"max_rtt"`
	P50         time.Duration `json:"p50"`
	P90         time.Duration `json:"p90"`
	P95         time.Duration `json:"p95"`
	P99         time.Duration `json:"p99"`
	LossRatio   float64       `json:"loss_ratio"`
	Jitter      time.Duration `json:"jitter"`
	MOS         float64       `json:"mos"`
	GamingReady bool          `json:"gaming_ready"`
	Hops        []HopStats    `json:"hops"`
	Updated     time.Time     `json:"updated"`
}

// hopAcc accumulates rolling stats for a single hop across cycles.
type hopAcc struct {
	addr  string
	host  string
	sent  int
	recv  int
	last  time.Duration
	best  time.Duration
	worst time.Duration
	sum   float64 // sum of RTT seconds
	sumSq float64 // sum of RTT seconds squared
}

type targetState struct {
	name string
	host string
	ip   string
	dscp int

	samples  []PingSample // ring buffer (append, trim front)
	sent     uint64
	received uint64

	jitter   float64 // seconds, RFC-3550 running estimate
	lastRTT  time.Duration
	haveLast bool

	hops    []hopAcc
	updated time.Time
}

// Store holds all target state.
type Store struct {
	mu      sync.RWMutex
	targets map[string]*targetState
	order   []string
	gaming  config.GamingThresholds
}

// New creates a Store using the given gaming-ready thresholds.
func New(gaming config.GamingThresholds) *Store {
	return &Store{targets: map[string]*targetState{}, gaming: gaming}
}

// Register adds a target so it appears in snapshots before any samples arrive.
func (s *Store) Register(name, host, ip string, dscp int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.targets[name]; ok {
		return
	}
	s.targets[name] = &targetState{name: name, host: host, ip: ip, dscp: dscp}
	s.order = append(s.order, name)
}

// AddPing records one ping sample and updates running jitter.
func (s *Store) AddPing(name string, sample PingSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.targets[name]
	if ts == nil {
		return
	}
	ts.sent++
	if sample.OK {
		ts.received++
		// RFC-3550 jitter: J += (|D| - J)/16 using consecutive RTT differences.
		if ts.haveLast {
			d := (sample.RTT - ts.lastRTT).Seconds()
			if d < 0 {
				d = -d
			}
			ts.jitter += (d - ts.jitter) / 16
		}
		ts.lastRTT = sample.RTT
		ts.haveLast = true
	}
	ts.samples = append(ts.samples, sample)
	if len(ts.samples) > ringCap {
		ts.samples = ts.samples[len(ts.samples)-ringCap:]
	}
	ts.updated = sample.T
}

// LoadHistorical appends a replayed sample without counting it toward sent/received
// totals or jitter (which reflect the live session). It only refills the ring so
// graphs and window rollups (loss, percentiles) survive a restart.
func (s *Store) LoadHistorical(name string, sample PingSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.targets[name]
	if ts == nil {
		return
	}
	ts.samples = append(ts.samples, sample)
	if len(ts.samples) > ringCap {
		ts.samples = ts.samples[len(ts.samples)-ringCap:]
	}
	if sample.T.After(ts.updated) {
		ts.updated = sample.T
	}
}

// TraceHopInput is one hop from a traceroute cycle fed into the store.
type TraceHopInput struct {
	Hop     int
	Addr    string
	Host    string
	Queries int
	Recv    int
	RTT     time.Duration
}

// UpdateTraceroute merges a traceroute cycle into the rolling hop stats.
func (s *Store) UpdateTraceroute(name string, hops []TraceHopInput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.targets[name]
	if ts == nil {
		return
	}
	// Resize accumulator slice to the observed hop count.
	if len(ts.hops) < len(hops) {
		grown := make([]hopAcc, len(hops))
		copy(grown, ts.hops)
		ts.hops = grown
	}
	for i, h := range hops {
		acc := &ts.hops[i]
		if h.Addr != "" {
			acc.addr = h.Addr
		}
		if h.Host != "" {
			acc.host = h.Host
		}
		acc.sent += h.Queries
		if h.Recv > 0 {
			acc.recv += h.Recv
			acc.last = h.RTT
			sec := h.RTT.Seconds()
			acc.sum += sec * float64(h.Recv)
			acc.sumSq += sec * sec * float64(h.Recv)
			if acc.best == 0 || h.RTT < acc.best {
				acc.best = h.RTT
			}
			if h.RTT > acc.worst {
				acc.worst = h.RTT
			}
		}
	}
	ts.updated = time.Now()
}

// Snapshot returns derived stats for all targets in registration order.
func (s *Store) Snapshot() []TargetStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TargetStats, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.deriveLocked(s.targets[name]))
	}
	return out
}

// TargetSnapshot returns derived stats for one target.
func (s *Store) TargetSnapshot(name string) (TargetStats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ts := s.targets[name]
	if ts == nil {
		return TargetStats{}, false
	}
	return s.deriveLocked(ts), true
}

// PingSeries returns up to `limit` most-recent ping samples for a target.
func (s *Store) PingSeries(name string, limit int) []PingSample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ts := s.targets[name]
	if ts == nil {
		return nil
	}
	src := ts.samples
	if limit > 0 && len(src) > limit {
		src = src[len(src)-limit:]
	}
	out := make([]PingSample, len(src))
	copy(out, src)
	return out
}

func (s *Store) deriveLocked(ts *targetState) TargetStats {
	st := TargetStats{
		Name:     ts.name,
		Host:     ts.host,
		IP:       ts.ip,
		DSCP:     ts.dscp,
		Sent:     ts.sent,
		Received: ts.received,
		Jitter:   time.Duration(ts.jitter * float64(time.Second)),
		Updated:  ts.updated,
	}

	// Rollups reflect the most recent rollupWindow of time (the "current" status),
	// walking backwards from the newest sample until we fall outside the window.
	cutoff := time.Now().Add(-rollupWindow)
	var rtts []time.Duration
	var sum time.Duration
	total := 0
	okCount := 0
	for i := len(ts.samples) - 1; i >= 0; i-- {
		sample := ts.samples[i]
		if sample.T.Before(cutoff) {
			break
		}
		total++
		if sample.OK {
			rtts = append(rtts, sample.RTT)
			sum += sample.RTT
			okCount++
		}
	}
	if total > 0 {
		st.LossRatio = float64(total-okCount) / float64(total)
	}
	if okCount > 0 {
		st.AvgRTT = sum / time.Duration(okCount)
		sorted := append([]time.Duration(nil), rtts...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		st.MinRTT = sorted[0]
		st.MaxRTT = sorted[len(sorted)-1]
		st.P50 = percentile(sorted, 0.50)
		st.P90 = percentile(sorted, 0.90)
		st.P95 = percentile(sorted, 0.95)
		st.P99 = percentile(sorted, 0.99)
	}
	if ts.haveLast {
		st.LastRTT = ts.lastRTT
	}
	st.Up = okCount > 0

	st.MOS = estimateMOS(st.AvgRTT, st.Jitter, st.LossRatio)
	st.GamingReady = st.Up &&
		st.AvgRTT <= s.gaming.MaxRTT.Std() &&
		st.Jitter <= s.gaming.MaxJitter.Std() &&
		st.LossRatio <= s.gaming.MaxLoss

	st.Hops = deriveHops(ts.hops)
	return st
}

func deriveHops(accs []hopAcc) []HopStats {
	out := make([]HopStats, 0, len(accs))
	for i, a := range accs {
		h := HopStats{
			Hop:      i + 1,
			Addr:     a.addr,
			Host:     a.host,
			Sent:     a.sent,
			Recv:     a.recv,
			LastRTT:  a.last,
			BestRTT:  a.best,
			WorstRTT: a.worst,
		}
		if a.sent > 0 {
			h.LossRatio = float64(a.sent-a.recv) / float64(a.sent)
		}
		if a.recv > 0 {
			mean := a.sum / float64(a.recv)
			h.AvgRTT = time.Duration(mean * float64(time.Second))
			variance := a.sumSq/float64(a.recv) - mean*mean
			if variance > 0 {
				h.StdDevRTT = time.Duration(sqrt(variance) * float64(time.Second))
			}
		}
		out = append(out, h)
	}
	return out
}

// RTTBuckets are the fixed histogram upper bounds (seconds) for RTT.
var RTTBuckets = []float64{0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1}

// RTTHistogram returns a Prometheus-style cumulative histogram of successful RTTs
// over the retained window for a target.
func (s *Store) RTTHistogram(name string) (count uint64, sum float64, buckets map[float64]uint64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ts := s.targets[name]
	if ts == nil {
		return 0, 0, nil, false
	}
	buckets = make(map[float64]uint64, len(RTTBuckets))
	for _, b := range RTTBuckets {
		buckets[b] = 0
	}
	for _, sample := range ts.samples {
		if !sample.OK {
			continue
		}
		sec := sample.RTT.Seconds()
		count++
		sum += sec
		for _, b := range RTTBuckets {
			if sec <= b {
				buckets[b]++
			}
		}
	}
	return count, sum, buckets, true
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
