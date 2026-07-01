// Package history persists ping samples to an append-only, fixed-size ring file so
// graphs and rollups survive restarts. The file has a fixed header (magic, version,
// ring head/count and the target-name table) followed by fixed-size records. When the
// ring is full the oldest records are overwritten.
package history

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"sync"
	"time"
)

const (
	magic       = "PROBYHS1"
	headerSize  = 4096
	recordSize  = 32
	nameSize    = 64
	nameTableAt = 44
	maxNames    = (headerSize - nameTableAt) / nameSize // 63
)

// Record is one persisted ping sample.
type Record struct {
	Target string
	T      time.Time
	RTTns  int64
	OK     bool
}

// History is the on-disk ring.
type History struct {
	mu       sync.Mutex
	f        *os.File
	capacity int64 // number of record slots
	head     int64 // next slot to write (oldest when full)
	count    int64
	names    []string
	nameID   map[string]int

	ch     chan Record
	closed chan struct{}
}

// Open opens or initialises the ring file at path with the given maximum size and
// target-name table. If the existing file is incompatible (different magic/version/
// record size or a different set of names), it is reinitialised.
func Open(path string, maxSize int64, names []string) (*History, error) {
	if len(names) > maxNames {
		names = names[:maxNames]
	}
	if maxSize < headerSize+recordSize*16 {
		maxSize = headerSize + recordSize*4096
	}
	capacity := (maxSize - headerSize) / recordSize

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	h := &History{
		f:        f,
		capacity: capacity,
		names:    names,
		nameID:   map[string]int{},
		ch:       make(chan Record, 256),
		closed:   make(chan struct{}),
	}
	for i, n := range names {
		h.nameID[n] = i
	}

	if reuse := h.tryLoadHeader(names); !reuse {
		if err := h.reinit(); err != nil {
			f.Close()
			return nil, err
		}
	}
	return h, nil
}

// tryLoadHeader reads and validates the existing header; returns true if the file can
// be reused (head/count loaded), false if it must be reinitialised.
func (h *History) tryLoadHeader(names []string) bool {
	buf := make([]byte, headerSize)
	if _, err := h.f.ReadAt(buf, 0); err != nil {
		return false
	}
	if string(buf[0:8]) != magic {
		return false
	}
	if binary.LittleEndian.Uint32(buf[8:12]) != 1 {
		return false
	}
	if binary.LittleEndian.Uint32(buf[12:16]) != recordSize {
		return false
	}
	if int64(binary.LittleEndian.Uint64(buf[16:24])) != h.capacity {
		return false
	}
	numNames := int(binary.LittleEndian.Uint32(buf[40:44]))
	if numNames != len(names) {
		return false
	}
	for i := 0; i < numNames; i++ {
		off := nameTableAt + i*nameSize
		stored := string(bytes.TrimRight(buf[off:off+nameSize], "\x00"))
		if stored != names[i] {
			return false
		}
	}
	h.head = int64(binary.LittleEndian.Uint64(buf[24:32]))
	h.count = int64(binary.LittleEndian.Uint64(buf[32:40]))
	if h.head < 0 || h.head >= h.capacity || h.count < 0 || h.count > h.capacity {
		return false
	}
	return true
}

func (h *History) reinit() error {
	h.head, h.count = 0, 0
	if err := h.f.Truncate(headerSize + h.capacity*recordSize); err != nil {
		return err
	}
	return h.writeHeader()
}

func (h *History) writeHeader() error {
	buf := make([]byte, headerSize)
	copy(buf[0:8], magic)
	binary.LittleEndian.PutUint32(buf[8:12], 1)
	binary.LittleEndian.PutUint32(buf[12:16], recordSize)
	binary.LittleEndian.PutUint64(buf[16:24], uint64(h.capacity))
	binary.LittleEndian.PutUint64(buf[24:32], uint64(h.head))
	binary.LittleEndian.PutUint64(buf[32:40], uint64(h.count))
	binary.LittleEndian.PutUint32(buf[40:44], uint32(len(h.names)))
	for i, n := range h.names {
		off := nameTableAt + i*nameSize
		b := []byte(n)
		if len(b) > nameSize {
			b = b[:nameSize]
		}
		copy(buf[off:off+nameSize], b)
	}
	_, err := h.f.WriteAt(buf, 0)
	return err
}

// Append enqueues a sample for persistence. It never blocks: if the buffer is full the
// sample is dropped so probing is never stalled by disk I/O.
func (h *History) Append(target string, t time.Time, rttns int64, ok bool) {
	select {
	case h.ch <- Record{Target: target, T: t, RTTns: rttns, OK: ok}:
	default:
	}
}

// Run drains the queue, writing records and flushing the header periodically, until
// ctx is cancelled.
func (h *History) Run(ctx context.Context, flush time.Duration) {
	if flush <= 0 {
		flush = 5 * time.Second
	}
	ticker := time.NewTicker(flush)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case <-ctx.Done():
			h.drain(&dirty)
			if dirty {
				h.mu.Lock()
				_ = h.writeHeader()
				h.mu.Unlock()
			}
			_ = h.f.Sync()
			close(h.closed)
			return
		case rec := <-h.ch:
			h.writeRecord(rec)
			dirty = true
		case <-ticker.C:
			if dirty {
				h.mu.Lock()
				_ = h.writeHeader()
				_ = h.f.Sync()
				h.mu.Unlock()
				dirty = false
			}
		}
	}
}

func (h *History) drain(dirty *bool) {
	for {
		select {
		case rec := <-h.ch:
			h.writeRecord(rec)
			*dirty = true
		default:
			return
		}
	}
}

func (h *History) writeRecord(rec Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.nameID[rec.Target]
	if !ok {
		return
	}
	var buf [recordSize]byte
	binary.LittleEndian.PutUint16(buf[0:2], uint16(id))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(rec.T.UnixMilli()))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(rec.RTTns))
	if rec.OK {
		buf[24] = 1
	}
	crc := crc32.ChecksumIEEE(buf[0:28])
	binary.LittleEndian.PutUint32(buf[28:32], crc)

	off := headerSize + h.head*recordSize
	if _, err := h.f.WriteAt(buf[:], off); err != nil {
		return
	}
	h.head = (h.head + 1) % h.capacity
	if h.count < h.capacity {
		h.count++
	}
}

// Replay returns all persisted records in chronological (oldest-first) order.
func (h *History) Replay() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Record, 0, h.count)
	var start int64
	if h.count < h.capacity {
		start = 0
	} else {
		start = h.head // oldest slot when full
	}
	for i := int64(0); i < h.count; i++ {
		slot := (start + i) % h.capacity
		off := headerSize + slot*recordSize
		var buf [recordSize]byte
		if _, err := h.f.ReadAt(buf[:], off); err != nil {
			continue
		}
		crc := binary.LittleEndian.Uint32(buf[28:32])
		if crc != crc32.ChecksumIEEE(buf[0:28]) {
			continue // torn/corrupt record
		}
		id := int(binary.LittleEndian.Uint16(buf[0:2]))
		if id >= len(h.names) {
			continue
		}
		out = append(out, Record{
			Target: h.names[id],
			T:      time.UnixMilli(int64(binary.LittleEndian.Uint64(buf[8:16]))),
			RTTns:  int64(binary.LittleEndian.Uint64(buf[16:24])),
			OK:     buf[24] == 1,
		})
	}
	return out
}

// Close waits for Run to finish flushing and closes the file. Callers should cancel
// the context passed to Run first.
func (h *History) Close() error {
	select {
	case <-h.closed:
	case <-time.After(2 * time.Second):
	}
	return h.f.Close()
}
