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
	DefaultMaxAge         = 500 * time.Millisecond
	DefaultMaxSegments    = 10
	DefaultMaxSegmentSize = 8 * 1024 * 1024 // 8MB.
)

var (
	// ErrFull is returned when the WAL is full. It is a transient error that
	// happens when all segments are either in the pending list waiting to be
	// flushed or in the process of being flushed.
	ErrFull = errors.New("WAL is full")
)

type AppendRequest struct {
	TenantID  string
	Labels    labels.Labels
	LabelsStr string
	Entries   []*logproto.Entry
}

// AppendResult contains the result of an AppendRequest.
type AppendResult struct {
	done chan struct{}
	err  error
}

// Done returns a channel that is closed when the result for the AppendRequest
// is available. Use Err() to check if the request succeeded or failed.
func (p *AppendResult) Done() <-chan struct{} {
	return p.done
}

// Err returns a non-nil error if the append request failed, and nil if it
// succeeded. It should not be called until Done() is closed to avoid data
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
	// allows more time for a segment to grow to MaxSegmentSize, but increases
	// latency when the write volume cannot fill segments quickly enough.
	MaxAge time.Duration

	// MaxSegments is the maximum number of segments that can be buffered in
	// memory. Increasing MaxSegments allows for large bursts of writes to be
	// buffered in memory, but may increase latency if the write volume exceeds
	// the rate at which segments can be flushed.
	MaxSegments int64

	// MaxSegmentSize is the maximum size of an uncompressed segment in bytes.
	// It is not a strict limit, and segments can exceed the maximum size when
	// individual appends are larger than the remaining capacity.
	MaxSegmentSize int64

	// RunPeriodic is the interval to run periodic tasks, such as checking if
	// the current segment has exceeded the maximum age.
	RunPeriodic time.Duration
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
// accepting writes and which are waiting to be flushed. It uses two doubly
// linked lists called the available and pending lists, and as such a Manager
// has constant time complexity.
//
// The maximum number of segments that can be buffered in memory, and their
// maximum age and maximum size before being flushed are configured when
// creating a Manager. When a segment exceeds the maximum age or maximum size
// it is moved to the pending list.
//
// By buffering segments in memory, the WAL can tolerate bursts of writes that
// arrive faster than can be flushed. The amount of data that can be buffered
// is configured using MaxSegments and MaxSegmentSize. You must use caution
// when configuring these to avoid excessive latency.
//
// The WAL is full when all segments are waiting to be flushed or in the
// process of being flushed. When the WAL is full, subsequent appends fail with
// ErrFull. It is not permitted to append more data until another segment has
// been flushed and returned to the available list. This allows the manager to
// apply back-pressure to clients that may be timing out and retrying.
type Manager struct {
	cfg     Config
	metrics *Metrics

	// available is a list of segments that are accepting writes. All segments
	// other than the segment at the front of the list are empty, and only the
	// segment at the front of the list is written to. When this segment has
	// exceeded its maximum age or maximum size it is moved to the pending list
	// to be flushed, and the next segment in the available list takes its place.
	available *list.List

	// firstAppend is the time of the first append to the segment at the
	// front of the available list. It is used to know when the segment has
	// exceeded the maximum age and should be moved to the pending list.
	// It is reset each time.
	firstAppend time.Time

	// pending is a list of segments that are waiting to be flushed. Once
	// flushed, the segment is reset and moved to the back of the available
	// list to accept writes again.
	pending *list.List

	sig  chan struct{}
	quit chan struct{}
	mu   sync.Mutex
}

// item is similar to PendingItem, but it is an internal struct used in the
// available and pending lists. It contains a single-use result that is returned
// to callers of Manager.Append() and a re-usable segment that is reset after
// each flush.
type item struct {
	r *AppendResult
	w *SegmentWriter
}

// PendingItem contains a result and the segment to be flushed.
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
		sig:       make(chan struct{}, cfg.MaxSegments),
		quit:      make(chan struct{}),
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
	if m.firstAppend.IsZero() {
		// This is the first append to the segment. This time will be used to
		// know when the segment has exceeded its maximum age and should be
		// moved to the pending list.
		m.firstAppend = time.Now()
	}
	it.w.Append(r.TenantID, r.LabelsStr, r.Labels, r.Entries)
	// If the segment exceeded the maximum age or maximum size, move it to the
	// closed list to be flushed.
	if time.Since(m.firstAppend) >= m.cfg.MaxAge || it.w.InputSize() >= m.cfg.MaxSegmentSize {
		m.firstAppend = time.Time{}
		m.pending.PushBack(it)
		m.metrics.NumPending.Inc()
		m.available.Remove(el)
		m.metrics.NumAvailable.Dec()
		m.sig <- struct{}{}
	}
	return it.r, nil
}

// Iter receives a message when there is a segment in the pending list waiting
// to be flushed. A closed channel guarantees that there will be no more
// segments to flush.
func (m *Manager) Iter() <-chan struct{} {
	return m.sig
}

// NextPending returns the next segment to be flushed from the pending list. It
// returns nil if the list is empty.
func (m *Manager) NextPending() *PendingItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending.Len() == 0 {
		return nil
	}
	el := m.pending.Front()
	it := el.Value.(*item)
	m.pending.Remove(el)
	m.metrics.NumPending.Dec()
	m.metrics.NumFlushing.Inc()
	return &PendingItem{Result: it.r, Writer: it.w}
}

// Put resets the segment and puts it back in the available list to accept
// writes. A PendingItem should not be put back until it has been flushed.
func (m *Manager) Put(it *PendingItem) {
	it.Writer.Reset()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics.NumFlushing.Dec()
	m.metrics.NumAvailable.Inc()
	m.available.PushBack(&item{
		r: &AppendResult{done: make(chan struct{})},
		w: it.Writer,
	})
}

// Runs periodic tasks every Config.RunPeriodic duration.
func (m *Manager) Run() {
	t := time.NewTicker(m.cfg.RunPeriodic)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			m.doPeriodicTasks()
		case <-m.quit:
			return
		}
	}
}

// Stop the Manager.
func (m *Manager) Stop() {
	close(m.sig)
	close(m.quit)
}

func (m *Manager) doPeriodicTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.moveIfExceededMaxAge()
}

// moveIfExceededMaxAge moves the segment at the front of the available list
// to the pending list if it has been written to and exceeded the maximum age
// of a segment.
func (m *Manager) moveIfExceededMaxAge() {
	if m.available.Len() > 0 {
		el := m.available.Front()
		it := el.Value.(*item)
		// Need to make sure firstAppend is non-zero otherwise it move segments
		// that have not had any writes.
		if !m.firstAppend.IsZero() && time.Since(m.firstAppend) >= m.cfg.MaxAge {
			m.firstAppend = time.Time{}
			m.pending.PushBack(it)
			m.metrics.NumPending.Inc()
			m.available.Remove(el)
			m.metrics.NumAvailable.Dec()
			m.sig <- struct{}{}
		}
	}
}
