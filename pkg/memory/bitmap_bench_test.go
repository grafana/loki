package memory_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/memory"
)

func BenchmarkBitmap_Appends(b *testing.B) {
	b.Run("type=AppendCount", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			dst.AppendCount(true, 100_00)
			dst.Resize(0)
		}
	})

	b.Run("type=Append", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			for range 100_000 {
				dst.Append(true)
			}
			dst.Resize(0)
		}
	})

	b.Run("type=AppendValues", func(b *testing.B) {
		values := make([]bool, 100_000)
		for i := range values {
			values[i] = true
		}

		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			dst.AppendValues(values...)
			dst.Resize(0)
		}
	})
}
