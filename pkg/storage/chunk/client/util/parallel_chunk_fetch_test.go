package util

import (
	"context"
	"testing"

	"github.com/grafana/loki/pkg/storage/chunk/encoding"
)

func BenchmarkGetParallelChunks(b *testing.B) {
	ctx := context.Background()
	in := make([]encoding.Chunk, 1024)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := GetParallelChunks(ctx, 150, in,
			func(_ context.Context, d *encoding.DecodeContext, c encoding.Chunk) (encoding.Chunk, error) {
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
