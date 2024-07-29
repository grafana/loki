package wal

import (
	"bytes"
	"context"
	"sort"
	"sync"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
)

const batchSize = 16

var _ iter.EntryIterator = (*lazyChunks)(nil)

type lazyChunk struct {
	meta   *chunks.Meta
	labels labels.Labels
	id     string
}

func newLazyChunk(id string, lbs *labels.ScratchBuilder, meta *chunks.Meta) lazyChunk {
	lbs.Sort()
	return lazyChunk{
		id:     id,
		meta:   meta,
		labels: lbs.Labels(),
	}
}

type lazyChunks struct {
	chunks     []lazyChunk
	direction  logproto.Direction
	pipeline   log.Pipeline
	minT, maxT int64
	storage    BlockStorage
	ctx        context.Context

	current iter.EntryIterator
	batch   []lazyChunk
	err     error
}

// todo: Support SampleIterator.
func NewChunksEntryIterator(
	ctx context.Context,
	storage BlockStorage,
	chunks []lazyChunk,
	pipeline log.Pipeline,
	direction logproto.Direction,
	minT, maxT int64,
) *lazyChunks {
	// sort by time and then by labels following the direction.
	sort.Slice(chunks, func(i, j int) bool {
		if direction == logproto.FORWARD {
			t1, t2 := chunks[i].meta.MinTime, chunks[j].meta.MinTime
			if t1 != t2 {
				return t1 < t2
			}
			return labels.Compare(chunks[i].labels, chunks[j].labels) < 0
		}
		t1, t2 := chunks[i].meta.MaxTime, chunks[j].meta.MaxTime
		if t1 != t2 {
			return t1 > t2
		}
		return labels.Compare(chunks[i].labels, chunks[j].labels) < 0
	})
	return &lazyChunks{
		ctx:       ctx,
		chunks:    chunks,
		direction: direction,
		pipeline:  pipeline,
		storage:   storage,
		batch:     make([]lazyChunk, 0, batchSize),
		minT:      minT,
		maxT:      maxT,
	}
}

// At implements iter.EntryIterator.
func (l *lazyChunks) At() push.Entry {
	if l.current == nil {
		return push.Entry{}
	}
	return l.current.At()
}

func (l *lazyChunks) Labels() string {
	if l.current == nil {
		return ""
	}
	return l.current.Labels()
}

func (l *lazyChunks) StreamHash() uint64 {
	if l.current == nil {
		return 0
	}
	return l.current.StreamHash()
}

// Close implements iter.EntryIterator.
func (l *lazyChunks) Close() error {
	if l.current == nil {
		return nil
	}
	return l.current.Close()
}

// Err implements iter.EntryIterator.
func (l *lazyChunks) Err() error {
	if l.err != nil {
		return l.err
	}
	if l.current == nil {
		return l.current.Err()
	}
	return nil
}

// Next implements iter.EntryIterator.
func (l *lazyChunks) Next() bool {
	if l.current != nil && l.current.Next() {
		return true
	}
	if l.current != nil {
		if err := l.current.Close(); err != nil {
			l.err = err
			return false
		}
	}
	if len(l.chunks) == 0 {
		return false
	}
	// take the next batch of chunks
	if err := l.nextBatch(); err != nil {
		l.err = err
		return false
	}
	return l.current.Next()
}

func (l *lazyChunks) nextBatch() error {
	l.batch = l.batch[:0]
	for len(l.chunks) > 0 &&
		(len(l.batch) < batchSize ||
			isOverlapping(l.batch[len(l.batch)-1], l.chunks[0], l.direction)) {
		l.batch = append(l.batch, l.chunks[0])
		l.chunks = l.chunks[1:]
	}
	// todo: error if the batch is too big.
	// todo: reuse previous sortIterator array
	// todo: Try to use iter.NonOverlappingEntryIterator if possible which can reduce the amount of work.
	var (
		iters []iter.EntryIterator
		mtx   sync.Mutex
	)
	g, ctx := errgroup.WithContext(l.ctx)
	g.SetLimit(64)
	for _, c := range l.batch {
		c := c
		g.Go(func() error {
			iter, err := fetchChunkEntries(ctx, c, l.minT, l.maxT, l.direction, l.pipeline, l.storage)
			if err != nil {
				return err
			}
			mtx.Lock()
			iters = append(iters, iter)
			mtx.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	l.current = iter.NewSortEntryIterator(iters, l.direction)
	return nil
}

func fetchChunkEntries(
	ctx context.Context,
	c lazyChunk,
	from, through int64,
	direction logproto.Direction,
	pipeline log.Pipeline,
	storage BlockStorage,
) (iter.EntryIterator, error) {
	offset, size := c.meta.Ref.Unpack()
	reader, err := storage.GetRangeObject(ctx, wal.Dir+c.id, int64(offset), int64(size))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	// todo: use a pool
	buf := bytes.NewBuffer(make([]byte, 0, size))
	_, err = buf.ReadFrom(reader)
	if err != nil {
		return nil, err
	}
	// create logql pipeline and remove tenantID
	// todo: we might want to create a single pipeline for all chunks from the same series.
	streamPipeline := pipeline.ForStream(
		labels.NewBuilder(c.labels).Del(index.TenantLabel).Labels(),
	)
	it, err := chunks.NewEntryIterator(buf.Bytes(), streamPipeline, direction, from, through)
	if err != nil {
		return nil, err
	}
	return iter.EntryIteratorWithClose(it, func() error {
		// todo: return buffer to pool.
		return nil
	}), nil
}

func isOverlapping(first, second lazyChunk, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		return first.meta.MinTime <= second.meta.MaxTime
	}
	return first.meta.MaxTime < second.meta.MinTime
}
