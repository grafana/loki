// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package indexheader

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/thanos-io/thanos/pkg/objstore"
)

// ReaderPool is used to istantiate new index-header readers and keep track of them.
// When the lazy reader is enabled, the pool keeps track of all instantiated readers
// and automatically close them once the idle timeout is reached. A closed lazy reader
// will be automatically re-opened upon next usage.
type ReaderPool struct {
	lazyReaderEnabled     bool
	lazyReaderIdleTimeout time.Duration
	lazyReaderMetrics     *LazyBinaryReaderMetrics
	logger                log.Logger

	// Channel used to signal once the pool is closing.
	close chan struct{}

	// Keep track of all readers managed by the pool.
	lazyReadersMx sync.Mutex
	lazyReaders   map[*LazyBinaryReader]struct{}
}

// NewReaderPool makes a new ReaderPool.
func NewReaderPool(logger log.Logger, lazyReaderEnabled bool, lazyReaderIdleTimeout time.Duration, reg prometheus.Registerer) *ReaderPool {
	p := &ReaderPool{
		logger:                logger,
		lazyReaderEnabled:     lazyReaderEnabled,
		lazyReaderIdleTimeout: lazyReaderIdleTimeout,
		lazyReaderMetrics:     NewLazyBinaryReaderMetrics(reg),
		lazyReaders:           make(map[*LazyBinaryReader]struct{}),
		close:                 make(chan struct{}),
	}

	// Start a goroutine to close idle readers (only if required).
	if p.lazyReaderEnabled && p.lazyReaderIdleTimeout > 0 {
		checkFreq := p.lazyReaderIdleTimeout / 10

		go func() {
			for {
				select {
				case <-p.close:
					return
				case <-time.After(checkFreq):
					p.closeIdleReaders()
				}
			}
		}()
	}

	return p
}

// NewBinaryReader creates and returns a new binary reader. If the pool has been configured
// with lazy reader enabled, this function will return a lazy reader. The returned lazy reader
// is tracked by the pool and automatically closed once the idle timeout expires.
func (p *ReaderPool) NewBinaryReader(ctx context.Context, logger log.Logger, bkt objstore.BucketReader, dir string, id ulid.ULID, postingOffsetsInMemSampling int) (Reader, error) {
	var reader Reader
	var err error

	if p.lazyReaderEnabled {
		reader, err = NewLazyBinaryReader(ctx, logger, bkt, dir, id, postingOffsetsInMemSampling, p.lazyReaderMetrics, p.onLazyReaderClosed)
	} else {
		reader, err = NewBinaryReader(ctx, logger, bkt, dir, id, postingOffsetsInMemSampling)
	}

	if err != nil {
		return nil, err
	}

	// Keep track of lazy readers only if required.
	if p.lazyReaderEnabled && p.lazyReaderIdleTimeout > 0 {
		p.lazyReadersMx.Lock()
		p.lazyReaders[reader.(*LazyBinaryReader)] = struct{}{}
		p.lazyReadersMx.Unlock()
	}

	return reader, err
}

// Close the pool and stop checking for idle readers. No reader tracked by this pool
// will be closed. It's the caller responsibility to close readers.
func (p *ReaderPool) Close() {
	close(p.close)
}

func (p *ReaderPool) closeIdleReaders() {
	for _, r := range p.getIdleReaders() {
		// Closing an already closed reader is a no-op, so we close it and just update
		// the last timestamp on success. If it will be still be idle the next time this
		// function is called, we'll try to close it again and will just be a no-op.
		//
		// Due to concurrency, the current implementation may close a reader which was
		// use between when the list of idle readers has been computed and now. This is
		// an edge case we're willing to accept, to not further complicate the logic.
		if err := r.unload(); err != nil {
			level.Warn(p.logger).Log("msg", "failed to close idle index-header reader", "err", err)
		}
	}
}

func (p *ReaderPool) getIdleReaders() []*LazyBinaryReader {
	p.lazyReadersMx.Lock()
	defer p.lazyReadersMx.Unlock()

	var idle []*LazyBinaryReader
	threshold := time.Now().Add(-p.lazyReaderIdleTimeout).UnixNano()

	for r := range p.lazyReaders {
		if r.lastUsedAt() < threshold {
			idle = append(idle, r)
		}
	}

	return idle
}

func (p *ReaderPool) isTracking(r *LazyBinaryReader) bool {
	p.lazyReadersMx.Lock()
	defer p.lazyReadersMx.Unlock()

	_, ok := p.lazyReaders[r]
	return ok
}

func (p *ReaderPool) onLazyReaderClosed(r *LazyBinaryReader) {
	p.lazyReadersMx.Lock()
	defer p.lazyReadersMx.Unlock()

	// When this function is called, it means the reader has been closed NOT because was idle
	// but because the consumer closed it. By contract, a reader closed by the consumer can't
	// be used anymore, so we can automatically remove it from the pool.
	delete(p.lazyReaders, r)
}
