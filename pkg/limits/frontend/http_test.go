package frontend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/dskit/limiter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFrontend_ServeHTTP(t *testing.T) {
	tests := []struct {
		name                          string
		limits                        Limits
		expectedGetStreamUsageRequest *GetStreamUsageRequest
		getStreamUsageResponses       []GetStreamUsageResponse
		request                       exceedsLimitsRequest
		expected                      exceedsLimitsResponse
	}{{
		name: "within limits",
		limits: &mockLimits{
			maxGlobalStreams: 1,
			ingestionRate:    100,
		},
		expectedGetStreamUsageRequest: &GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		},
		getStreamUsageResponses: []GetStreamUsageResponse{{
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 1,
				Rate:          10,
			},
		}},
		request: exceedsLimitsRequest{
			TenantID:     "test",
			StreamHashes: []uint64{0x1},
		},
		// expected should be default value (no rejected streams).
	}, {
		name: "exceeds limits",
		limits: &mockLimits{
			maxGlobalStreams: 1,
			ingestionRate:    100,
		},
		expectedGetStreamUsageRequest: &GetStreamUsageRequest{
			Tenant:       "test",
			StreamHashes: []uint64{0x1},
		},
		getStreamUsageResponses: []GetStreamUsageResponse{{
			Response: &logproto.GetStreamUsageResponse{
				Tenant:        "test",
				ActiveStreams: 2,
				Rate:          200,
			},
		}},
		request: exceedsLimitsRequest{
			TenantID:     "test",
			StreamHashes: []uint64{0x1},
		},
		expected: exceedsLimitsResponse{
			RejectedStreams: []*logproto.RejectedStream{{
				StreamHash: 0x1,
				Reason:     "exceeds_rate_limit",
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := Frontend{
				limits:      test.limits,
				rateLimiter: limiter.NewRateLimiter(newRateLimitsAdapter(test.limits), time.Second),
				streamUsage: &mockStreamUsageGatherer{
					t:               t,
					expectedRequest: test.expectedGetStreamUsageRequest,
					responses:       test.getStreamUsageResponses,
				},
				metrics: newMetrics(prometheus.NewRegistry()),
			}
			ts := httptest.NewServer(&f)
			defer ts.Close()

			b, err := json.Marshal(test.request)
			require.NoError(t, err)

			resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(b))
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)

			defer resp.Body.Close()
			b, err = io.ReadAll(resp.Body)
			require.NoError(t, err)

			var actual exceedsLimitsResponse
			require.NoError(t, json.Unmarshal(b, &actual))
			require.Equal(t, test.expected, actual)
		})
	}
}
