package bloomshipper

import (
	"context"
	"math"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	bloom_shipper "github.com/grafana/loki/pkg/storage/bloom/bloom-shipper"
)

// TODO(chaudum): This is just a placeholder and needs to be replaced by actual
// bloom filter interface.
type BloomFilter interface {
}

type ForEachBloomFilterCallback func(f BloomFilter) error

type ReadShipper interface {
	ForEachBloom(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBloomFilterCallback) error
}

type WriteShipper interface {
}

type Shipper interface {
	ReadShipper
	WriteShipper
	Stop()
}

type BloomShipper struct {
	client bloom_shipper.Shipper
}

func NewBloomShipper(client bloom_shipper.Shipper) (*BloomShipper, error) {
	return &BloomShipper{client: client}, nil
}

func (bs *BloomShipper) Stop() {
	bs.client.Stop()
}

func (bs *BloomShipper) ForEachBloom(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBloomFilterCallback) error {
	// Assume fingerprints to be sorted in ascending order
	var minFp uint64 = 0
	var maxFp uint64 = math.MaxUint64
	if len(fingerprints) > 0 {
		minFp = fingerprints[0]
		maxFp = fingerprints[len(fingerprints)-1]
	}

	blockRefs, err := bs.loadBlockRefs(ctx, tenant, from, through, minFp, maxFp)
	if err != nil {
		return err
	}

	// TODO(chaudum): Do we need to initialize blocks so they load their data?
	_, err = bs.client.GetBlocks(ctx, blockRefs)
	if err != nil {
		return err
	}

	// TODO(chaudum) Read blooms from the blocks
	// for _, block := range blocks {
	// 	// Iterate over blocks and their bloom filters
	// 	// For each bloom filter invoke the callback function.
	// }

	return nil
}

func (bs *BloomShipper) loadBlockRefs(ctx context.Context, tenant string, start, end time.Time, minFp, maxFp uint64) ([]bloom_shipper.BlockRef, error) {
	params := bloom_shipper.MetaSearchParams{
		TenantID:       tenant,
		MinFingerprint: minFp,
		MaxFingerprint: maxFp,
		StartTimestamp: start.UnixNano(),
		EndTimestamp:   end.UnixNano(),
	}
	metas, err := bs.client.GetAll(ctx, params)
	if err != nil {
		return nil, err
	}
	var maxBlocks int
	for i := range metas {
		maxBlocks += len(metas[i].Blocks)
	}
	blocks := make([]bloom_shipper.BlockRef, 0, maxBlocks)
	for i := range metas {
		blocks = append(blocks, metas[i].Blocks...)
	}
	return blocks, nil
}

type Store interface {
	FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error)
	Stop()
}

type BloomStore struct {
	shipper Shipper
}

func NewBloomStore(shipper Shipper) (*BloomStore, error) {
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
	bs.shipper.ForEachBloom(ctx, tenant, from, through, fingerprints, func(f BloomFilter) error {
		bf.add(f)
		return nil
	})
	return bf, nil
}

type bloomFilters struct {
	filters []BloomFilter
}

func newBloomFilters(size int) *bloomFilters {
	return &bloomFilters{
		filters: make([]BloomFilter, 0),
	}
}

func (bf *bloomFilters) add(f BloomFilter) {
	bf.filters = append(bf.filters, f)
}

func (bf *bloomFilters) FilterChunkRefs(ctx context.Context, tenant string, from, through time.Time, chunkRefs []*logproto.ChunkRef, filters ...*logproto.LineFilterExpression) ([]*logproto.ChunkRef, error) {
	return nil, nil
}
