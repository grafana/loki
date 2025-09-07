package limits

import (
	"net/http"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/util"
)

type httpTenantLimitsResponse struct {
	Tenant  string  `json:"tenant"`
	Streams uint64  `json:"streams"`
	Rate    float64 `json:"rate"`
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
	for _, stream := range s.usage.TenantActiveStreams(tenant) {
		streams++
		for _, bucket := range stream.rateBuckets {
			sumBuckets += bucket.size
		}
	}
	rate := float64(sumBuckets) / s.cfg.ActiveWindow.Seconds()

	// Log the calculated values for debugging
	level.Debug(s.logger).Log(
		"msg", "HTTP endpoint calculated stream usage",
		"tenant", tenant,
		"streams", streams,
		"sum_buckets", util.HumanizeBytes(sumBuckets),
		"rate_window_seconds", s.cfg.RateWindow.Seconds(),
		"rate", util.HumanizeBytes(uint64(rate)),
	)

	// Use util.WriteJSONResponse to write the JSON response
	util.WriteJSONResponse(w, httpTenantLimitsResponse{
		Tenant:  tenant,
		Streams: streams,
		Rate:    rate,
	})
}
