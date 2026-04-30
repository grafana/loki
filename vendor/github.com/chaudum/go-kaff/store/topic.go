package store

import (
	"fmt"
	"sync"
	"time"
)

// ── Value types ───────────────────────────────────────────────────────────────

// Header is a key/value pair attached to a Record.
type Header struct {
	Key   string
	Value []byte
}

// Record is a single message stored in a Kafka partition.
type Record struct {
	// Offset is the absolute partition offset, assigned by Partition.Append.
	Offset    int64
	Timestamp time.Time
	Key       []byte
	Value     []byte
	Headers   []Header
}

// ── Partition ─────────────────────────────────────────────────────────────────

// Partition is an append-only, ordered sequence of Records.
// All exported methods are safe for concurrent use.
type Partition struct {
	id int32

	mu         sync.RWMutex
	records    []Record
	logStart   int64 // absolute offset of records[0]; advances on eviction
	totalBytes int64 // approximate byte footprint of all non-evicted records
	maxBytes   int64 // soft cap (0 = unlimited)

	// notify is replaced (old closed) on each Append so that long-polling
	// Fetch handlers are woken immediately.  Callers must capture it via
	// WaitChan() BEFORE checking for new records to avoid races.
	notifyMu sync.Mutex
	notify   chan struct{}
}

func newPartition(id int32, maxBytes int64) *Partition {
	return &Partition{
		id:       id,
		maxBytes: maxBytes,
		notify:   make(chan struct{}),
	}
}

// recordSize returns an approximate byte count for a single Record.
func recordSize(r *Record) int64 {
	sz := int64(len(r.Key) + len(r.Value) + 32)
	for _, h := range r.Headers {
		sz += int64(len(h.Key) + len(h.Value))
	}
	return sz
}

// Append assigns monotonically increasing offsets to records and appends them.
// Returns the base offset of the first appended record.
// If the partition has a maxBytes cap, oldest records are evicted after the
// append to keep totalBytes within the limit.
func (p *Partition) Append(records []Record) int64 {
	p.mu.Lock()
	base := p.logStart + int64(len(p.records))
	for i := range records {
		records[i].Offset = base + int64(i)
		p.totalBytes += recordSize(&records[i])
		p.records = append(p.records, records[i])
	}
	if p.maxBytes > 0 {
		p.evictLocked()
	}
	p.mu.Unlock()

	// Wake all long-poll waiters by closing the current channel and
	// installing a fresh one for future waits.
	p.notifyMu.Lock()
	old := p.notify
	p.notify = make(chan struct{})
	p.notifyMu.Unlock()
	close(old)

	return base
}

// evictLocked removes the oldest records until totalBytes <= maxBytes,
// advancing logStart accordingly.  At least one record is always retained.
// Must be called with p.mu held (write lock).
func (p *Partition) evictLocked() {
	for len(p.records) > 1 && p.totalBytes > p.maxBytes {
		p.totalBytes -= recordSize(&p.records[0])
		p.records[0] = Record{} // release key/value memory
		p.records = p.records[1:]
		p.logStart++
	}
}

// Read returns records starting at offset up to roughly maxBytes of data.
// At least one record is always returned if one exists at or after offset.
// Returns nil when offset equals HighWaterMark (nothing new yet).
// Returns an error when offset is out of range or below the log start (evicted).
func (p *Partition) Read(offset int64, maxBytes int) ([]Record, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	hwm := p.logStart + int64(len(p.records))
	if offset < p.logStart || offset > hwm {
		return nil, fmt.Errorf("store: offset %d out of range [%d, %d)", offset, p.logStart, hwm)
	}
	if offset == hwm {
		return nil, nil
	}

	var out []Record
	size := 0
	for i := offset - p.logStart; i < int64(len(p.records)); i++ {
		r := p.records[i]
		// Approximate record size; always include at least the first record.
		approx := int(recordSize(&r))
		if len(out) > 0 && size+approx > maxBytes {
			break
		}
		out = append(out, r)
		size += approx
	}
	return out, nil
}

// HighWaterMark returns the next offset to be assigned.
func (p *Partition) HighWaterMark() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.logStart + int64(len(p.records))
}

// LogStartOffset returns the earliest available offset (advances after eviction).
func (p *Partition) LogStartOffset() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.logStart
}

// WaitChan returns a channel that will be closed when new records are appended.
//
// Callers MUST capture this channel before calling Read/HighWaterMark to
// guarantee they never miss a notification:
//
//	ch := p.WaitChan()
//	recs, _ := p.Read(offset, maxBytes)
//	if len(recs) == 0 {
//	    <-ch  // safe: fires immediately if Append happened in the window
//	}
func (p *Partition) WaitChan() <-chan struct{} {
	p.notifyMu.Lock()
	defer p.notifyMu.Unlock()
	return p.notify
}

// ── Topic ─────────────────────────────────────────────────────────────────────

// Topic holds the ordered partitions for a named Kafka topic.
// The Topic struct itself is immutable after creation; all mutation is
// within individual Partitions.
type Topic struct {
	name       string
	partitions []*Partition
}

func newTopic(name string, numPartitions int32, maxBytesPerPartition int64) *Topic {
	parts := make([]*Partition, numPartitions)
	for i := range parts {
		parts[i] = newPartition(int32(i), maxBytesPerPartition)
	}
	return &Topic{name: name, partitions: parts}
}

// Close wakes any goroutines blocked in WaitChan() on any of this topic's
// partitions.  Used during topic deletion to unblock long-poll Fetch handlers.
func (t *Topic) Close() {
	for _, p := range t.partitions {
		p.notifyMu.Lock()
		old := p.notify
		p.notify = make(chan struct{})
		p.notifyMu.Unlock()
		close(old)
	}
}

// Name returns the topic name.
func (t *Topic) Name() string { return t.name }

// NumPartitions returns the number of partitions.
func (t *Topic) NumPartitions() int32 { return int32(len(t.partitions)) }

// Partition returns the Partition with the given index, or (nil, false) if the
// index is out of range.
func (t *Topic) Partition(id int32) (*Partition, bool) {
	if id < 0 || int(id) >= len(t.partitions) {
		return nil, false
	}
	return t.partitions[id], true
}
