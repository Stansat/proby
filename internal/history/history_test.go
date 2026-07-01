package history

import (
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"
)

func writeN(h *History, target string, n int) {
	for i := 0; i < n; i++ {
		h.writeRecord(Record{
			Target: target,
			T:      time.UnixMilli(int64(i+1) * 1000),
			RTTns:  int64(i) * 1_000_000,
			OK:     i%2 == 0,
		})
	}
	_ = h.writeHeader()
}

func TestReplayAndRingWrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.bin")
	maxSize := int64(headerSize + recordSize*16) // capacity 16
	h, err := Open(path, maxSize, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if h.capacity != 16 {
		t.Fatalf("capacity = %d, want 16", h.capacity)
	}
	writeN(h, "a", 20) // overflow the ring by 4
	_ = h.f.Sync()

	recs := h.Replay()
	if len(recs) != 16 {
		t.Fatalf("replay len = %d, want 16 (ring wrapped)", len(recs))
	}
	// Oldest kept is i=4 -> timestamp 5000; newest is i=19 -> 20000.
	if recs[0].T.UnixMilli() != 5000 {
		t.Errorf("oldest = %d, want 5000", recs[0].T.UnixMilli())
	}
	if recs[15].T.UnixMilli() != 20000 {
		t.Errorf("newest = %d, want 20000", recs[15].T.UnixMilli())
	}
	if recs[0].Target != "a" {
		t.Errorf("target = %q", recs[0].Target)
	}
	_ = h.f.Close()
}

func TestReuseAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.bin")
	maxSize := int64(headerSize + recordSize*32)

	h, _ := Open(path, maxSize, []string{"a"})
	writeN(h, "a", 5)
	_ = h.f.Sync()
	_ = h.f.Close()

	// Reopen with the SAME names -> data is preserved.
	h2, err := Open(path, maxSize, []string{"a"})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := len(h2.Replay()); got != 5 {
		t.Fatalf("replay after reopen = %d, want 5", got)
	}
	_ = h2.f.Close()

	// Reopen with DIFFERENT names -> reinitialised, empty.
	h3, err := Open(path, maxSize, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("reopen diff: %v", err)
	}
	if got := len(h3.Replay()); got != 0 {
		t.Fatalf("replay after name change = %d, want 0 (reinit)", got)
	}
	_ = h3.f.Close()
}

func TestTornTailSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.bin")
	maxSize := int64(headerSize + recordSize*32)
	h, _ := Open(path, maxSize, []string{"a"})
	writeN(h, "a", 5)

	// Corrupt the CRC of slot 2.
	var bad [4]byte
	binary.LittleEndian.PutUint32(bad[:], 0xDEADBEEF)
	if _, err := h.f.WriteAt(bad[:], headerSize+2*recordSize+28); err != nil {
		t.Fatal(err)
	}
	_ = h.f.Sync()

	recs := h.Replay()
	if len(recs) != 4 {
		t.Fatalf("replay len = %d, want 4 (one torn record skipped)", len(recs))
	}
	_ = h.f.Close()
}
