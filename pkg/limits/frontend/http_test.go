package frontend

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFrontend_ServeHTTP(t *testing.T) {
	tests := []struct {
		name                         string
		expectedExceedsLimitsRequest *logproto.ExceedsLimitsRequest
		exceedsLimitsResponses       []*logproto.ExceedsLimitsResponse
		request                      httpExceedsLimitsRequest
		expected                     httpExceedsLimitsResponse
	}{{
		name: "within limits",
		expectedExceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{}},
		request: httpExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		// expected should be default value.
	}, {
		name: "exceeds limits",
		expectedExceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     "exceeds_rate_limit",
			}},
		}},
		request: httpExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
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
				gatherer: &mockExceedsLimitsGatherer{
					t:                            t,
					expectedExceedsLimitsRequest: test.expectedExceedsLimitsRequest,
					exceedsLimitsResponses:       test.exceedsLimitsResponses,
				},
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
