package frontend

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFrontend_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name                   string
		exceedsLimitsRequest   *logproto.ExceedsLimitsRequest
		exceedsLimitsResponses []*logproto.ExceedsLimitsResponse
		expected               *logproto.ExceedsLimitsResponse
	}{{
		name: "no streams",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: nil,
		},
		expected: &logproto.ExceedsLimitsResponse{
			Tenant:  "test",
			Results: []*logproto.ExceedsLimitsResult{},
		},
	}, {
		name: "one stream",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}},
		expected: &logproto.ExceedsLimitsResponse{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		},
	}, {
		name: "one stream, no responses",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{
			Tenant:  "test",
			Results: []*logproto.ExceedsLimitsResult{},
		}},
		expected: &logproto.ExceedsLimitsResponse{
			Tenant:  "test",
			Results: []*logproto.ExceedsLimitsResult{},
		},
	}, {
		name: "two stream, one response",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x4,
				EntriesSize:            0x5,
				StructuredMetadataSize: 0x6,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsRateLimit),
			}},
		}},
		expected: &logproto.ExceedsLimitsResponse{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsRateLimit),
			}},
		},
	}, {
		name: "two stream, two responses",
		exceedsLimitsRequest: &logproto.ExceedsLimitsRequest{
			Tenant: "test",
			Streams: []*logproto.StreamMetadata{{
				StreamHash:             0x1,
				EntriesSize:            0x2,
				StructuredMetadataSize: 0x3,
			}, {
				StreamHash:             0x4,
				EntriesSize:            0x5,
				StructuredMetadataSize: 0x6,
			}},
		},
		exceedsLimitsResponses: []*logproto.ExceedsLimitsResponse{{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}},
		}, {
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsRateLimit),
			}},
		}},
		expected: &logproto.ExceedsLimitsResponse{
			Tenant: "test",
			Results: []*logproto.ExceedsLimitsResult{{
				StreamHash: 0x1,
				Reason:     uint32(limits.ReasonExceedsMaxStreams),
			}, {
				StreamHash: 0x4,
				Reason:     uint32(limits.ReasonExceedsRateLimit),
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
