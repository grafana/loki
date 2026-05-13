package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestInjectHTTPHeaderIntoGRPCRequest(t *testing.T) {
	for _, tt := range []struct {
		name               string
		headers            map[string]string
		md, expectMetadata metadata.MD
	}{
		{
			name: "creates new metadata and sets header",
			headers: map[string]string{
				httpreq.LokiDisablePipelineWrappersHeader: "true",
			},
			expectMetadata: metadata.New(map[string]string{httpreq.LokiDisablePipelineWrappersHeader: "true"}),
		},
		{
			name: "creates new metadata and sets replay header",
			headers: map[string]string{
				httpreq.AdaptiveTelemetryReplayHeader: "true",
			},
			expectMetadata: metadata.New(map[string]string{lokiReplayGRPCMetadataKey: "true"}),
		},
		{
			name: "sets headers on existing metadata",
			headers: map[string]string{
				httpreq.LokiDisablePipelineWrappersHeader: "true",
				httpreq.AdaptiveTelemetryReplayHeader:     "true",
			},
			md: metadata.New(map[string]string{"x-foo": "bar"}),
			expectMetadata: metadata.New(map[string]string{
				"x-foo": "bar",
				httpreq.LokiDisablePipelineWrappersHeader: "true",
				lokiReplayGRPCMetadataKey:                 "true",
			}),
		},
		{
			name:           "no header, leave metadata untouched",
			md:             metadata.New(map[string]string{"x-foo": "bar"}),
			expectMetadata: metadata.New(map[string]string{"x-foo": "bar"}),
		},
		{
			name:           "no header",
			expectMetadata: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			for header, value := range tt.headers {
				ctx = httpreq.InjectHeader(ctx, header, value)
			}

			if tt.md != nil {
				ctx = metadata.NewOutgoingContext(ctx, tt.md)
			}

			ctx = injectHTTPHeadersIntoGRPCRequest(ctx)
			md, _ := metadata.FromOutgoingContext(ctx)
			require.EqualValues(t, tt.expectMetadata, md)
		})
	}
}

func TestExtractHTTPHeaderFromGRPCRequest(t *testing.T) {
	for _, tt := range []struct {
		name            string
		md              metadata.MD
		expectedHeaders map[string]string
	}{
		{
			name: "extracts headers from metadata",
			md: metadata.New(map[string]string{
				httpreq.LokiDisablePipelineWrappersHeader: "true",
				lokiReplayGRPCMetadataKey:                 "true",
			}),
			expectedHeaders: map[string]string{
				httpreq.LokiDisablePipelineWrappersHeader: "true",
				httpreq.AdaptiveTelemetryReplayHeader:     "true",
			},
		},
		{
			name: "non-nil metadata without header",
			md:   metadata.New(map[string]string{"x-foo": "bar"}),
		},
		{
			name: "nil metadata",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), tt.md)
			ctx = extractHTTPHeadersFromGRPCRequest(ctx)
			for header, value := range tt.expectedHeaders {
				require.Equal(t, value, httpreq.ExtractHeader(ctx, header))
			}
		})
	}
}
