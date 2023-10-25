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
	ForEachBlock(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBlockCallback) error
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
	GetBlockQueriers(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) ([]BlockQuerierWithFingerprintRange, error)
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

func (bs *BloomStore) GetBlockQueriers(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64) ([]BlockQuerierWithFingerprintRange, error) {
	bqs := make([]BlockQuerierWithFingerprintRange, 0, 32)
	err := bs.shipper.ForEachBlock(ctx, tenant, from, through, fingerprints, func(bq *v1.BlockQuerier, minFp uint64, maxFp uint64) error {
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
