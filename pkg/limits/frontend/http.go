package frontend

import (
	"encoding/json"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

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
		http.Error(w, "JSON is invalid or does not match expected schema", http.StatusBadRequest)
		return
	}

	if req.TenantID == "" {
		http.Error(w, "tenantID is required", http.StatusBadRequest)
		return
	}

	streams := make([]*logproto.StreamMetadata, len(req.StreamHashes))
	for _, streamHash := range req.StreamHashes {
		streams = append(streams, &logproto.StreamMetadata{
			StreamHash: streamHash,
		})
	}
	protoReq := &logproto.ExceedsLimitsRequest{
		Tenant:  req.TenantID,
		Streams: streams,
	}

	ctx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(r.Context(), req.TenantID))
	if err != nil {
		http.Error(w, "failed to inject org ID", http.StatusInternalServerError)
		return
	}

	resp, err := f.ExceedsLimits(ctx, protoReq)
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to check limits", "err", err)
		http.Error(w, "an unexpected error occurred while checking limits", http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, exceedsLimitsResponse{
		RejectedStreams: resp.RejectedStreams,
	})
}
