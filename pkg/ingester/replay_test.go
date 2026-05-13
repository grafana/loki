package ingester

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
	server_util "github.com/grafana/loki/v3/pkg/util/server"
)

func TestInjectReplayHeaderFromGRPCMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(server_util.LokiReplayGRPCMetadataKey, "true"))

	ctx = injectReplayHeaderFromGRPCMetadata(ctx)

	require.Equal(t, "true", httpreq.ExtractHeader(ctx, httpreq.AdaptiveTelemetryReplayHeader))
}
