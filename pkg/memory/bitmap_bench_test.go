package memory_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/memory"
)

func BenchmarkBitmap_PassAsValue(b *testing.B) {
	var bmap memory.Bitmap

	for b.Loop() {
		_ = identity(bmap)
	}
}

func identity[T any](v T) T { return v }

func BenchmarkBitmap_Appends(b *testing.B) {
	b.Run("method=AppendCount", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			dst.AppendCount(true, 100_00)
			dst.Resize(0)
		}
	})

	b.Run("method=Append", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			for range 100_000 {
				dst.Append(true)
			}
			dst.Resize(0)
		}
	})

	b.Run("method=AppendValues", func(b *testing.B) {
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

func BenchmarkBitmap_Access(b *testing.B) {
	b.Run("method=Set", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.Resize(100_000)

		for b.Loop() {
			for i := range 100_000 {
				dst.Set(i, true)
			}
		}
	})

	b.Run("method=Get", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.AppendCount(true, 100_000)

		for b.Loop() {
			for i := range 100_000 {
				_ = dst.Get(i)
			}
		}
	})
}

func BenchmarkBitmap_BulkOperations(b *testing.B) {
	b.Run("method=SetRange", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.Resize(100_000)

		for b.Loop() {
			dst.SetRange(0, 100_000, true)
		}
	})

	b.Run("method=AppendBitmap", func(b *testing.B) {
		src := memory.NewBitmap(nil, 100_000)
		src.AppendCount(true, 100_000)

		dst := memory.NewBitmap(nil, 100_000)

		for b.Loop() {
			dst.AppendBitmap(src)
			dst.Resize(0)
		}
	})
}

func BenchmarkBitmap_Counting(b *testing.B) {
	b.Run("method=SetCount/density=sparse", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.Resize(100_000)
		// Set every 10th bit
		for i := 0; i < 100_000; i += 10 {
			dst.Set(i, true)
		}

		for b.Loop() {
			_ = dst.SetCount()
		}
	})

	b.Run("method=SetCount/density=dense", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.AppendCount(true, 100_000)

		for b.Loop() {
			_ = dst.SetCount()
		}
	})

	b.Run("method=ClearCount", func(b *testing.B) {
		dst := memory.NewBitmap(nil, 100_000)
		dst.AppendCount(true, 100_000)

		for b.Loop() {
			_ = dst.ClearCount()
		}
	})
}
