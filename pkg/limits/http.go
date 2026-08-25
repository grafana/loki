package limits

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/util"
)

type httpTenantLimitsResponse struct {
	Tenant          string            `json:"tenant"`
	StreamsTotal    uint64            `json:"streams_total"`
	StreamsByPolicy map[string]uint64 `json:"streams_by_policy"`
	Rate            float64           `json:"rate"`
}

// ServeHTTP implements the http.Handler interface.
// It returns the current stream counts and status per tenant as a JSON response.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tenant := mux.Vars(r)["tenant"]
	if tenant == "" {
		http.Error(w, "invalid tenant", http.StatusBadRequest)
		return
	}
	var streams, sumBuckets uint64
	streamsByPolicy := make(map[string]uint64)
	for _, stream := range s.usage.TenantActiveStreams(tenant) {
		streams++
		for _, bucket := range stream.rateBuckets {
			sumBuckets += bucket.size
		}
		streamsByPolicy[stream.policy]++
	}
	rate := float64(sumBuckets) / s.cfg.ActiveWindow.Seconds()
	util.WriteJSONResponse(w, httpTenantLimitsResponse{
		Tenant:          tenant,
		StreamsTotal:    streams,
		StreamsByPolicy: streamsByPolicy,
		Rate:            rate,
	})
}
