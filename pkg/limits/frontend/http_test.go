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
		request                       httpExceedsLimitsRequest
		expected                      httpExceedsLimitsResponse
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
		request: httpExceedsLimitsRequest{
			TenantID:     "test",
			StreamHashes: []uint64{0x1},
		},
		// expected should be default value.
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
		request: httpExceedsLimitsRequest{
			TenantID:     "test",
			StreamHashes: []uint64{0x1},
		},
		expected: httpExceedsLimitsResponse{
			Results: []*logproto.ExceedsLimitsResult{{
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

			var actual httpExceedsLimitsResponse
			require.NoError(t, json.Unmarshal(b, &actual))
			require.Equal(t, test.expected, actual)
		})
	}
}
