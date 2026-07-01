package hoststat

import (
	"sync"
	"time"
)

// linkState records the last observed link state for one interface.
type linkState struct {
	up         bool
	init       bool
	lastChange time.Time
	changes    uint64
}

var (
	linkMu     sync.Mutex
	linkStates = map[string]*linkState{}
)

// trackLink records the current link state for an interface and returns the time of the
// last up<->down transition and the total number of transitions observed this session.
//
// On the first observation of an interface no transition is recorded (lastChange is the
// zero time), so `time() - last_change_timestamp` stays large until a real flap happens
// — making it safe to alert on recent physical disconnects.
func trackLink(name string, up bool, now time.Time) (lastChange time.Time, changes uint64) {
	linkMu.Lock()
	defer linkMu.Unlock()
	st := linkStates[name]
	if st == nil {
		linkStates[name] = &linkState{up: up, init: true}
		return time.Time{}, 0
	}
	if up != st.up {
		st.up = up
		st.lastChange = now
		st.changes++
	}
	return st.lastChange, st.changes
}
