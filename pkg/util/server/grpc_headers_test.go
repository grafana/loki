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
		name, header       string
		md, expectMetadata metadata.MD
	}{
		{
			name:           "creates new metadata and sets header",
			header:         "true",
			expectMetadata: metadata.New(map[string]string{httpreq.LokiDisablePipelineWrappersHeader: "true"}),
		},
		{
			name:           "sets header on existing metadata",
			header:         "true",
			md:             metadata.New(map[string]string{"x-foo": "bar"}),
			expectMetadata: metadata.New(map[string]string{"x-foo": "bar", httpreq.LokiDisablePipelineWrappersHeader: "true"}),
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
			if tt.header != "" {
				ctx = httpreq.InjectHeader(context.Background(), httpreq.LokiDisablePipelineWrappersHeader, tt.header)
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
		name         string
		md           metadata.MD
		expectedResp string
	}{
		{
			name:         "extracts header from metadata",
			md:           metadata.New(map[string]string{httpreq.LokiDisablePipelineWrappersHeader: "true"}),
			expectedResp: "true",
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
			require.Equal(t, tt.expectedResp, httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader))
		})
	}
}
