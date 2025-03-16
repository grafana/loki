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
		Streams: make([]*logproto.StreamMetadata, len(req.StreamHashes)),
	}
	for i, hash := range req.StreamHashes {
		protoReq.Streams[i] = &logproto.StreamMetadata{
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
