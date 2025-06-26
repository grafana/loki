package frontend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestFrontend_ServeHTTP(t *testing.T) {
	tests := []struct {
		name                         string
		expectedExceedsLimitsRequest *proto.ExceedsLimitsRequest
		exceedsLimitsResponses       []*proto.ExceedsLimitsResponse
		request                      httpExceedsLimitsRequest
		expected                     httpExceedsLimitsResponse
	}{{
		name: "within limits",
		expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{}},
		request: httpExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		// expected should be default value.
	}, {
		name: "exceeds limits",
		expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		}},
		request: httpExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		expected: httpExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readRing, _ := newMockRingWithClientPool(t, "test", nil, nil)
			f, err := New(Config{
				LifecyclerConfig: ring.LifecyclerConfig{
					RingConfig: ring.Config{
						KVStore: kv.Config{
							Store: "inmemory",
						},
					},
				},
			}, "test", readRing, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)
			f.gatherer = &mockExceedsLimitsGatherer{
				t:                            t,
				expectedExceedsLimitsRequest: test.expectedExceedsLimitsRequest,
				exceedsLimitsResponses:       test.exceedsLimitsResponses,
			}
			ts := httptest.NewServer(f)
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
