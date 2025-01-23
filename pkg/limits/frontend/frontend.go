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
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

// Frontend is the limits-frontend service, and acts a service wrapper for
// all components needed to run the limits-frontend.
type Frontend struct {
	services.Service
	cfg    Config
	logger log.Logger
	limits IngestLimitsService
}

// NewFrontend returns a new Frontend.
func NewFrontend(cfg Config, srv IngestLimitsService, logger log.Logger) (*Frontend, error) {
	f := &Frontend{
		cfg:    cfg,
		logger: logger,
		limits: srv,
	}
	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)
	return f, nil
}

// starting implements services.Service.
func (f *Frontend) starting(_ context.Context) error {
	return nil
}

// running implements services.Service.
func (f *Frontend) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// stopping implements services.Service.
func (f *Frontend) stopping(_ error) error {
	return nil
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
