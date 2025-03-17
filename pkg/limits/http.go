package limits

import (
	"net/http"
	"time"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/util"
)

// ServeHTTP implements the http.Handler interface.
// It returns the current stream counts and status per tenant as a JSON response.
func (s *IngestLimits) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Get the rate window cutoff for rate calculations
	rateWindowCutoff := time.Now().Add(-s.cfg.BucketDuration).UnixNano()

	// Calculate stream counts and status per tenant
	type tenantLimits struct {
		Tenant        string  `json:"tenant"`
		ActiveStreams uint64  `json:"activeStreams"`
		Rate          float64 `json:"rate"`
	}

	// Get tenant and partitions from query parameters
	params := r.URL.Query()
	tenant := params.Get("tenant")
	var (
		activeStreams uint64
		totalSize     uint64
		response      tenantLimits
	)

	for _, partitions := range s.metadata[tenant] {
		for _, stream := range partitions {
			if stream.lastSeenAt >= cutoff {
				activeStreams++

				// Calculate size only within the rate window
				for _, bucket := range stream.rateBuckets {
					if bucket.timestamp >= rateWindowCutoff {
						totalSize += bucket.size
					}
				}
			}
		}
	}

	// Calculate rate using only data from within the rate window
	calculatedRate := float64(totalSize) / s.cfg.WindowSize.Seconds()

	if activeStreams > 0 {
		response = tenantLimits{
			Tenant:        tenant,
			ActiveStreams: activeStreams,
			Rate:          calculatedRate,
		}
	} else {
		// If no active streams found, return zeros
		response = tenantLimits{
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
