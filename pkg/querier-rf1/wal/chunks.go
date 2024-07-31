package wal

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"

	"github.com/grafana/loki/pkg/push"
)

const batchSize = 16

type ChunkData struct {
	meta   *chunks.Meta
	labels labels.Labels
	id     string
}

func newChunkData(id string, lbs *labels.ScratchBuilder, meta *chunks.Meta) ChunkData {
	lbs.Sort()
	return ChunkData{
		id:     id,
		meta:   meta,
		labels: lbs.Labels(),
	}
}

// ChunksEntryIterator iterates over log entries
type ChunksEntryIterator[T iter.EntryIterator] struct {
	baseChunksIterator[T]

	pipeline log.Pipeline
	current  iter.EntryIterator
}

// ChunksSampleIterator iterates over metric samples
type ChunksSampleIterator[T iter.SampleIterator] struct {
	baseChunksIterator[T]
	current   iter.SampleIterator
	extractor log.SampleExtractor
}

func NewChunksEntryIterator(
	ctx context.Context,
	storage BlockStorage,
	chunks []ChunkData,
	pipeline log.Pipeline,
	direction logproto.Direction,
	minT, maxT int64,
) *ChunksEntryIterator[iter.EntryIterator] {
	sortChunks(chunks, direction)
	return &ChunksEntryIterator[iter.EntryIterator]{
		baseChunksIterator: baseChunksIterator[iter.EntryIterator]{
			ctx:       ctx,
			chunks:    chunks,
			direction: direction,
			storage:   storage,
			batch:     make([]ChunkData, 0, batchSize),
			minT:      minT,
			maxT:      maxT,

			iteratorFactory: func(chunks []ChunkData) (iter.EntryIterator, error) {
				return createNextEntryIterator(ctx, chunks, direction, pipeline, storage, minT, maxT)
			},
			isNil: func(it iter.EntryIterator) bool { return it == nil },
		},
		pipeline: pipeline,
	}
}

func NewChunksSampleIterator(
	ctx context.Context,
	storage BlockStorage,
	chunks []ChunkData,
	extractor log.SampleExtractor,
	minT, maxT int64,
) *ChunksSampleIterator[iter.SampleIterator] {
	sortChunks(chunks, logproto.FORWARD)
	return &ChunksSampleIterator[iter.SampleIterator]{
		baseChunksIterator: baseChunksIterator[iter.SampleIterator]{
			ctx:       ctx,
			chunks:    chunks,
			direction: logproto.FORWARD,
			storage:   storage,
			batch:     make([]ChunkData, 0, batchSize),
			minT:      minT,
			maxT:      maxT,

			iteratorFactory: func(chunks []ChunkData) (iter.SampleIterator, error) {
				return createNextSampleIterator(ctx, chunks, extractor, storage, minT, maxT)
			},
			isNil: func(it iter.SampleIterator) bool { return it == nil },
		},
		extractor: extractor,
	}
}

func sortChunks(chunks []ChunkData, direction logproto.Direction) {
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
}

// baseChunksIterator contains common fields and methods for both entry and sample iterators
type baseChunksIterator[T interface {
	Next() bool
	Close() error
	Err() error
	StreamHash() uint64
	Labels() string
}] struct {
	chunks          []ChunkData
	direction       logproto.Direction
	minT, maxT      int64
	storage         BlockStorage
	ctx             context.Context
	iteratorFactory func([]ChunkData) (T, error)
	isNil           func(T) bool

	batch   []ChunkData
	current T
	err     error
}

func (b *baseChunksIterator[T]) nextBatch() error {
	b.batch = b.batch[:0]
	for len(b.chunks) > 0 &&
		(len(b.batch) < batchSize ||
			isOverlapping(b.batch[len(b.batch)-1], b.chunks[0], b.direction)) {
		b.batch = append(b.batch, b.chunks[0])
		b.chunks = b.chunks[1:]
	}
	// todo: error if the batch is too big.
	return nil
}

// todo: better chunk batch iterator
func (b *baseChunksIterator[T]) Next() bool {
	for b.isNil(b.current) || !b.current.Next() {
		if !b.isNil(b.current) {
			if err := b.current.Close(); err != nil {
				b.err = err
				return false
			}
		}
		if len(b.chunks) == 0 {
			return false
		}
		if err := b.nextBatch(); err != nil {
			b.err = err
			return false
		}
		var err error
		b.current, err = b.iteratorFactory(b.batch)
		if err != nil {
			b.err = err
			return false
		}
	}
	return true
}

func createNextEntryIterator(
	ctx context.Context,
	batch []ChunkData,
	direction logproto.Direction,
	pipeline log.Pipeline,
	storage BlockStorage,
	minT, maxT int64,
) (iter.EntryIterator, error) {
	var (
		iterators []iter.EntryIterator
		mtx       sync.Mutex
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(64)
	for _, chunk := range batch {
		chunk := chunk // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			chunkData, err := readChunkData(ctx, storage, chunk)
			if err != nil {
				return fmt.Errorf("error reading chunk data: %w", err)
			}

			streamPipeline := pipeline.ForStream(chunk.labels)
			chunkIterator, err := chunks.NewEntryIterator(chunkData, streamPipeline, direction, minT, maxT)
			if err != nil {
				return fmt.Errorf("error creating entry iterator: %w", err)
			}

			mtx.Lock()
			iterators = append(iterators, chunkIterator)
			mtx.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortEntryIterator(iterators, direction), nil
}

func createNextSampleIterator(
	ctx context.Context,
	batch []ChunkData,
	pipeline log.SampleExtractor,
	storage BlockStorage,
	minT, maxT int64,
) (iter.SampleIterator, error) {
	var (
		iterators []iter.SampleIterator
		mtx       sync.Mutex
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(64)
	for _, chunk := range batch {
		chunk := chunk // https://golang.org/doc/faq#closures_and_goroutines
		g.Go(func() error {
			chunkData, err := readChunkData(ctx, storage, chunk)
			if err != nil {
				return fmt.Errorf("error reading chunk data: %w", err)
			}

			streamPipeline := pipeline.ForStream(chunk.labels)
			chunkIterator, err := chunks.NewSampleIterator(chunkData, streamPipeline, minT, maxT)
			if err != nil {
				return fmt.Errorf("error creating sample iterator: %w", err)
			}

			mtx.Lock()
			iterators = append(iterators, chunkIterator)
			mtx.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return iter.NewSortSampleIterator(iterators), nil
}

func (b *baseChunksIterator[T]) Close() error {
	if !b.isNil(b.current) {
		return b.current.Close()
	}
	return nil
}

func (b *baseChunksIterator[T]) Err() error {
	if b.err != nil {
		return b.err
	}
	if !b.isNil(b.current) {
		return b.current.Err()
	}
	return nil
}

func (b *baseChunksIterator[T]) Labels() string {
	return b.current.Labels()
}

func (b *baseChunksIterator[T]) StreamHash() uint64 {
	return b.current.StreamHash()
}

func (c *ChunksEntryIterator[T]) At() push.Entry       { return c.current.At() }
func (c *ChunksSampleIterator[T]) At() logproto.Sample { return c.current.At() }

func isOverlapping(first, second ChunkData, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		return first.meta.MinTime <= second.meta.MaxTime
	}
	return first.meta.MaxTime < second.meta.MinTime
}

func readChunkData(ctx context.Context, storage BlockStorage, chunk ChunkData) ([]byte, error) {
	offset, size := chunk.meta.Ref.Unpack()
	// todo: We should be able to avoid many IOPS to object storage
	// if chunks are next to each other and we should be able to pack range request
	// together.
	reader, err := storage.GetRangeObject(ctx, chunk.id, int64(offset), int64(size))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data := make([]byte, size)
	_, err = reader.Read(data)
	if err != nil {
		return nil, err
	}

	return data, nil
}
