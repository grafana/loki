package distributor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockIngestLimitsFrontendClient mocks the RPC calls for tests.
type mockIngestLimitsFrontendClient struct {
	t               *testing.T
	calls           atomic.Uint64
	expectedRequest *proto.ExceedsLimitsRequest
	response        *proto.ExceedsLimitsResponse
	responseErr     error
}

// Implements the ingestLimitsFrontendClient interface.
func (c *mockIngestLimitsFrontendClient) ExceedsLimits(_ context.Context, r *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	c.calls.Add(1)
	if c.expectedRequest != nil {
		require.Equal(c.t, c.expectedRequest, r)
	}
	if c.responseErr != nil {
		return nil, c.responseErr
	}
	return c.response, nil
}

func TestIngestLimits_EnforceLimits(t *testing.T) {
	clock := quartz.NewMock(t)
	clock.Set(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name            string
		tenant          string
		streams         []KeyedStream
		expectedRequest *proto.ExceedsLimitsRequest
		response        *proto.ExceedsLimitsResponse
		responseErr     error
		expectedStreams []KeyedStream
		expectedErr     string
	}{{
		// This test also asserts that streams are returned unmodified.
		name:   "error should be returned if limits cannot be checked",
		tenant: "test",
		streams: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
			Stream: logproto.Stream{
				Labels: "foo",
				Entries: []logproto.Entry{{
					Timestamp: clock.Now(),
					Line:      "bar",
					StructuredMetadata: []logproto.LabelAdapter{{
						Name:  "baz",
						Value: "qux",
					}},
				}},
			},
		}, {
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
			Stream: logproto.Stream{
				Labels: "bar",
				Entries: []logproto.Entry{{
					Timestamp: clock.Now(),
					Line:      "baz",
					StructuredMetadata: []logproto.LabelAdapter{{
						Name:  "qux",
						Value: "corge",
					}},
				}},
			},
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
				TotalSize:  9,
			}, {
				StreamHash: 2,
				TotalSize:  11,
			}},
		},
		responseErr: errors.New("failed to check limits"),
		expectedErr: "failed to check limits",
	}, {
		name:   "exceeds limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		response: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expectedStreams: []KeyedStream{},
	}, {
		name:   "one of two streams exceeds limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
		}, {
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
			}, {
				StreamHash: 2,
			}},
		},
		response: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expectedStreams: []KeyedStream{{
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
	}, {
		name:   "does not exceed limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
		}, {
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
			}, {
				StreamHash: 2,
			}},
		},
		response: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{},
		},
		expectedStreams: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
		}, {
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
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
			accepted, err := l.EnforceLimits(ctx, test.tenant, test.streams)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				// The streams should be returned unmodified.
				require.Equal(t, test.streams, accepted)
			} else {
				require.Nil(t, err)
				require.Equal(t, test.expectedStreams, accepted)
			}
		})
	}
}

func TestIngestLimits_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name            string
		tenant          string
		streams         []KeyedStream
		expectedRequest *proto.ExceedsLimitsRequest
		response        *proto.ExceedsLimitsResponse
		responseErr     error
		expectedResult  []*proto.ExceedsLimitsResult
		expectedErr     string
	}{{
		name:   "error should be returned if limits cannot be checked",
		tenant: "test",
		streams: []KeyedStream{{
			HashKeyNoShard: 1,
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
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
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		response: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 1,
				Reason:     uint32(limits.ReasonMaxStreams),
			}},
		},
		expectedResult: []*proto.ExceedsLimitsResult{{
			StreamHash: 1,
			Reason:     uint32(limits.ReasonMaxStreams),
		}},
	}, {
		name:   "does not exceed limits",
		tenant: "test",
		streams: []KeyedStream{{
			HashKeyNoShard: 1,
		}},
		expectedRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 1,
			}},
		},
		response: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{},
		},
		expectedResult: []*proto.ExceedsLimitsResult{},
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
			res, err := l.ExceedsLimits(ctx, test.tenant, test.streams)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				require.Nil(t, res)
			} else {
				require.Nil(t, err)
				require.Equal(t, test.expectedResult, res)
			}
		})
	}
}
