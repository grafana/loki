package bloomshipper

import (
	"context"
	"time"

	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type ForEachBlockCallback func(bq *v1.BlockQuerier, minFp, maxFp uint64) error

type ReadShipper interface {
	GetBlockRefs(ctx context.Context, tenant string, from, through model.Time) ([]BlockRef, error)
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

func toModelTime(t time.Time) model.Time {
	return model.TimeFromUnixNano(t.UnixNano())
}
