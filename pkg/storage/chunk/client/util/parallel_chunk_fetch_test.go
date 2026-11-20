package util //nolint:revive

import (
	"bytes"
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestGetParallelChunks_CanceledQueryNotLoggedAsError(t *testing.T) {
	var buf bytes.Buffer
	defer func(l log.Logger) { util_log.Logger = l }(util_log.Logger)
	util_log.Logger = log.NewLogfmtLogger(&buf)

	ctx, cancel := context.WithCancel(context.Background())
	_, err := GetParallelChunks(ctx, 1, make([]chunk.Chunk, 1),
		func(ctx context.Context, _ *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
			cancel()
			return c, ctx.Err()
		})

	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, buf.String(), "level=debug")
	require.NotContains(t, buf.String(), "level=error")
}

func BenchmarkGetParallelChunks(b *testing.B) {
	ctx := context.Background()
	in := make([]chunk.Chunk, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := GetParallelChunks(ctx, 150, in,
			func(_ context.Context, _ *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
				return c, nil
			})
		if err != nil {
			b.Fatal(err)
		}
		if len(res) != len(in) {
			b.Fatal("unexpected number of chunk returned")
		}
	}
}
