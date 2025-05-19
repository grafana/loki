package limits

import (
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/util"
)

type httpTenantLimitsResponse struct {
	Tenant        string  `json:"tenant"`
	ActiveStreams uint64  `json:"activeStreams"`
	Rate          float64 `json:"rate"`
}

// ServeHTTP implements the http.Handler interface.
// It returns the current stream counts and status per tenant as a JSON response.
func (s *IngestLimits) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tenant := mux.Vars(r)["tenant"]
	if tenant == "" {
		http.Error(w, "invalid tenant", http.StatusBadRequest)
		return
	}

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Get the rate window cutoff for rate calculations
	rateWindowCutoff := time.Now().Add(-s.cfg.BucketDuration).UnixNano()

	// Calculate stream counts and status per tenant
	var (
		activeStreams uint64
		totalSize     uint64
		response      httpTenantLimitsResponse
	)

	s.usage.All(func(tenantID string, _ int32, stream Stream) {
		if tenantID != tenant {
			return
		}

		if stream.LastSeenAt >= cutoff {
			activeStreams++

			// Calculate size only within the rate window
			for _, bucket := range stream.RateBuckets {
				if bucket.Timestamp >= rateWindowCutoff {
					totalSize += bucket.Size
				}
			}
		}
	})

	// Calculate rate using only data from within the rate window
	calculatedRate := float64(totalSize) / s.cfg.WindowSize.Seconds()

	if activeStreams > 0 {
		response = httpTenantLimitsResponse{
			Tenant:        tenant,
			ActiveStreams: activeStreams,
			Rate:          calculatedRate,
		}
	} else {
		// If no active streams found, return zeros
		response = httpTenantLimitsResponse{
			Tenant:        tenant,
			ActiveStreams: 0,
			Rate:          0,
		}
	}

	// Log the calculated values for debugging
	level.Debug(s.logger).Log(
		"msg", "HTTP endpoint calculated stream usage",
		"tenant", tenant,
		"active_streams", activeStreams,
		"total_size", util.HumanizeBytes(totalSize),
		"rate_window_seconds", s.cfg.RateWindow.Seconds(),
		"calculated_rate", util.HumanizeBytes(uint64(calculatedRate)),
	)

	// Use util.WriteJSONResponse to write the JSON response
	util.WriteJSONResponse(w, response)
}
