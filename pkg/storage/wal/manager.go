package wal

import (
	"container/list"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	// MaxAge is the default maximum amount of time a segment can be buffered
	// in memory before it should be flushed.
	MaxAge = 500 * time.Millisecond
	// MaxSegments is the default maximum number of segments that can be buffered
	// in memory, including closed segments waiting to be flushed.
	MaxSegments = 10
	// MaxSegmentSize is the default maximum segment size (8MB).
	MaxSegmentSize = 8 * 1024 * 1024
)

var (
	// ErrFull is an error returned when there are no available segments, as
	// all segments are closed and waiting to be flushed.
	ErrFull = errors.New("The WAL is full")
)

type AppendRequest struct {
	TenantID  string
	Labels    labels.Labels
	LabelsStr string
	Entries   []*logproto.Entry
}

type AppendResult struct {
	done chan struct{}
	err  error
}

// Done returns a channel that is closed when the result is available.
func (p *AppendResult) Done() <-chan struct{} {
	return p.done
}

// Err returns a non-nil error if the append failed. It should not be called
// until Done() is closed to avoid data races.
func (p *AppendResult) Err() error {
	return p.err
}

// SetDone closes the channel and sets the (optional) error.
func (p *AppendResult) SetDone(err error) {
	p.err = err
	close(p.done)
}

type Config struct {
	// MaxAge is the maximum amount of time a segment can be buffered in memory
	// before it is closed. Increasing MaxAge allows more time for a segment to
	// grow to MaxSegmentSize, but may increase latency if the write volume is
	// low.
	MaxAge time.Duration

	// MaxSegments is the maximum number of segments that can be buffered in
	// memory. Increasing MaxSegments allows for large bursts of writes to be
	// buffered in memory, but may increase latency if the write volume exceeds
	// the rate at which segments can be flushed.
	MaxSegments int64

	// MaxSegmentSize is the maximum size of a segment. It is not a strict
	// limit, and segments can exceed the maximum size if the size of an
	// individual append is larger than the remaining capacity.
	MaxSegmentSize int64
}

// Manager buffers segments in memory, and keeps track of which segments are
// accepting writes and which segments need to be flushed. The number of
// segments that are buffered in memory, their maximum age and maximum size
// are configured when creating the Manager.
//
// By buffering segments in memory, the manager can tolerate bursts of appends
// that arrive faster than they can be flushed. The amount of data that can be
// buffered is configured using MaxSegments and MaxSegmentSize. You must use
// caution when configuring these to avoid excessive latency.
//
// If no segments are accepting writes, because all segments are waiting to be
// flushed, then subsequent appends fail. It will not be possible to append more
// data until an existing segment has been flushed. This allows the manager to
// also apply back pressure to writers to avoid congestion collapse due to high
// latency and retries.
type Manager struct {
	cfg Config

	// available is a list of segments that are accepting writes. When a
	// segment is full, or the close timer has expired, it is moved to the
	// closed list to be flushed.
	available *list.List

	// closed is a list of segments that are full and waiting to be flushed.
	// Once flushed, the segment is moved back to the available list to
	// accept writes again.
	closed   *list.List
	shutdown chan struct{}
	mu       sync.Mutex
}

// item is similar to ClosedWriter, but it is an internal struct used in the
// available and closed lists. It contains a single-use result that is returned
// to callers of Append and a re-usable writer that is reset after each flush.
type item struct {
	r *AppendResult
	w *SegmentWriter
}

// ClosedWriter contains a result and a writer that has either exceeded the
// maximum age or the maximum size for a segment. ClosedWriters are to be
// flushed and then returned so the writer can be re-used.
type ClosedWriter struct {
	Result *AppendResult
	Writer *SegmentWriter
}

func NewManager(cfg Config) (*Manager, error) {
	m := Manager{
		cfg:       cfg,
		available: list.New(),
		closed:    list.New(),
		shutdown:  make(chan struct{}),
	}
	for i := int64(0); i < cfg.MaxSegments; i++ {
		w, err := NewWalSegmentWriter()
		if err != nil {
			return nil, err
		}
		m.available.PushBack(&item{
			r: &AppendResult{done: make(chan struct{})},
			w: w,
		})
	}
	return &m, nil
}

func (m *Manager) Append(r AppendRequest) (*AppendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// When the WAL becomes full it stops accepting writes.
	if m.available.Len() == 0 {
		return nil, ErrFull
	}
	el := m.available.Front()
	it := el.Value.(*item)
	it.w.Append(r.TenantID, r.LabelsStr, r.Labels, r.Entries)
	// If the item exceeded the maximum age or the maximum size, move it to
	// closed to be flushed.
	if it.w.InputSize() >= m.cfg.MaxSegmentSize {
		m.closed.PushBack(it)
		m.available.Remove(el)
	}
	return it.r, nil
}

func (m *Manager) Get() (*ClosedWriter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Len() == 0 {
		return nil, nil
	}
	el := m.closed.Front()
	it := el.Value.(*item)
	m.closed.Remove(el)
	return &ClosedWriter{Result: it.r, Writer: it.w}, nil
}

func (m *Manager) Put(it *ClosedWriter) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it.Writer.Reset()
	m.available.PushBack(&item{
		r: &AppendResult{done: make(chan struct{})},
		w: it.Writer,
	})
	return nil
}

// Run the Manager. This runs a number of periodic tasks.
func (m *Manager) Run() error {
	t := time.NewTicker(m.cfg.MaxAge)
	for {
		select {
		case <-t.C:
			m.doTick()
		case <-m.shutdown:
			return nil
		}
	}
}

// Stop the Manager.
func (m *Manager) Stop() error {
	close(m.shutdown)
	return nil
}

func (m *Manager) doTick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Check if the current segment has exceeded its maximum age, and if so,
	// move it to the closed list.
	if m.available.Len() > 0 {
		el := m.available.Front()
		it := el.Value.(*item)
		if it.w.InputSize() > 0 {
			m.closed.PushBack(it)
			m.available.Remove(el)
		}
	}
}
