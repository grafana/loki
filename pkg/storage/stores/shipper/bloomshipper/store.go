package bloomshipper

import (
	"context"
	"sort"
	"time"

	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type ForEachBlockCallback func(bq *v1.BlockQuerier, minFp, maxFp uint64) error

type ReadShipper interface {
	GetBlockRefs(ctx context.Context, tenant string, from, through model.Time) ([]BlockRef, error)
	ForEachBlock(ctx context.Context, tenant string, from, through model.Time, fingerprints []uint64, callback ForEachBlockCallback) error
	Fetch(ctx context.Context, tenant string, blocks []BlockRef, callback ForEachBlockCallback) error
}

type Interface interface {
	ReadShipper
	Stop()
}

type BlockQuerierWithFingerprintRange struct {
	*v1.BlockQuerier
	MinFp, MaxFp model.Fingerprint
}

type Store interface {
	GetBlockRefs(ctx context.Context, tenant string, from, through time.Time) ([]BlockRef, error)
	GetBlockQueriers(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) ([]BlockQuerierWithFingerprintRange, error)
	GetBlockQueriersForBlockRefs(ctx context.Context, tenant string, blocks []BlockRef) ([]BlockQuerierWithFingerprintRange, error)
	ForEach(ctx context.Context, tenant string, blocks []BlockRef, callback ForEachBlockCallback) error
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

// GetBlockRefs implements Store
func (bs *BloomStore) GetBlockRefs(ctx context.Context, tenant string, from, through time.Time) ([]BlockRef, error) {
	return bs.shipper.GetBlockRefs(ctx, tenant, toModelTime(from), toModelTime(through))
}

// ForEach implements Store
func (bs *BloomStore) ForEach(ctx context.Context, tenant string, blocks []BlockRef, callback ForEachBlockCallback) error {
	return bs.shipper.Fetch(ctx, tenant, blocks, callback)
}

// GetQueriersForBlocks implements Store
func (bs *BloomStore) GetBlockQueriersForBlockRefs(ctx context.Context, tenant string, blocks []BlockRef) ([]BlockQuerierWithFingerprintRange, error) {
	bqs := make([]BlockQuerierWithFingerprintRange, 0, 32)
	err := bs.shipper.Fetch(ctx, tenant, blocks, func(bq *v1.BlockQuerier, minFp uint64, maxFp uint64) error {
		bqs = append(bqs, BlockQuerierWithFingerprintRange{
			BlockQuerier: bq,
			MinFp:        model.Fingerprint(minFp),
			MaxFp:        model.Fingerprint(maxFp),
		})
		return nil
	})
	sort.Slice(bqs, func(i, j int) bool {
		return bqs[i].MinFp < bqs[j].MinFp
	})
	return bqs, err
}

// BlockQueriers implements Store
func (bs *BloomStore) GetBlockQueriers(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) ([]BlockQuerierWithFingerprintRange, error) {
	bqs := make([]BlockQuerierWithFingerprintRange, 0, 32)
	err := bs.shipper.ForEachBlock(ctx, tenant, toModelTime(from), toModelTime(through), fingerprints, func(bq *v1.BlockQuerier, minFp uint64, maxFp uint64) error {
		bqs = append(bqs, BlockQuerierWithFingerprintRange{
			BlockQuerier: bq,
			MinFp:        model.Fingerprint(minFp),
			MaxFp:        model.Fingerprint(maxFp),
		})
		return nil
	})
	sort.Slice(bqs, func(i, j int) bool {
		return bqs[i].MinFp < bqs[j].MinFp
	})
	return bqs, err
}

func toModelTime(t time.Time) model.Time {
	return model.TimeFromUnixNano(t.UnixNano())
}
