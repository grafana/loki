package util

import (
	"context"
	"testing"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

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
