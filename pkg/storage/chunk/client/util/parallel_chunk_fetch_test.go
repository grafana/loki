package util //nolint:revive

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestGetParallelChunksReturnsAllErrors(t *testing.T) {
	fetchErrs := []error{errors.New("first fetch failed"), errors.New("second fetch failed")}
	nextErr := 0

	chunks, err := GetParallelChunks(context.Background(), 1, make([]chunk.Chunk, len(fetchErrs)),
		func(_ context.Context, _ *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
			err := fetchErrs[nextErr]
			nextErr++
			return c, err
		})

	require.Empty(t, chunks)
	require.Equal(t, util.MultiError(fetchErrs), err)
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
