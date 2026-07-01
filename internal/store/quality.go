package store

import (
	"math"
	"time"
)

// estimateMOS returns a Mean Opinion Score (1.0-4.5) estimated from round-trip
// latency, jitter and loss using a simplified ITU-T E-model (G.711-style constants).
// It is a rough but useful indicator, not a calibrated measurement.
func estimateMOS(rtt, jitter time.Duration, loss float64) float64 {
	// Effective one-way latency in milliseconds: half the RTT, plus a jitter
	// allowance and a nominal codec/de-jitter delay.
	rttMs := float64(rtt) / float64(time.Millisecond)
	jitMs := float64(jitter) / float64(time.Millisecond)
	effLat := rttMs/2 + 2*jitMs + 10

	// R-factor: start at 93.2 and subtract latency and loss penalties.
	var r float64
	if effLat < 160 {
		r = 93.2 - effLat/40
	} else {
		r = 93.2 - (effLat-120)/10
	}
	r -= 2.5 * (loss * 100) // each 1% loss costs ~2.5 R points

	if r < 0 {
		r = 0
	}
	if r > 100 {
		r = 100
	}

	// Map R to MOS.
	mos := 1 + 0.035*r + r*(r-60)*(100-r)*7e-6
	if mos < 1 {
		mos = 1
	}
	if mos > 4.5 {
		mos = 4.5
	}
	return mos
}

func sqrt(x float64) float64 { return math.Sqrt(x) }
