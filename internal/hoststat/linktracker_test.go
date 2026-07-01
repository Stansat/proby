package hoststat

import (
	"testing"
	"time"
)

func TestTrackLinkTransitions(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	const iface = "test-track-iface"

	// First observation: no transition recorded.
	lc, ch := trackLink(iface, true, base)
	if !lc.IsZero() || ch != 0 {
		t.Fatalf("first sight: lc=%v ch=%d, want zero/0", lc, ch)
	}

	// Same state: still no transition.
	lc, ch = trackLink(iface, true, base.Add(time.Second))
	if !lc.IsZero() || ch != 0 {
		t.Fatalf("no-change: lc=%v ch=%d, want zero/0", lc, ch)
	}

	// Link goes down: one transition at t2.
	t2 := base.Add(2 * time.Second)
	lc, ch = trackLink(iface, false, t2)
	if ch != 1 || !lc.Equal(t2) {
		t.Fatalf("down: lc=%v ch=%d, want %v/1", lc, ch, t2)
	}

	// Link comes back up: second transition at t3.
	t3 := base.Add(3 * time.Second)
	lc, ch = trackLink(iface, true, t3)
	if ch != 2 || !lc.Equal(t3) {
		t.Fatalf("up: lc=%v ch=%d, want %v/2", lc, ch, t3)
	}
}
