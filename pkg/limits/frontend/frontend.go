// Package frontend contains provides a frontend service for ingest limits.
// It is responsible for receiving and answering gRPC requests from distributors,
// such as exceeds limits requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
package frontend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
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
}

// New returns a new Frontend.
func New(cfg Config, ringName string, ring ring.ReadRing, limits Limits, logger log.Logger) (*Frontend, error) {
	var servs []services.Service

	factory := ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
		return NewIngestLimitsClient(cfg.ClientConfig, addr)
	})

	pool := NewIngestLimitsClientPool(ringName, cfg.ClientConfig.PoolConfig, ring, factory, logger)
	limitsSrv := NewRingIngestLimitsService(ring, pool, limits, logger)

	f := &Frontend{
		cfg:    cfg,
		logger: logger,
		limits: limitsSrv,
	}

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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		level.Error(f.logger).Log("msg", "error reading request body", "err", err)
		http.Error(w, "error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req exceedsLimitsRequest
	if err := json.Unmarshal(body, &req); err != nil {
		level.Error(f.logger).Log("msg", "error unmarshaling request body", "err", err)
		http.Error(w, "error unmarshaling request body", http.StatusBadRequest)
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

	// Call the service
	resp, err := f.limits.ExceedsLimits(r.Context(), protoReq)
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
