package xcap

import (
	"context"
	"fmt"
	"testing"
)

func BenchmarkCaptureUnmarshalBinary(b *testing.B) {
	capture := benchmarkCapture(b)
	data, err := capture.MarshalBinary()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for b.Loop() {
		decoded := &Capture{}
		if err := decoded.UnmarshalBinary(data); err != nil {
			b.Fatal(err)
		}
		benchmarkRegions = len(decoded.Regions())
	}
}

func BenchmarkCaptureMerge(b *testing.B) {
	src := benchmarkCapture(b)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, dst := NewCapture(context.Background(), nil)
		dst.Merge(nil, src)
		benchmarkRegions = len(dst.Regions())
	}
}

func BenchmarkCaptureValue(b *testing.B) {
	capture := benchmarkCapture(b)
	stat := NewStatisticInt64("benchmark.sum", AggregationTypeSum)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		benchmarkValue = Value[int64](capture, stat)
	}
}

var (
	benchmarkRegions int
	benchmarkValue   int64
)

func benchmarkCapture(b *testing.B) *Capture {
	b.Helper()

	ctx, capture := NewCapture(context.Background(), nil)
	sum := NewStatisticInt64("benchmark.sum", AggregationTypeSum)
	min := NewStatisticFloat64("benchmark.min", AggregationTypeMin)
	flag := NewStatisticFlag("benchmark.flag")

	for i := range 12 {
		var region *Region
		ctx, region = StartRegion(ctx, fmt.Sprintf("region-%d", i))
		for j := range 32 {
			region.Record(sum.Observe(int64(i + j)))
			region.Record(min.Observe(float64(i + j)))
			region.Record(flag.Observe(j%2 == 0))
		}
		region.End()
	}
	capture.End()

	return capture
}
