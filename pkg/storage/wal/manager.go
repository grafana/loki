package wal

import (
	"container/list"
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	// DefaultMaxAge is the default value for the maximum amount of time a
	// segment can can be buffered in memory before it should be flushed.
	DefaultMaxAge = 500 * time.Millisecond
	// DefaultMaxSegments is the default value for the maximum number of
	// segments that can be buffered in memory, including segments waiting to
	// be flushed.
	DefaultMaxSegments = 10
	// DefaultMaxSegmentSize is the default value for the maximum segment size
	// (uncompressed).
	DefaultMaxSegmentSize = 8 * 1024 * 1024 // 8MB.
)

var (
	// ErrFull is returned when an append fails because the WAL is full. This
	// happens when all segments are either in the pending list waiting to be
	// flushed, or in the process of being flushed.
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

// Done returns a channel that is closed when the result of an append is
// available. Err() should be called to check if the operation was successful.
func (p *AppendResult) Done() <-chan struct{} {
	return p.done
}

// Err returns a non-nil error if the operation failed, and nil if it was
// successful. It should not be called until Done() is closed to avoid data
// races.
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
	// before it is moved to the pending list to be flushed. Increasing MaxAge
	// allows more time for a segment to grow to MaxSegmentSize, but may increase
	// latency if the write volume is too small.
	MaxAge time.Duration

	// MaxSegments is the maximum number of segments that can be buffered in
	// memory. Increasing MaxSegments allows for large bursts of writes to be
	// buffered in memory, but may increase latency if the write volume exceeds
	// the rate at which segments can be flushed.
	MaxSegments int64

	// MaxSegmentSize is the maximum size (uncompressed) of a segment. It is
	// not a strict limit, and segments can exceed the maximum size when
	// individual appends are larger than the remaining capacity.
	MaxSegmentSize int64
}

type Metrics struct {
	NumAvailable prometheus.Gauge
	NumPending   prometheus.Gauge
	NumFlushing  prometheus.Gauge
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		NumAvailable: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_available",
			Help: "The number of WAL segments accepting writes.",
		}),
		NumPending: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_pending",
			Help: "The number of WAL segments waiting to be flushed.",
		}),
		NumFlushing: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_flushing",
			Help: "The number of WAL segments being flushed.",
		}),
	}
}

// Manager buffers segments in memory, and keeps track of which segments are
// available and which are waiting to be flushed. The maximum number of
// segments that can be buffered in memory, and their maximum age and maximum
// size before being flushed are configured when creating the Manager.
//
// By buffering segments in memory, the WAL can tolerate bursts of append
// requests that arrive faster than can be flushed. The amount of data that can
// be buffered is configured using MaxSegments and MaxSegmentSize. You must use
// caution when configuring these to avoid excessive latency.
//
// The WAL is full when all segments are waiting to be flushed or in the process
// of being flushed. When the WAL is full, subsequent appends fail with ErrFull.
// It is not permitted to append more data until another segment has been flushed
// and returned to the available list. This allows the manager to apply back-pressure
// and avoid congestion collapse due to excessive timeouts and retries.
type Manager struct {
	cfg     Config
	metrics *Metrics

	// available is a list of segments that are available and accepting data.
	// All segments other than the segment at the front of the list are empty,
	// and only the segment at the front of the list is written to. When this
	// segment has exceeded its maximum age or maximum size it is moved to the
	// pending list to be flushed, and the next segment in the available list
	// takes its place.
	available *list.List

	// pending is a list of segments that are waiting to be flushed. Once
	// flushed, the segment is reset and moved to the back of the available
	// list to accept writes again.
	pending  *list.List
	shutdown chan struct{}
	mu       sync.Mutex
}

// item is similar to PendingItem, but it is an internal struct used in the
// available and pending lists. It contains a single-use result that is returned
// to callers of Manager.Append() and a re-usable segment that is reset after
// each flush.
type item struct {
	r *AppendResult
	w *SegmentWriter
	// firstAppendedAt is the time of the first append to the segment, and is
	// used to know when the segment has exceeded the maximum age and should
	// be moved to the pending list. It is reset after each flush.
	firstAppendedAt time.Time
}

// PendingItem contains a result and the segment to be flushed. ClosedWriters
// are to be returned following a flush so the segment can be re-used.
type PendingItem struct {
	Result *AppendResult
	Writer *SegmentWriter
}

func NewManager(cfg Config, metrics *Metrics) (*Manager, error) {
	m := Manager{
		cfg:       cfg,
		metrics:   metrics,
		available: list.New(),
		pending:   list.New(),
		shutdown:  make(chan struct{}),
	}
	m.metrics.NumPending.Set(0)
	m.metrics.NumFlushing.Set(0)
	for i := int64(0); i < cfg.MaxSegments; i++ {
		w, err := NewWalSegmentWriter()
		if err != nil {
			return nil, err
		}
		m.available.PushBack(&item{
			r: &AppendResult{done: make(chan struct{})},
			w: w,
		})
		m.metrics.NumAvailable.Inc()
	}
	return &m, nil
}

func (m *Manager) Append(r AppendRequest) (*AppendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.available.Len() == 0 {
		return nil, ErrFull
	}
	el := m.available.Front()
	it := el.Value.(*item)
	if it.firstAppendedAt.IsZero() {
		it.firstAppendedAt = time.Now()
	}
	it.w.Append(r.TenantID, r.LabelsStr, r.Labels, r.Entries)
	// If the segment exceeded the maximum age or the maximum size, move it to
	// the closed list to be flushed.
	if time.Since(it.firstAppendedAt) >= m.cfg.MaxAge || it.w.InputSize() >= m.cfg.MaxSegmentSize {
		m.pending.PushBack(it)
		m.metrics.NumPending.Inc()
		m.available.Remove(el)
		m.metrics.NumAvailable.Dec()
	}
	return it.r, nil
}

// NextPending returns the next segment to be flushed. It returns nil if the
// pending list is empty.
func (m *Manager) NextPending() (*PendingItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending.Len() == 0 {
		if m.available.Len() > 0 {
			// Check if the current segment has exceeded its maximum age and
			// should be moved to the pending list.
			el := m.available.Front()
			it := el.Value.(*item)
			if !it.firstAppendedAt.IsZero() && time.Since(it.firstAppendedAt) >= m.cfg.MaxAge {
				m.pending.PushBack(it)
				m.metrics.NumPending.Inc()
				m.available.Remove(el)
				m.metrics.NumAvailable.Dec()
			}
		}
		// If the pending list is still empty return nil.
		if m.pending.Len() == 0 {
			return nil, nil
		}
	}
	el := m.pending.Front()
	it := el.Value.(*item)
	m.pending.Remove(el)
	m.metrics.NumPending.Dec()
	m.metrics.NumFlushing.Inc()
	return &PendingItem{Result: it.r, Writer: it.w}, nil
}

// Put resets the segment and puts it back in the available list to accept
// writes. A PendingItem should not be put back until it has been flushed.
func (m *Manager) Put(it *PendingItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it.Writer.Reset()
	m.metrics.NumFlushing.Dec()
	m.metrics.NumAvailable.Inc()
	m.available.PushBack(&item{
		r: &AppendResult{done: make(chan struct{})},
		w: it.Writer,
	})
	return nil
}
