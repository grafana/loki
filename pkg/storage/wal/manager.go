package wal

import (
	"container/list"
	"errors"
	"sync"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var (
	// ErrClosed is returned when the WAL is closed. It is a permanent error
	// as once closed, a WAL cannot be re-opened.
	ErrClosed = errors.New("WAL is closed")

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
	// allows more time for a segment to grow to MaxSegmentSize, but may
	// increase latency if appends cannot fill segments quickly enough.
	MaxAge time.Duration

	// MaxSegments is the maximum number of segments that can be buffered in
	// memory. Increasing MaxSegments allows more data to be buffered, but may
	// increase latency if the incoming volume of data exceeds the rate at
	// which segments can be flushed.
	MaxSegments int64

	// MaxSegmentSize is the maximum size of an uncompressed segment in bytes.
	// It is not a strict limit, and segments can exceed the maximum size when
	// individual appends are larger than the remaining capacity.
	MaxSegmentSize int64
}

// Manager is a pool of in-memory segments. It keeps track of which segments
// are accepting writes and which are waiting to be flushed using two doubly
// linked lists called the available and pending lists.
//
// By buffering segments in memory, the WAL can tolerate bursts of writes that
// arrive faster than can be flushed. The amount of data that can be buffered
// is configured using MaxSegments and MaxSegmentSize. You must use caution
// when configuring these to avoid excessive latency.
//
// The WAL is full when all segments are waiting to be flushed or in the
// process of being flushed. When the WAL is full, subsequent appends fail with
// the transient error ErrFull, and will not succeed until one or more other
// segments have been flushed and returned to the available list. Callers
// should back off and retry at a later time.
//
// On shutdown, the WAL must be closed to avoid losing data. This prevents
// additional appends to the WAL and allows all remaining segments to be
// flushed.
type Manager struct {
	cfg       Config
	metrics   *ManagerMetrics
	available *list.List
	pending   *list.List
	closed    bool
	mu        sync.Mutex
	// Used in tests.
	clock quartz.Clock
}

// segment is an internal struct used in the available and pending lists. It
// contains a single-use result that is returned to callers appending to the
// WAL and a re-usable segment that is reset after each flush.
type segment struct {
	r *AppendResult
	w *SegmentWriter
}

// PendingSegment contains a result and the segment to be flushed.
type PendingSegment struct {
	Result *AppendResult
	Writer *SegmentWriter
}

func NewManager(cfg Config, metrics *ManagerMetrics) (*Manager, error) {
	m := Manager{
		cfg:       cfg,
		metrics:   metrics,
		available: list.New(),
		pending:   list.New(),
		clock:     quartz.NewReal(),
	}
	m.metrics.NumAvailable.Set(0)
	m.metrics.NumPending.Set(0)
	m.metrics.NumFlushing.Set(0)
	for i := int64(0); i < cfg.MaxSegments; i++ {
		w, err := NewWalSegmentWriter()
		if err != nil {
			return nil, err
		}
		m.available.PushBack(&segment{
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
	if m.closed {
		return nil, ErrClosed
	}
	el := m.available.Front()
	if el == nil {
		return nil, ErrFull
	}
	s := el.Value.(*segment)
	s.w.Append(r.TenantID, r.LabelsStr, r.Labels, r.Entries, m.clock.Now())
	// If the segment exceeded the maximum age or the maximum size, move s to
	// the closed list to be flushed.
	if s.w.Age(m.clock.Now()) >= m.cfg.MaxAge || s.w.InputSize() >= m.cfg.MaxSegmentSize {
		m.move(el, s)
	}
	return s.r, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if el := m.available.Front(); el != nil {
		s := el.Value.(*segment)
		if s.w.InputSize() > 0 {
			m.move(el, s)
		}
	}
	m.closed = true
}

// NextPending returns the next segment to be flushed from the pending list.
// It returns nil if there are no segments waiting to be flushed. If the WAL
// is closed it returns all remaining segments from the pending list and then
// ErrClosed.
func (m *Manager) NextPending() (*PendingSegment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending.Len() == 0 && !m.moveFrontIfExpired() {
		if m.closed {
			return nil, ErrClosed
		}
		return nil, nil
	}
	el := m.pending.Front()
	s := el.Value.(*segment)
	m.pending.Remove(el)
	m.metrics.NumPending.Dec()
	m.metrics.NumFlushing.Inc()
	return &PendingSegment{Result: s.r, Writer: s.w}, nil
}

// Put resets the segment and puts it back in the available list to accept
// writes. A PendingSegment should not be put back until it has been flushed.
func (m *Manager) Put(s *PendingSegment) {
	s.Writer.Reset()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics.NumFlushing.Dec()
	m.metrics.NumAvailable.Inc()
	m.available.PushBack(&segment{
		r: &AppendResult{done: make(chan struct{})},
		w: s.Writer,
	})
}

// move the element from the available list to the pending list and sets the
// relevant metrics.
func (m *Manager) move(el *list.Element, s *segment) {
	m.pending.PushBack(s)
	m.metrics.NumPending.Inc()
	m.available.Remove(el)
	m.metrics.NumAvailable.Dec()
}

// moveFrontIfExpired moves the element from the front of the available list to
// the pending list if the segment has exceeded its maximum age and sets the
// relevant metrics.
func (m *Manager) moveFrontIfExpired() bool {
	if el := m.available.Front(); el != nil {
		s := el.Value.(*segment)
		if s.w.Age(m.clock.Now()) >= m.cfg.MaxAge {
			m.move(el, s)
			return true
		}
	}
	return false
}
