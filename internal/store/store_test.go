package store

import (
	"testing"
	"time"

	"github.com/stansat/proby/internal/config"
)

func gamingCfg() config.GamingThresholds {
	return config.GamingThresholds{
		MaxRTT:    config.Duration(60 * time.Millisecond),
		MaxJitter: config.Duration(10 * time.Millisecond),
		MaxLoss:   0.02,
	}
}

func TestRollupsAndLoss(t *testing.T) {
	s := New(gamingCfg())
	s.Register("t", "host", "1.2.3.4", 0)
	now := time.Now()
	// 8 OK samples of 10ms, 2 losses => 20% loss.
	for i := 0; i < 8; i++ {
		s.AddPing("t", PingSample{T: now, RTT: 10 * time.Millisecond, OK: true})
	}
	for i := 0; i < 2; i++ {
		s.AddPing("t", PingSample{T: now, OK: false})
	}
	st, ok := s.TargetSnapshot("t")
	if !ok {
		t.Fatal("missing target")
	}
	if st.Sent != 10 || st.Received != 8 {
		t.Errorf("sent/recv = %d/%d, want 10/8", st.Sent, st.Received)
	}
	if st.LossRatio < 0.19 || st.LossRatio > 0.21 {
		t.Errorf("loss = %.3f, want ~0.20", st.LossRatio)
	}
	if st.AvgRTT != 10*time.Millisecond {
		t.Errorf("avg = %v, want 10ms", st.AvgRTT)
	}
	if st.P50 != 10*time.Millisecond {
		t.Errorf("p50 = %v", st.P50)
	}
}

func TestRollupWindowIsTimeBased(t *testing.T) {
	s := New(gamingCfg())
	s.Register("t", "h", "1.2.3.4", 0)
	now := time.Now()

	// Old samples (20s ago) with high RTT + losses — must be excluded from the 15s window.
	for i := 0; i < 10; i++ {
		s.AddPing("t", PingSample{T: now.Add(-20 * time.Second), RTT: 500 * time.Millisecond, OK: true})
	}
	for i := 0; i < 10; i++ {
		s.AddPing("t", PingSample{T: now.Add(-20 * time.Second), OK: false})
	}
	// Recent samples (now) with low RTT, no loss.
	for i := 0; i < 5; i++ {
		s.AddPing("t", PingSample{T: now, RTT: 10 * time.Millisecond, OK: true})
	}

	st, _ := s.TargetSnapshot("t")
	if st.AvgRTT != 10*time.Millisecond {
		t.Errorf("avg = %v, want 10ms (older samples must be excluded)", st.AvgRTT)
	}
	if st.LossRatio != 0 {
		t.Errorf("loss = %.3f, want 0 (old losses excluded)", st.LossRatio)
	}
	if st.MaxRTT != 10*time.Millisecond {
		t.Errorf("max = %v, want 10ms (old 500ms excluded)", st.MaxRTT)
	}
}

func TestGamingReadyVerdict(t *testing.T) {
	s := New(gamingCfg())
	s.Register("good", "h", "1.1.1.1", 0)
	for i := 0; i < 20; i++ {
		s.AddPing("good", PingSample{T: time.Now(), RTT: 15 * time.Millisecond, OK: true})
	}
	st, _ := s.TargetSnapshot("good")
	if !st.GamingReady {
		t.Errorf("expected gaming-ready for low-latency stable link: %+v", st)
	}

	s.Register("bad", "h", "2.2.2.2", 0)
	for i := 0; i < 20; i++ {
		s.AddPing("bad", PingSample{T: time.Now(), RTT: 200 * time.Millisecond, OK: true})
	}
	st2, _ := s.TargetSnapshot("bad")
	if st2.GamingReady {
		t.Errorf("expected NOT gaming-ready for high latency: %+v", st2)
	}
}

func TestMOSRanges(t *testing.T) {
	// Perfect conditions -> high MOS.
	if m := estimateMOS(10*time.Millisecond, 1*time.Millisecond, 0); m < 4.0 {
		t.Errorf("MOS for great link = %.2f, want >= 4.0", m)
	}
	// Terrible conditions -> low MOS.
	if m := estimateMOS(500*time.Millisecond, 100*time.Millisecond, 0.3); m > 2.5 {
		t.Errorf("MOS for bad link = %.2f, want <= 2.5", m)
	}
}

func TestTracerouteAggregation(t *testing.T) {
	s := New(gamingCfg())
	s.Register("t", "h", "1.1.1.1", 0)
	// Two cycles for a 2-hop path.
	for c := 0; c < 2; c++ {
		s.UpdateTraceroute("t", []TraceHopInput{
			{Hop: 1, Addr: "10.0.0.1", Queries: 3, Recv: 3, RTT: 2 * time.Millisecond},
			{Hop: 2, Addr: "1.1.1.1", Queries: 3, Recv: 3, RTT: 10 * time.Millisecond},
		})
	}
	st, _ := s.TargetSnapshot("t")
	if len(st.Hops) != 2 {
		t.Fatalf("hops = %d, want 2", len(st.Hops))
	}
	h1 := st.Hops[0]
	if h1.Addr != "10.0.0.1" || h1.Sent != 6 || h1.Recv != 6 {
		t.Errorf("hop1 = %+v", h1)
	}
	if h1.LossRatio != 0 {
		t.Errorf("hop1 loss = %.2f, want 0", h1.LossRatio)
	}
	if st.Hops[1].AvgRTT != 10*time.Millisecond {
		t.Errorf("hop2 avg = %v, want 10ms", st.Hops[1].AvgRTT)
	}
}

func TestJitterAccumulates(t *testing.T) {
	s := New(gamingCfg())
	s.Register("t", "h", "1.1.1.1", 0)
	rtts := []time.Duration{10, 20, 10, 30, 10}
	for _, r := range rtts {
		s.AddPing("t", PingSample{T: time.Now(), RTT: r * time.Millisecond, OK: true})
	}
	st, _ := s.TargetSnapshot("t")
	if st.Jitter <= 0 {
		t.Errorf("expected positive jitter, got %v", st.Jitter)
	}
}
