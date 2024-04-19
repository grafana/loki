package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestInjectQueryTagsIntoGRPCRequest(t *testing.T) {
	for _, tt := range []struct {
		name, tags         string
		md, expectMetadata metadata.MD
	}{
		{
			name:           "creates new metadata and sets query tags",
			tags:           "Source=logvolhist",
			expectMetadata: metadata.New(map[string]string{"x-query-tags": "Source=logvolhist"}),
		},
		{
			name:           "sets query tags on existing metadata",
			tags:           "Source=logvolhist",
			md:             metadata.New(map[string]string{"x-foo": "bar"}),
			expectMetadata: metadata.New(map[string]string{"x-foo": "bar", "x-query-tags": "Source=logvolhist"}),
		},
		{
			name:           "no query tags, leave metadata untouched",
			md:             metadata.New(map[string]string{"x-foo": "bar"}),
			expectMetadata: metadata.New(map[string]string{"x-foo": "bar"}),
		},
		{
			name:           "no query tags",
			expectMetadata: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.tags != "" {
				ctx = httpreq.InjectQueryTags(context.Background(), tt.tags)
			}

			if tt.md != nil {
				ctx = metadata.NewOutgoingContext(ctx, tt.md)
			}

			ctx = injectIntoGRPCRequest(ctx)
			md, _ := metadata.FromOutgoingContext(ctx)
			require.EqualValues(t, tt.expectMetadata, md)
		})
	}
}

func TestExtractQueryTagsFromGRPCRequest(t *testing.T) {
	for _, tt := range []struct {
		name         string
		md           metadata.MD
		expectedResp string
	}{
		{
			name:         "extracts query tags from metadata",
			md:           metadata.New(map[string]string{"x-query-tags": "Source=logvolhist"}),
			expectedResp: "Source=logvolhist",
		},
		{
			name: "non-nil metadata without query tags",
			md:   metadata.New(map[string]string{"x-foo": "bar"}),
		},
		{
			name: "nil metadata",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), tt.md)
			require.Equal(t, tt.expectedResp, getQueryTags(extractFromGRPCRequest(ctx)))
		})
	}

}
