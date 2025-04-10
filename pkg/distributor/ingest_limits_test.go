package distributor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockIngestLimitsFrontendClient mocks the RPC calls for tests.
type mockIngestLimitsFrontendClient struct {
	t               *testing.T
	expectedRequest *logproto.ExceedsLimitsRequest
	response        *logproto.ExceedsLimitsResponse
	responseErr     error
}

// Implements the ingestLimitsFrontendClient interface.
func (c *mockIngestLimitsFrontendClient) exceedsLimits(_ context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	require.Equal(c.t, c.expectedRequest, r)
	if c.responseErr != nil {
		return nil, c.responseErr
	}
	return c.response, nil
}

// This test asserts that when checking ingest limits the expected proto
// message is sent, and that for a given response, the result contains the
// expected streams each with their expected reasons.
func TestIngestLimits_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name            string
		tenant          string
		streams         []KeyedStream
		expectedRequest *logproto.ExceedsLimitsRequest
		response        *logproto.ExceedsLimitsResponse
		responseErr     error
		expected        []exceedsIngestLimitsResult
		expectedErr     string
	}{{
		name:   "error should be returned if limits cannot be checked",
		tenant: "test",
		streams: []KeyedStream{{
			HashKeyNoShard: 1,
		}},
		expectedRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		responseErr: errors.New("failed to check limits"),
		expectedErr: "failed to check limits",
	}, {
		name:   "exceeds limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKeyNoShard: 1,
		}},
		expectedRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		response: &logproto.ExceedsLimitsResponse{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 1,
				Reason:     "test",
			}},
		},
		expected: []exceedsIngestLimitsResult{{
			hash:    1,
			reasons: []string{"test"},
		}},
	}, {
		name:   "does not exceed limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKeyNoShard: 1,
		}},
		expectedRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		response: &logproto.ExceedsLimitsResponse{
			Tenant:  "test",
			Results: []*logproto.ExceedsLimitsResult{},
		},
		expected: nil,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockClient := mockIngestLimitsFrontendClient{
				t:               t,
				expectedRequest: test.expectedRequest,
				response:        test.response,
				responseErr:     test.responseErr,
			}
			l := newIngestLimits(&mockClient, prometheus.NewRegistry())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			exceedsLimits, rejectedStreams, err := l.exceedsLimits(ctx, test.tenant, test.streams)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				require.False(t, exceedsLimits)
				require.Empty(t, rejectedStreams)
			} else {
				require.Nil(t, err)
				require.Equal(t, test.expected, rejectedStreams)
				require.Equal(t, len(test.expected) > 0, exceedsLimits)
			}
		})
	}
}
