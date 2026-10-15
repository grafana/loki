// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/util/filepool/filepool.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package filepool

import (
	"errors"
	"math/rand/v2"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
)

var ErrPoolStopped = errors.New("file handle pool is stopped")

type FilePoolMetrics struct { // nolint:revive
	openCount        prometheus.Counter
	pooledOpenCount  prometheus.Counter
	closeCount       prometheus.Counter
	pooledCloseCount prometheus.Counter
}

func NewFilePoolMetrics(reg prometheus.Registerer) *FilePoolMetrics {
	return &FilePoolMetrics{
		openCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_index_file_handle_unpooled_open_total",
			Help: "Total number of times a TSDB index file has been opened instead of using a pooled handle.",
		}),
		pooledOpenCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_index_file_handle_pooled_open_total",
			Help: "Total number of times a pooled TSDB index file handle has been used instead of opened.",
		}),
		closeCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_index_file_handle_unpooled_close_total",
			Help: "Total number of times a TSDB index file has been closed instead of returning the handle to the pool.",
		}),
		pooledCloseCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_tsdb_index_file_handle_pooled_close_total",
			Help: "Total number of times a pooled TSDB index file handle has been returned to the pool instead of closed.",
		}),
	}
}

type FilePoolCloser interface { // nolint:revive
	Put(*os.File) error
}

// SingleFilePoolNoopCloser implements FilePoolCloser without closing the file.
// Some operations may call the pool closer on a file handle upon completion,
// as they assume the file handle was originally retrieved from a FilePool.
// This type allows calling those operations an unpooled file handle,
// which can be closed manually according to its own lifecycle.
type SingleFilePoolNoopCloser struct{}

func (c SingleFilePoolNoopCloser) Put(_ *os.File) error {
	return nil
}

// FilePool maintains a pool of file handles up to a maximum number, creating
// new handles and closing them when required. Get and Put operations on this
// pool never block. If there are no available file handles, one will be created
// on Get. If the pool is full, the file handle is closed on Put.
//
// The pool is lock-free: idle file handles live in a fixed array of slots,
// with atomic Swap and CompareAndSwap operations used to Get and Put file
// handles into slots.
// Each scan for an available handle, or empty slot starts at a random index
// to reduce contention.
type FilePool struct {
	path    string
	slots   []atomic.Pointer[os.File]
	stopped atomic.Bool

	metrics *FilePoolMetrics
}

// NewFilePool creates a new file pool for path with capacity capacity. If capacity is 0,
// Get always opens new file handles and Put always closes them immediately.
func NewFilePool(path string, capacity uint, metrics *FilePoolMetrics) *FilePool {
	return &FilePool{
		path:    path,
		slots:   make([]atomic.Pointer[os.File], capacity),
		metrics: metrics,
	}
}

// Get returns a pooled file handle if available or opens a new one if there
// are no pooled handles available. If this pool has been stopped, an error
// is returned.
func (p *FilePool) Get() (*os.File, error) {
	if p.stopped.Load() {
		return nil, ErrPoolStopped
	}

	if n := len(p.slots); n > 0 {
		start := rand.IntN(n)
		for i := range n {
			s := &p.slots[(start+i)%n]
			if s.Load() == nil {
				continue
			}
			if f := s.Swap(nil); f != nil {
				p.metrics.pooledOpenCount.Inc()
				return f, nil
			}
		}
	}

	p.metrics.openCount.Inc()
	return os.Open(p.path)
}

// Put returns a file handle to the pool if there is space available or closes
// the file handle if there is not. If this pool has been stopped, the file handle
// is closed immediately.
func (p *FilePool) Put(f *os.File) error {
	if p.stopped.Load() {
		return f.Close()
	}

	if n := len(p.slots); n > 0 {
		start := rand.IntN(n)
		for i := range n {
			s := &p.slots[(start+i)%n]
			if s.Load() != nil {
				continue
			}
			if !s.CompareAndSwap(nil, f) {
				continue
			}
			// Stop may have run since we checked.
			// If the pool is now stopped, try to reclaim the handle and close it.
			// If we fail, then Stop beat us to it.
			if p.stopped.Load() && s.CompareAndSwap(f, nil) {
				return f.Close()
			}
			p.metrics.pooledCloseCount.Inc()
			return nil
		}
	}

	p.metrics.closeCount.Inc()
	return f.Close()
}

// Stop closes all pooled file handles. After this method is called, subsequent
// Get calls will return an error and Put calls will immediately close the file
// handle.
func (p *FilePool) Stop() {
	p.stopped.Store(true)

	for i := range p.slots {
		if f := p.slots[i].Swap(nil); f != nil {
			_ = f.Close()
		}
	}
}
