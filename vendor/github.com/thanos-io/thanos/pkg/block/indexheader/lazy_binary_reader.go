// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package indexheader

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/tsdb/index"
	"go.uber.org/atomic"

	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// LazyBinaryReaderMetrics holds metrics tracked by LazyBinaryReader.
type LazyBinaryReaderMetrics struct {
	loadCount         prometheus.Counter
	loadFailedCount   prometheus.Counter
	unloadCount       prometheus.Counter
	unloadFailedCount prometheus.Counter
	loadDuration      prometheus.Histogram
}

// NewLazyBinaryReaderMetrics makes new LazyBinaryReaderMetrics.
func NewLazyBinaryReaderMetrics(reg prometheus.Registerer) *LazyBinaryReaderMetrics {
	return &LazyBinaryReaderMetrics{
		loadCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "indexheader_lazy_load_total",
			Help: "Total number of index-header lazy load operations.",
		}),
		loadFailedCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "indexheader_lazy_load_failed_total",
			Help: "Total number of failed index-header lazy load operations.",
		}),
		unloadCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "indexheader_lazy_unload_total",
			Help: "Total number of index-header lazy unload operations.",
		}),
		unloadFailedCount: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "indexheader_lazy_unload_failed_total",
			Help: "Total number of failed index-header lazy unload operations.",
		}),
		loadDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "indexheader_lazy_load_duration_seconds",
			Help:    "Duration of the index-header lazy loading in seconds.",
			Buckets: []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5},
		}),
	}
}

// LazyBinaryReader wraps BinaryReader and loads (mmap) the index-header only upon
// the first Reader function is called.
type LazyBinaryReader struct {
	ctx                         context.Context
	logger                      log.Logger
	bkt                         objstore.BucketReader
	dir                         string
	filepath                    string
	id                          ulid.ULID
	postingOffsetsInMemSampling int
	metrics                     *LazyBinaryReaderMetrics
	onClosed                    func(*LazyBinaryReader)

	readerMx  sync.RWMutex
	reader    *BinaryReader
	readerErr error

	// Keep track of the last time it was used.
	usedAt *atomic.Int64
}

// NewLazyBinaryReader makes a new LazyBinaryReader. If the index-header does not exist
// on the local disk at dir location, this function will build it downloading required
// sections from the full index stored in the bucket. However, this function doesn't load
// (mmap) the index-header; it will be loaded at first Reader function call.
func NewLazyBinaryReader(
	ctx context.Context,
	logger log.Logger,
	bkt objstore.BucketReader,
	dir string,
	id ulid.ULID,
	postingOffsetsInMemSampling int,
	metrics *LazyBinaryReaderMetrics,
	onClosed func(*LazyBinaryReader),
) (*LazyBinaryReader, error) {
	filepath := filepath.Join(dir, id.String(), block.IndexHeaderFilename)

	// If the index-header doesn't exist we should download it.
	if _, err := os.Stat(filepath); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "read index header")
		}

		level.Debug(logger).Log("msg", "the index-header doesn't exist on disk; recreating", "path", filepath)

		start := time.Now()
		if err := WriteBinary(ctx, bkt, id, filepath); err != nil {
			return nil, errors.Wrap(err, "write index header")
		}

		level.Debug(logger).Log("msg", "built index-header file", "path", filepath, "elapsed", time.Since(start))
	}

	return &LazyBinaryReader{
		ctx:                         ctx,
		logger:                      logger,
		bkt:                         bkt,
		dir:                         dir,
		filepath:                    filepath,
		id:                          id,
		postingOffsetsInMemSampling: postingOffsetsInMemSampling,
		metrics:                     metrics,
		usedAt:                      atomic.NewInt64(time.Now().UnixNano()),
		onClosed:                    onClosed,
	}, nil
}

// Close implements Reader. It unloads the index-header from memory (releasing the mmap
// area), but a subsequent call to any other Reader function will automatically reload it.
func (r *LazyBinaryReader) Close() error {
	if r.onClosed != nil {
		defer r.onClosed(r)
	}

	return r.unload()
}

// IndexVersion implements Reader.
func (r *LazyBinaryReader) IndexVersion() (int, error) {
	r.readerMx.RLock()
	defer r.readerMx.RUnlock()

	if err := r.load(); err != nil {
		return 0, err
	}

	r.usedAt.Store(time.Now().UnixNano())
	return r.reader.IndexVersion()
}

// PostingsOffset implements Reader.
func (r *LazyBinaryReader) PostingsOffset(name string, value string) (index.Range, error) {
	r.readerMx.RLock()
	defer r.readerMx.RUnlock()

	if err := r.load(); err != nil {
		return index.Range{}, err
	}

	r.usedAt.Store(time.Now().UnixNano())
	return r.reader.PostingsOffset(name, value)
}

// LookupSymbol implements Reader.
func (r *LazyBinaryReader) LookupSymbol(o uint32) (string, error) {
	r.readerMx.RLock()
	defer r.readerMx.RUnlock()

	if err := r.load(); err != nil {
		return "", err
	}

	r.usedAt.Store(time.Now().UnixNano())
	return r.reader.LookupSymbol(o)
}

// LabelValues implements Reader.
func (r *LazyBinaryReader) LabelValues(name string) ([]string, error) {
	r.readerMx.RLock()
	defer r.readerMx.RUnlock()

	if err := r.load(); err != nil {
		return nil, err
	}

	r.usedAt.Store(time.Now().UnixNano())
	return r.reader.LabelValues(name)
}

// LabelNames implements Reader.
func (r *LazyBinaryReader) LabelNames() ([]string, error) {
	r.readerMx.RLock()
	defer r.readerMx.RUnlock()

	if err := r.load(); err != nil {
		return nil, err
	}

	r.usedAt.Store(time.Now().UnixNano())
	return r.reader.LabelNames()
}

// load ensures the underlying binary index-header reader has been successfully loaded. Returns
// an error on failure. This function MUST be called with the read lock already acquired.
func (r *LazyBinaryReader) load() error {
	// Nothing to do if we already tried loading it.
	if r.reader != nil {
		return nil
	}
	if r.readerErr != nil {
		return r.readerErr
	}

	// Take the write lock to ensure we'll try to load it only once. Take again
	// the read lock once done.
	r.readerMx.RUnlock()
	r.readerMx.Lock()
	defer r.readerMx.RLock()
	defer r.readerMx.Unlock()

	// Ensure none else tried to load it in the meanwhile.
	if r.reader != nil {
		return nil
	}
	if r.readerErr != nil {
		return r.readerErr
	}

	level.Debug(r.logger).Log("msg", "lazy loading index-header file", "path", r.filepath)
	r.metrics.loadCount.Inc()
	startTime := time.Now()

	reader, err := NewBinaryReader(r.ctx, r.logger, r.bkt, r.dir, r.id, r.postingOffsetsInMemSampling)
	if err != nil {
		r.metrics.loadFailedCount.Inc()
		r.readerErr = err
		return errors.Wrapf(err, "lazy load index-header file at %s", r.filepath)
	}

	r.reader = reader
	level.Debug(r.logger).Log("msg", "lazy loaded index-header file", "path", r.filepath, "elapsed", time.Since(startTime))
	r.metrics.loadDuration.Observe(time.Since(startTime).Seconds())

	return nil
}

// unload closes underlying BinaryReader. Calling this function on a already unloaded reader is a no-op.
func (r *LazyBinaryReader) unload() error {
	// Always update the used timestamp so that the pool will not call unload() again until the next
	// idle timeout is hit.
	r.usedAt.Store(time.Now().UnixNano())

	r.readerMx.Lock()
	defer r.readerMx.Unlock()

	if r.reader == nil {
		return nil
	}

	r.metrics.unloadCount.Inc()
	if err := r.reader.Close(); err != nil {
		r.metrics.unloadFailedCount.Inc()
		return err
	}

	r.reader = nil
	return nil
}

func (r *LazyBinaryReader) lastUsedAt() int64 {
	return r.usedAt.Load()
}
