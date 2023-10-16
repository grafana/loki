package bloomshipper

import (
	"context"
	"time"

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
	FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error)
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

func (bs *BloomStore) FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error) {
	fingerprints := make([]uint64, 0, len(chunkRefs))
	for _, ref := range chunkRefs {
		fingerprints = append(fingerprints, ref.Fingerprint)
	}
	blooms, err := bs.blooms(ctx, tenant, from, through, fingerprints)
	if err != nil {
		return nil, err
	}
	return blooms.FilterChunkRefs(ctx, tenant, from, through, chunkRefs, filters...)
}

func (bs *BloomStore) blooms(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) (*bloomFilters, error) {
	bf := &bloomFilters{}
	err := bs.shipper.ForEachBlock(ctx, tenant, from, through, fingerprints, func(bq *v1.BlockQuerier) error {
		return nil
	})
	return bf, err
}

type bloomFilters struct {
}

func newBloomFilters(size int) *bloomFilters {
	return &bloomFilters{}
}

func (bf *bloomFilters) FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error) {
	return nil, nil
}
