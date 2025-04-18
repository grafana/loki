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

func TestRingStreamUsageGatherer_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		initialCache   map[string]*logproto.GetAssignedPartitionsResponse
		expectedCache  map[string]*logproto.GetAssignedPartitionsResponse
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "GET with empty cache",
			initialCache:   map[string]*logproto.GetAssignedPartitionsResponse{},
			expectedStatus: http.StatusOK,
			expectedBody:   "Ring Stream Usage Cache",
		},
		{
			name: "GET with populated cache",
			initialCache: map[string]*logproto.GetAssignedPartitionsResponse{
				"instance1:8080": {
					AssignedPartitions: map[int32]int64{
						1: time.Now().Unix(),
						2: time.Now().Unix(),
						3: time.Now().Unix(),
					},
				},
				"instance2:8080": {
					AssignedPartitions: map[int32]int64{
						4: time.Now().Unix(),
						5: time.Now().Unix(),
						6: time.Now().Unix(),
					},
				},
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "instance1:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl := time.Hour
			// Create a new cache with test data
			cache := NewPartitionConsumerCache(ttl)
			for k, v := range tt.initialCache {
				cache.Set(k, v, ttl)
			}

			f := Frontend{
				partitionIDCache: cache,
			}

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/ring-usage", nil)

			// Create response recorder
			w := httptest.NewRecorder()

			// Call the handler
			f.PartitionConsumersCacheHandler(w, req)

			// Check status code
			require.Equal(t, tt.expectedStatus, w.Code)
			require.Contains(t, w.Body.String(), tt.expectedBody)
		})
	}
}

func TestRingStreamUsageGatherer_PartitionConsumerCacheEvictHandler(t *testing.T) {
	tests := []struct {
		name           string
		formData       map[string]string
		initialCache   map[string]*logproto.GetAssignedPartitionsResponse
		expectedCache  map[string]*logproto.GetAssignedPartitionsResponse
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "POST clear specific instance",
			formData: map[string]string{
				"instance": "instance1:8080",
			},
			initialCache: map[string]*logproto.GetAssignedPartitionsResponse{
				"instance1:8080": {
					AssignedPartitions: map[int32]int64{
						1: time.Now().Unix(),
						2: time.Now().Unix(),
						3: time.Now().Unix(),
					},
				},
				"instance2:8080": {
					AssignedPartitions: map[int32]int64{
						4: time.Now().Unix(),
						5: time.Now().Unix(),
						6: time.Now().Unix(),
					},
				},
			},
			expectedCache: map[string]*logproto.GetAssignedPartitionsResponse{
				"instance2:8080": {
					AssignedPartitions: map[int32]int64{
						4: time.Now().Unix(),
						5: time.Now().Unix(),
						6: time.Now().Unix(),
					},
				},
			},
			expectedStatus: http.StatusSeeOther,
		},
		{
			name: "POST clear all cache",
			initialCache: map[string]*logproto.GetAssignedPartitionsResponse{
				"instance1:8080": {
					AssignedPartitions: map[int32]int64{
						1: time.Now().Unix(),
						2: time.Now().Unix(),
						3: time.Now().Unix(),
					},
				},
				"instance2:8080": {
					AssignedPartitions: map[int32]int64{
						4: time.Now().Unix(),
						5: time.Now().Unix(),
						6: time.Now().Unix(),
					},
				},
			},
			expectedCache:  map[string]*logproto.GetAssignedPartitionsResponse{},
			expectedStatus: http.StatusSeeOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ttl := time.Hour
			// Create a new cache with test data
			cache := NewPartitionConsumerCache(ttl)
			for k, v := range tt.initialCache {
				cache.Set(k, v, ttl)
			}

			f := Frontend{
				partitionIDCache: cache,
			}

			// Create request
			req := httptest.NewRequest(http.MethodPost, "/ring-usage", nil)
			if tt.formData != nil {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				form := make(map[string][]string)
				for k, v := range tt.formData {
					form[k] = []string{v}
				}
				req.PostForm = form
				req.Form = form
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Call the handler
			f.PartitionConsumersCacheEvictHandler(w, req)

			// Check status code
			require.Equal(t, tt.expectedStatus, w.Code)

			require.Equal(t, len(tt.expectedCache), cache.Len())
			for key := range tt.expectedCache {
				require.NotNil(t, cache.Get(key))
			}
		})
	}
}
