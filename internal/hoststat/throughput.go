package hoststat

import (
	"sync"
	"time"
)

// ifRate holds the previous byte counters for one interface, to derive throughput.
type ifRate struct {
	rxPrev uint64
	txPrev uint64
	at     time.Time
	init   bool
}

var (
	rateMu     sync.Mutex
	rateStates = map[string]*ifRate{}
)

// trackThroughput returns the current receive/transmit rate in bytes per second,
// derived from the delta of cumulative counters between consecutive collections. The
// first observation (and any counter reset) yields 0.
func trackThroughput(name string, rx, tx uint64, now time.Time) (rxBps, txBps uint64) {
	rateMu.Lock()
	defer rateMu.Unlock()
	st := rateStates[name]
	if st == nil || !st.init {
		rateStates[name] = &ifRate{rxPrev: rx, txPrev: tx, at: now, init: true}
		return 0, 0
	}
	dt := now.Sub(st.at).Seconds()
	prevRx, prevTx := st.rxPrev, st.txPrev
	st.rxPrev, st.txPrev, st.at = rx, tx, now
	if dt <= 0 {
		return 0, 0
	}
	if rx >= prevRx {
		rxBps = uint64(float64(rx-prevRx) / dt)
	}
	if tx >= prevTx {
		txBps = uint64(float64(tx-prevTx) / dt)
	}
	return rxBps, txBps
}
