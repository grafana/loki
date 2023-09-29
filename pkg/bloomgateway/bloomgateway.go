package bloomgateway

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/bloom/bloom-shipper"
	"github.com/grafana/loki/pkg/storage/config"
)

var errGatewayUnhealthy = errors.New("bloom-gateway is unhealthy in the ring")

type metrics struct{}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{}
}

type Gateway struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *metrics

	bloomShipper bloom_shipper.Shipper
	sharding     ShardingStrategy
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, shardingStrategy ShardingStrategy, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:      cfg,
		logger:   logger,
		metrics:  newMetrics(reg),
		sharding: shardingStrategy,
	}

	shipper, err := bloom_shipper.NewShipper(schemaCfg.Configs, storageCfg, storage.NewClientMetrics())
	if err != nil {
		return nil, err
	}

	g.bloomShipper = shipper
	g.Service = services.NewIdleService(g.starting, g.stopping)

	g.init()

	return g, nil
}

func (g *Gateway) init() error {
	return nil
}

func (g *Gateway) starting(ctx context.Context) error {
	// Do not sync on startup
	// return g.sync(ctx)
	return nil
}

func (g *Gateway) stopping(_ error) error {
	return nil
}

func (g *Gateway) sync(ctx context.Context) error {
	// TODO(chaudum): Make configurable
	start := time.Now()
	end := start.Add(24 * time.Hour)

	tenants, err := g.bloomShipper.ListTenants(ctx)
	if err != nil {
		return err
	}

	tenants, err = g.sharding.FilterTenants(ctx, tenants)
	if err != nil {
		return err
	}

	blockRefs := make([]bloom_shipper.BlockRef, 0, 64)
	for _, tenant := range tenants {
		ownedBlocks, err := g.loadAllBlocksForTenant(ctx, tenant, end, start)
		if err != nil {
			return err
		}
		blockRefs = append(blockRefs, ownedBlocks...)
	}

	// TODO(chaudum): Do we need to initialize blocks so they load their data?
	_, err = g.bloomShipper.GetBlocks(ctx, blockRefs)
	if err != nil {
		return err
	}

	return nil
}

func (g *Gateway) loadAllBlocksForTenant(ctx context.Context, tenant string, start, end time.Time) ([]bloom_shipper.BlockRef, error) {
	params := bloom_shipper.MetaSearchParams{
		TenantID:       tenant,
		MinFingerprint: 0,
		MaxFingerprint: math.MaxUint64,
		StartTimestamp: start.UnixNano(),
		EndTimestamp:   end.UnixNano(),
	}
	metas, err := g.bloomShipper.GetAll(ctx, params)
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
	return g.sharding.FilterBlocks(ctx, tenant, blocks)
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	return &logproto.FilterChunkRefResponse{}, nil
}
