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
	t                            *testing.T
	calls                        atomic.Uint64
	expectedExceedsLimitsRequest *proto.ExceedsLimitsRequest
	exceedsLimitsResponse        *proto.ExceedsLimitsResponse
	exceedsLimitsResponseErr     error
	expectedUpdateRatesRequest   *proto.UpdateRatesRequest
	updateRatesResponse          *proto.UpdateRatesResponse
	updateRatesResponseErr       error
}

// Implements the ingestLimitsFrontendClient interface.
func (c *mockIngestLimitsFrontendClient) ExceedsLimits(_ context.Context, r *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	c.calls.Add(1)
	if c.expectedExceedsLimitsRequest != nil {
		require.Equal(c.t, c.expectedExceedsLimitsRequest, r)
	}
	if c.exceedsLimitsResponseErr != nil {
		return nil, c.exceedsLimitsResponseErr
	}
	return c.exceedsLimitsResponse, nil
}

func (c *mockIngestLimitsFrontendClient) UpdateRates(_ context.Context, r *proto.UpdateRatesRequest) (*proto.UpdateRatesResponse, error) {
	c.calls.Add(1)
	if c.expectedUpdateRatesRequest != nil {
		require.Equal(c.t, c.expectedUpdateRatesRequest, r)
	}
	if c.updateRatesResponseErr != nil {
		return nil, c.updateRatesResponseErr
	}
	return c.updateRatesResponse, nil
}

func TestIngestLimits_EnforceLimits(t *testing.T) {
	clock := quartz.NewMock(t)
	clock.Set(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))

	tests := []struct {
		name             string
		tenant           string
		streams          []KeyedStream
		expectedRequest  *proto.ExceedsLimitsRequest
		response         *proto.ExceedsLimitsResponse
		responseErr      error
		expectedAccepted []KeyedStream
		expectedRejected []KeyedStream
		expectedErr      string
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
		expectedAccepted: []KeyedStream{{
			HashKey:        1000,
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
			HashKey:        2000,
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
		expectedRejected: []KeyedStream{},
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
		expectedAccepted: []KeyedStream{},
		expectedRejected: []KeyedStream{{
			HashKey:        1000,
			HashKeyNoShard: 1,
		}},
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
		expectedAccepted: []KeyedStream{{
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
		expectedRejected: []KeyedStream{{
			HashKey:        1000,
			HashKeyNoShard: 1,
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
		expectedAccepted: []KeyedStream{{
			HashKey:        1000, // Should not be used.
			HashKeyNoShard: 1,
		}, {
			HashKey:        2000, // Should not be used.
			HashKeyNoShard: 2,
		}},
		expectedRejected: []KeyedStream{},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockClient := mockIngestLimitsFrontendClient{
				t:                            t,
				expectedExceedsLimitsRequest: test.expectedRequest,
				exceedsLimitsResponse:        test.response,
				exceedsLimitsResponseErr:     test.responseErr,
			}
			l := newIngestLimits(&mockClient, prometheus.NewRegistry())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			accepted, rejected, err := l.EnforceLimits(ctx, test.tenant, test.streams)
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
				// The streams should be returned unmodified in accepted when there's an error.
				require.Equal(t, test.expectedAccepted, accepted)
				require.Equal(t, test.expectedRejected, rejected)
			} else {
				require.Nil(t, err)
				require.Equal(t, test.expectedAccepted, accepted)
				require.Equal(t, test.expectedRejected, rejected)
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
				t:                            t,
				expectedExceedsLimitsRequest: test.expectedRequest,
				exceedsLimitsResponse:        test.response,
				exceedsLimitsResponseErr:     test.responseErr,
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

func TestIngestLimits_UpdateRates(t *testing.T) {
	tests := []struct {
		name            string
		tenant          string
		streams         []SegmentedStream
		expectedRequest *proto.UpdateRatesRequest
		response        *proto.UpdateRatesResponse
		responseErr     error
		expectedResult  []*proto.UpdateRatesResult
		expectedErr     string
	}{{
		name:   "error should be returned if rates cannot be updated",
		tenant: "test",
		streams: []SegmentedStream{{
			SegmentationKey: "test",
		}},
		responseErr: errors.New("failed to update rates"),
		expectedErr: "failed to update rates",
	}, {
		name:   "updates rates",
		tenant: "test",
		streams: []SegmentedStream{{
			SegmentationKey:     "test",
			SegmentationKeyHash: 13113208752873574959,
		}},
		expectedRequest: &proto.UpdateRatesRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 13113208752873574959,
			}},
		},
		response: &proto.UpdateRatesResponse{
			Results: []*proto.UpdateRatesResult{{
				StreamHash: 13113208752873574959,
				Rate:       1024,
			}},
		},
		expectedResult: []*proto.UpdateRatesResult{{
			StreamHash: 13113208752873574959,
			Rate:       1024,
		}},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockClient := mockIngestLimitsFrontendClient{
				t:                          t,
				expectedUpdateRatesRequest: test.expectedRequest,
				updateRatesResponse:        test.response,
				updateRatesResponseErr:     test.responseErr,
			}
			l := newIngestLimits(&mockClient, prometheus.NewRegistry())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			res, err := l.UpdateRates(ctx, test.tenant, test.streams)
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
