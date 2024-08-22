package wal

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"

	"github.com/grafana/loki/pkg/push"
)

const defaultBatchSize = 16

type ChunkData struct {
	meta   *chunks.Meta
	labels labels.Labels
	id     string
}

func newChunkData(id string, lbs *labels.ScratchBuilder, meta *chunks.Meta) ChunkData {
	lbs.Sort()
	newLbs := lbs.Labels()
	j := 0
	for _, l := range newLbs {
		if l.Name != index.TenantLabel {
			newLbs[j] = l
			j++
		}
	}
	newLbs = newLbs[:j]
	return ChunkData{
		id: id,
		meta: &chunks.Meta{ // incoming Meta is from a shared buffer, so create a new one
			Ref:     meta.Ref,
			MinTime: meta.MinTime,
			MaxTime: meta.MaxTime,
		},
		labels: newLbs,
	}
}

// ChunksEntryIterator iterates over log entries
type ChunksEntryIterator[T iter.EntryIterator] struct {
	baseChunksIterator[T]
}

// ChunksSampleIterator iterates over metric samples
type ChunksSampleIterator[T iter.SampleIterator] struct {
	baseChunksIterator[T]
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
			bachSize:  defaultBatchSize,
			batch:     make([]ChunkData, 0, defaultBatchSize),
			minT:      minT,
			maxT:      maxT,

			iteratorFactory: func(chunks []ChunkData) (iter.EntryIterator, error) {
				return createNextEntryIterator(ctx, chunks, direction, pipeline, storage, minT, maxT)
			},
			isNil: func(it iter.EntryIterator) bool { return it == nil },
		},
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
			bachSize:  defaultBatchSize,
			batch:     make([]ChunkData, 0, defaultBatchSize),
			minT:      minT,
			maxT:      maxT,

			iteratorFactory: func(chunks []ChunkData) (iter.SampleIterator, error) {
				return createNextSampleIterator(ctx, chunks, extractor, storage, minT, maxT)
			},
			isNil: func(it iter.SampleIterator) bool { return it == nil },
		},
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

	bachSize int
	batch    []ChunkData
	current  T
	err      error
}

func (b *baseChunksIterator[T]) nextBatch() error {
	b.batch = b.batch[:0]
	for len(b.chunks) > 0 &&
		(len(b.batch) < b.bachSize ||
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
	iterators := make([]iter.EntryIterator, 0, len(batch))

	data, err := downloadChunks(ctx, storage, batch)
	if err != nil {
		return nil, err
	}

	for i, chunk := range batch {
		streamPipeline := pipeline.ForStream(chunk.labels)
		chunkIterator, err := chunks.NewEntryIterator(data[i], streamPipeline, direction, minT, maxT)
		if err != nil {
			return nil, fmt.Errorf("error creating entry iterator: %w", err)
		}
		iterators = append(iterators, chunkIterator)
	}

	// todo: Use NonOverlapping iterator when possible. This will reduce the amount of entries processed during iteration.
	return iter.NewSortEntryIterator(iterators, direction), nil
}

func createNextSampleIterator(
	ctx context.Context,
	batch []ChunkData,
	pipeline log.SampleExtractor,
	storage BlockStorage,
	minT, maxT int64,
) (iter.SampleIterator, error) {
	iterators := make([]iter.SampleIterator, 0, len(batch))

	data, err := downloadChunks(ctx, storage, batch)
	if err != nil {
		return nil, err
	}

	for i, chunk := range batch {
		streamPipeline := pipeline.ForStream(chunk.labels)
		chunkIterator, err := chunks.NewSampleIterator(data[i], streamPipeline, minT, maxT)
		if err != nil {
			return nil, fmt.Errorf("error creating sample iterator: %w", err)
		}
		iterators = append(iterators, chunkIterator)
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
	return first.meta.MaxTime >= second.meta.MinTime
}

func downloadChunks(ctx context.Context, storage BlockStorage, chks []ChunkData) ([][]byte, error) {
	data := make([][]byte, len(chks))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(64)
	for i, chunk := range chks {
		chunk := chunk
		i := i
		g.Go(func() error {
			chunkData, err := readChunkData(ctx, storage, chunk)
			if err != nil {
				return fmt.Errorf("error reading chunk data: %w", err)
			}
			data[i] = chunkData
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return data, nil
}

func readChunkData(ctx context.Context, storage BlockStorage, chunk ChunkData) ([]byte, error) {
	offset, size := chunk.meta.Ref.Unpack()
	// todo: We should be able to avoid many IOPS to object storage
	// if chunks are next to each other and we should be able to pack range request
	// together.
	reader, err := storage.GetObjectRange(ctx, wal.Dir+chunk.id, int64(offset), int64(size))
	if err != nil {
		return nil, fmt.Errorf("could not get range reader for %s: %w", chunk.id, err)
	}
	defer reader.Close()

	data := make([]byte, size)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return nil, fmt.Errorf("could not read socket for %s: %w", chunk.id, err)
	}

	return data, nil
}
