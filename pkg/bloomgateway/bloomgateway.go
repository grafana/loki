/*
Bloom Gateway package

The bloom gateway is a component that can be run as a standalone microserivce
target and provides capabilities for filtering ChunkRefs based on a given list
of line filter expressions.

			     Querier   Query Frontend
			        |           |
			................................... service boundary
			        |           |
			        +----+------+
			             |
			     indexgateway.Gateway
			             |
			   bloomgateway.BloomQuerier
			             |
			   bloomgateway.GatewayClient
			             |
			  logproto.BloomGatewayClient
			             |
			................................... service boundary
			             |
			      bloomgateway.Gateway
			             |
			       bloomshipper.Store
			             |
			      bloomshipper.Shipper
			             |
	     bloomshipper.BloomFileClient
			             |
			        ObjectClient
			             |
			................................... service boundary
			             |
		         object storage
*/
package bloomgateway

import (
	"context"
	"sort"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

var errGatewayUnhealthy = errors.New("bloom-gateway is unhealthy in the ring")
var errInvalidTenant = errors.New("invalid tenant in chunk refs")

type metrics struct{}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{}
}

type Gateway struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *metrics

	bloomStore bloomshipper.Store

	sharding ShardingStrategy

	ConvertChunkRefToChunkID func(chunkRef logproto.ChunkRef) string
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, shardingStrategy ShardingStrategy, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:      cfg,
		logger:   logger,
		metrics:  newMetrics(reg),
		sharding: shardingStrategy,
		// Only keep convert function instead of full schemaCfg
		ConvertChunkRefToChunkID: schemaCfg.ExternalKey,
	}

	bloomShipper, err := bloomshipper.NewBloomShipper(bloomshipper.Config{}, schemaCfg, storageCfg, cm, logger)
	if err != nil {
		return nil, err
	}

	bloomStore, err := bloomshipper.NewBloomStore(bloomShipper)
	if err != nil {
		return nil, err
	}

	g.bloomStore = bloomStore
	g.Service = services.NewIdleService(g.starting, g.stopping)

	return g, nil
}

func (g *Gateway) starting(ctx context.Context) error {
	return nil
}

func (g *Gateway) stopping(_ error) error {
	g.bloomStore.Stop()
	return nil
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	for _, ref := range req.Refs {
		if ref.UserID != tenantID {
			return nil, errors.Wrapf(errInvalidTenant, "expected chunk refs from tenant %s, got tenant %s", tenantID, ref.UserID)
		}
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	chunkRefs := req.Refs

	// Only query bloom filters if filters are present
	if len(req.Filters) > 0 {
		chunkRefs, err = g.bloomStore.FilterChunkRefs(ctx, tenantID, req.From.Time(), req.Through.Time(), req.Refs, req.Filters...)
		if err != nil {
			return nil, err
		}
	}

	// TODO(chaudum): Re-use buffers for response.
	resp := make([]*logproto.ChunkIDsForStream, 0)
	for idx, chunkRef := range chunkRefs {
		fp := chunkRef.Fingerprint
		if idx == 0 || fp > resp[len(resp)-1].Fingerprint {
			r := &logproto.ChunkIDsForStream{
				Fingerprint: fp,
				ChunkIDs:    []string{g.ConvertChunkRefToChunkID(*chunkRef)},
			}
			resp = append(resp, r)
			continue
		}
		resp[len(resp)-1].ChunkIDs = append(resp[len(resp)-1].ChunkIDs, g.ConvertChunkRefToChunkID(*chunkRef))
	}

	return &logproto.FilterChunkRefResponse{Chunks: resp}, nil
}
