package bloomshipper

import (
	"context"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type ForEachBlockCallback func(bq *v1.BlockQuerier) error

type ReadShipper interface {
	ForEachBlock(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBlockCallback) error
}

type Interface interface {
	ReadShipper
	Stop()
}

type Store interface {
	FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.GroupedChunkRefs, filters ...*logproto.LineFilterExpression) ([]*logproto.GroupedChunkRefs, error)
	Stop()
}

type BloomStore struct {
	shipper Interface
}

func NewBloomStore(shipper Interface) (*BloomStore, error) {
	return &BloomStore{
		shipper: shipper,
	}, nil
}

func (bs *BloomStore) Stop() {
	bs.shipper.Stop()
}

func (bs *BloomStore) FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.GroupedChunkRefs, filters ...*logproto.LineFilterExpression) ([]*logproto.GroupedChunkRefs, error) {
	fingerprints := make([]uint64, 0, len(chunkRefs))
	for _, ref := range chunkRefs {
		fingerprints = append(fingerprints, ref.Fingerprint)
	}

	blooms, err := bs.queriers(ctx, tenant, from, through, fingerprints)
	if err != nil {
		return nil, err
	}

	searches := convertLineFilterExpressions(filters)

	for _, ref := range chunkRefs {
		refs, err := blooms.Filter(ctx, model.Fingerprint(ref.Fingerprint), convertToChunkRefs(ref.Refs), searches)
		if err != nil {
			return nil, err
		}
		ref.Refs = convertToShortRefs(refs)
	}
	return chunkRefs, nil
}

func (bs *BloomStore) queriers(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) (*bloomQueriers, error) {
	bf := newBloomFilters(1024)
	err := bs.shipper.ForEachBlock(ctx, tenant, from, through, fingerprints, func(bq *v1.BlockQuerier) error {
		bf.queriers = append(bf.queriers, bq)
		return nil
	})
	return bf, err
}

func convertLineFilterExpressions(filters []*logproto.LineFilterExpression) [][]byte {
	searches := make([][]byte, len(filters))
	for _, f := range filters {
		searches = append(searches, []byte(f.Match))
	}
	return searches
}

// convertToShortRefs converts a v1.ChunkRefs into []*logproto.ShortRef
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToShortRefs(refs v1.ChunkRefs) []*logproto.ShortRef {
	result := make([]*logproto.ShortRef, len(refs))
	for _, ref := range refs {
		result = append(result, &logproto.ShortRef{From: ref.Start, Through: ref.End, Checksum: ref.Checksum})
	}
	return result
}

// convertToChunkRefs converts a []*logproto.ShortRef into v1.ChunkRefs
// TODO(chaudum): Avoid conversion by transferring v1.ChunkRefs in gRPC request.
func convertToChunkRefs(refs []*logproto.ShortRef) v1.ChunkRefs {
	result := make(v1.ChunkRefs, len(refs))
	for _, ref := range refs {
		result = append(result, v1.ChunkRef{Start: ref.From, End: ref.Through, Checksum: ref.Checksum})
	}
	return result
}

type bloomQueriers struct {
	queriers []*v1.BlockQuerier
}

func newBloomFilters(size int) *bloomQueriers {
	return &bloomQueriers{
		queriers: make([]*v1.BlockQuerier, size),
	}
}

func (bf *bloomQueriers) Filter(_ context.Context, fp model.Fingerprint, chunkRefs v1.ChunkRefs, filters [][]byte) (v1.ChunkRefs, error) {
	result := make(v1.ChunkRefs, len(chunkRefs))
	for _, bq := range bf.queriers {
		refs, err := bq.CheckChunksForSeries(fp, chunkRefs, filters)
		if err != nil {
			return nil, err
		}
		result = append(result, refs...)
	}
	return result, nil
}
