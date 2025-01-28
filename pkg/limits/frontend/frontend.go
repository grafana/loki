// Package frontend contains provides a frontend service for ingest limits.
// It is responsible for receiving and answering gRPC requests from distributors,
// such as exceeds limits requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
package frontend

import (
	"context"
	"fmt"
	"encoding/json"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	RingKey  = "ingest-limits-frontend"
	RingName = "ingest-limits-frontend"
)

// Frontend is the limits-frontend service, and acts a service wrapper for
// all components needed to run the limits-frontend.
type Frontend struct {
	services.Service

	cfg    Config
	logger log.Logger

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	limits IngestLimitsService

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher
}

// New returns a new Frontend.
func New(cfg Config, ringName string, readRing ring.ReadRing, limits Limits, logger log.Logger, reg prometheus.Registerer) (*Frontend, error) {
	var servs []services.Service

	factory := ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
		return NewIngestLimitsBackendClient(cfg.ClientConfig, addr)
	})

	pool := NewIngestLimitsClientPool(ringName, cfg.ClientConfig.PoolConfig, readRing, factory, logger)
	limitsSrv := NewRingIngestLimitsService(readRing, pool, limits, logger)

	f := &Frontend{
		cfg:    cfg,
		logger: logger,
		limits: limitsSrv,
	}

	var err error
	f.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, f, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	// Watch the lifecycler
	f.lifecyclerWatcher = services.NewFailureWatcher()
	f.lifecyclerWatcher.WatchService(f.lifecycler)

	servs = append(servs, f.lifecycler)
	servs = append(servs, pool)
	mgr, err := services.NewManager(servs...)
	if err != nil {
		return nil, errors.Wrap(err, "services manager")
	}

	f.subservices = mgr
	f.subservicesWatcher = services.NewFailureWatcher()
	f.subservicesWatcher.WatchManager(f.subservices)
	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)

	return f, nil
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest limits frontend instance.
func (s *Frontend) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another ingest limits frontend instance.
func (s *Frontend) TransferOut(_ context.Context) error {
	return nil
}

// starting implements services.Service.
func (f *Frontend) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, f.subservices)
}

// running implements services.Service.
func (f *Frontend) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-f.subservicesWatcher.Chan():
		return errors.Wrap(err, "ingest limits frontend subservice failed")
	}
}

// stopping implements services.Service.
func (f *Frontend) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
}

// ExceedsLimits implements logproto.IngestLimitsFrontendClient.
func (f *Frontend) ExceedsLimits(ctx context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	return f.limits.ExceedsLimits(ctx, r)
}

type exceedsLimitsRequest struct {
	TenantID     string   `json:"tenantID"`
	StreamHashes []uint64 `json:"streamHashes"`
}

type exceedsLimitsResponse struct {
	RejectedStreams []*logproto.RejectedStream `json:"rejectedStreams,omitempty"`
}

// ServeHTTP implements http.Handler.
func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req exceedsLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		level.Error(f.logger).Log("msg", "error unmarshalling request body", "err", err)
		http.Error(w, "error unmarshalling request body", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		http.Error(w, "tenantID is required", http.StatusBadRequest)
		return
	}

	// Convert request to protobuf format
	protoReq := &logproto.ExceedsLimitsRequest{
		Tenant:  req.TenantID,
		Streams: make([]*logproto.StreamMetadataWithSize, len(req.StreamHashes)),
	}
	for i, hash := range req.StreamHashes {
		protoReq.Streams[i] = &logproto.StreamMetadataWithSize{
			StreamHash: hash,
		}
	}

	ctx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(r.Context(), req.TenantID))
	if err != nil {
		http.Error(w, "failed to inject org ID", http.StatusInternalServerError)
		return
	}

	// Call the service
	resp, err := f.limits.ExceedsLimits(ctx, protoReq)
	if err != nil {
		level.Error(f.logger).Log("msg", "error checking limits", "err", err)
		http.Error(w, "error checking limits", http.StatusInternalServerError)
		return
	}

	// Convert response to JSON format
	jsonResp := exceedsLimitsResponse{
		RejectedStreams: resp.RejectedStreams,
	}

	util.WriteJSONResponse(w, jsonResp)
}
