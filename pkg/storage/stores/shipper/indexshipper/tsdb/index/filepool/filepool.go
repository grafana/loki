// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/util/filepool/filepool.go

package filepool

import (
	"errors"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ErrPoolStopped = errors.New("file handle pool is stopped")

type FilePoolMetrics struct {
	openCount        prometheus.Counter
	pooledOpenCount  prometheus.Counter
	closeCount       prometheus.Counter
	pooledCloseCount prometheus.Counter
}

func NewFilePoolMetrics(reg prometheus.Registerer) *FilePoolMetrics {
	return &FilePoolMetrics{
		openCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "file_handle_unpooled_open_total",
			Help: "Total number of times index-header file has been opened instead of using a pooled handle.",
		}),
		pooledOpenCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "file_handle_pooled_open_total",
			Help: "Total number of times a pooled index-header file handle has been used instead of opened.",
		}),
		closeCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "file_handle_unpooled_close_total",
			Help: "Total number of times index-header file has been closed instead of returning the handle to the pool.",
		}),
		pooledCloseCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "file_handle_pooled_close_total",
			Help: "Total number of times pooled index-header file handle has been returned to the pool instead of closed.",
		}),
	}
}

type FilePoolCloser interface {
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
type FilePool struct {
	path    string
	handles chan *os.File
	mtx     sync.RWMutex
	stopped bool

	metrics *FilePoolMetrics
}

// NewFilePool creates a new file pool for path with cap capacity. If cap is 0,
// Get always opens new file handles and Put always closes them immediately.
func NewFilePool(path string, cap uint, metrics *FilePoolMetrics) *FilePool {
	return &FilePool{
		path: path,
		// We don't care if cap is 0 which means the channel will be unbuffered. Because
		// we have default cases for reads and writes to the channel, we will always open
		// new files and close file handles immediately if the channel is unbuffered.
		handles: make(chan *os.File, cap),
		metrics: metrics,
	}
}

// Get returns a pooled file handle if available or opens a new one
// are no pooled handles available. If this pool has been stopped, an error
// is returned.
func (p *FilePool) Get() (*os.File, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	if p.stopped {
		return nil, ErrPoolStopped
	}

	select {
	case f := <-p.handles:
		p.metrics.pooledOpenCount.Inc()
		return f, nil
	default:
		p.metrics.openCount.Inc()
		return os.Open(p.path)
	}
}

// Put returns a file handle to the pool if there is space available or closes
// the file handle if there is not. If this pool has been stopped, the file handle
// is closed immediately.
func (p *FilePool) Put(f *os.File) error {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	if p.stopped {
		return f.Close()
	}

	select {
	case p.handles <- f:
		p.metrics.pooledCloseCount.Inc()
		return nil
	default:
		p.metrics.closeCount.Inc()
		return f.Close()
	}
}

// Stop closes all pooled file handles. After this method is called, subsequent
// Get calls will return an error and Put calls will immediately close the file
// handle.
func (p *FilePool) Stop() {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.stopped = true

	for {
		select {
		case f := <-p.handles:
			_ = f.Close()
		default:
			return
		}
	}
}
