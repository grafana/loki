package bloomshipper

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
)

// TODO(chaudum): This is just a placeholder and needs to be replaced by actual
// bloom block querier interface.
type BlockQuerier interface {
}

type ForEachBlockCallback func(bq BlockQuerier) error

type ReadShipper interface {
	ForEachBlock(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBlockCallback) error
}

type Shipper interface {
	ReadShipper
	Stop()
}

type NoopBloomShipper struct {
	cfg    Config
	logger log.Logger
}

// NewBloomShipper creates a new BloomShipper struct.
// TODO(chaudum): Replace NoopBloomShipper with actual implementation.
func NewBloomShipper(cfg Config, _ config.SchemaConfig, _ storage.Config, _ storage.ClientMetrics, logger log.Logger) (*NoopBloomShipper, error) {
	return &NoopBloomShipper{
		cfg:    cfg,
		logger: log.With(logger, "component", "noop-bloom-shipper"),
	}, nil
}

func (bs *NoopBloomShipper) Stop() {
}

func (bs *NoopBloomShipper) ForEachBlock(ctx context.Context, tenant string, from, through time.Time, fingerprints []uint64, callback ForEachBlockCallback) error {
	level.Debug(bs.logger).Log("msg", "ForEachBlock", "tenant", tenant, "from", from, "through", through, "fingerprints", fingerprints)
	return nil
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
	err := bs.shipper.ForEachBlock(ctx, tenant, from, through, fingerprints, func(bq BlockQuerier) error {
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
