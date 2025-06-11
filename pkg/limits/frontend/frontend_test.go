package frontend

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestFrontend_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name                   string
		exceedsLimitsRequest   *proto.ExceedsLimitsRequest
		exceedsLimitsResponses []*proto.ExceedsLimitsResponse
		expected               *proto.ExceedsLimitsResponse
	}{{
		name: "no streams",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: nil,
		},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{},
		},
	}, {
		name: "one stream",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		},
	}, {
		name: "one stream, no responses",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{},
		}},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{},
		},
	}, {
		name: "two stream, one response",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x4,
				TotalSize:  0x9,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		},
	}, {
		name: "two stream, two responses",
		exceedsLimitsRequest: &proto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*proto.StreamMetadata{{
				StreamHash: 0x1,
				TotalSize:  0x5,
			}, {
				StreamHash: 0x4,
				TotalSize:  0x9,
			}},
		},
		exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}, {
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		expected: &proto.ExceedsLimitsResponse{
			Results: []*proto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := Frontend{
				gatherer: &mockExceedsLimitsGatherer{
					t:                            t,
					expectedExceedsLimitsRequest: test.exceedsLimitsRequest,
					exceedsLimitsResponses:       test.exceedsLimitsResponses,
				},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			actual, err := f.ExceedsLimits(ctx, test.exceedsLimitsRequest)
			require.NoError(t, err)
			require.Equal(t, test.expected, actual)
		})
	}
}
