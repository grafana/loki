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

var (
	benchmarkRegions int
)

func benchmarkCapture(b *testing.B) *Capture {
	b.Helper()

	ctx, capture := NewCapture(context.Background(), nil)

	for r := range 4 {
		var region *Region
		ctx, region = StartRegion(ctx, fmt.Sprintf("region-%d", r))
		for s := range 32 {
			stat := NewStatisticInt64(fmt.Sprintf("stat.%d", s), AggregationTypeSum)
			region.Record(stat.Observe(int64(s+1) << 20))
		}
		region.End()
	}
	capture.End()

	return capture
}
