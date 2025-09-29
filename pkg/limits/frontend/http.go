package frontend

import (
	"encoding/json"
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/util"
)

type httpExceedsLimitsRequest struct {
	Tenant  string                  `json:"tenant"`
	Streams []*proto.StreamMetadata `json:"streams"`
}

type httpExceedsLimitsResponse struct {
	Results []*proto.ExceedsLimitsResult `json:"results,omitempty"`
}

// ServeHTTP implements http.Handler.
func (f *Frontend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req httpExceedsLimitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON is invalid or does not match expected schema", http.StatusBadRequest)
		return
	}

	if req.Tenant == "" {
		http.Error(w, "tenant is required", http.StatusBadRequest)
		return
	}

	ctx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(r.Context(), req.Tenant))
	if err != nil {
		http.Error(w, "failed to inject org ID", http.StatusInternalServerError)
		return
	}

	resp, err := f.ExceedsLimits(ctx, &proto.ExceedsLimitsRequest{
		Tenant:  req.Tenant,
		Streams: req.Streams,
	})
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to check if request exceeds limits", "err", err)
		http.Error(w, "an unexpected error occurred while checking if request exceeds limits", http.StatusInternalServerError)
		return
	}

	util.WriteJSONResponse(w, httpExceedsLimitsResponse{
		Results: resp.Results,
	})
}
