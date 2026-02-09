package xcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTracer(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := NewTracer(tp.Tracer("xcap-test"))

	ctx, capture := NewCapture(context.Background(), nil)
	pagesScanned := NewStatisticInt64("pages.scanned", AggregationTypeSum)
	rowsRead := NewStatisticInt64("rows.read", AggregationTypeSum)

	ctx, span1 := tracer.Start(ctx, "DataObjScan",
		trace.WithAttributes(attribute.Int("num_targets", 5)),
	)

	// The returned span must be an *xcap.Span.
	xcapSpan1, ok := span1.(*Span)
	require.True(t, ok, "span should be *xcap.Span")

	// Region is retrievable from context.
	region1 := RegionFromContext(ctx)
	require.NotNil(t, region1)
	require.Same(t, xcapSpan1.Region(), region1)

	// Record observations into the region.
	region1.Record(pagesScanned.Observe(3))
	region1.Record(pagesScanned.Observe(7))
	region1.Record(rowsRead.Observe(100))

	ctx, span2 := tracer.Start(ctx, "RangeAggregation") // child of first span
	xcapSpan2, ok := span2.(*Span)
	require.True(t, ok, "span should be *xcap.Span")

	region2 := RegionFromContext(ctx)
	require.NotNil(t, region2)
	require.Same(t, xcapSpan2.Region(), region2)

	region2.Record(rowsRead.Observe(50))

	// End spans (child first).
	span2.End()
	span1.End()

	// Verify regions on the capture
	regions := capture.Regions()
	require.Len(t, regions, 2)

	regionsByName := make(map[string]*Region, len(regions))
	for _, r := range regions {
		regionsByName[r.name] = r
	}
	require.Contains(t, regionsByName, "DataObjScan")
	require.Contains(t, regionsByName, "RangeAggregation")

	// Observations should still be readable on the regions.
	scanObs := regionsByName["DataObjScan"].Observations()
	require.Len(t, scanObs, 2, "DataObjScan should have two distinct statistics")

	rangeObs := regionsByName["RangeAggregation"].Observations()
	require.Len(t, rangeObs, 1, "RangeAggregation should have one statistic")

	// Verify OTel span attributes via the recorder
	ended := recorder.Ended()
	require.Len(t, ended, 2)

	// Spans are recorded in end order: RangeAggregation first, then DataObjScan.
	spansByName := make(map[string]sdktrace.ReadOnlySpan, len(ended))
	for _, s := range ended {
		spansByName[s.Name()] = s
	}

	// DataObjScan span should have the initial attribute plus flushed observations.
	scanSpan := spansByName["DataObjScan"]
	require.NotNil(t, scanSpan)
	scanAttrs := attrMap(scanSpan.Attributes())
	require.Equal(t, int64(5), scanAttrs["num_targets"].AsInt64(), "initial attribute should be present")
	require.Equal(t, int64(10), scanAttrs["pages.scanned"].AsInt64(), "pages.scanned should be 3+7=10")
	require.Equal(t, int64(100), scanAttrs["rows.read"].AsInt64(), "rows.read should be 100")

	// RangeAggregation span should have its flushed observation.
	rangeSpan := spansByName["RangeAggregation"]
	require.NotNil(t, rangeSpan)
	rangeAttrs := attrMap(rangeSpan.Attributes())
	require.Equal(t, int64(50), rangeAttrs["rows.read"].AsInt64(), "rows.read should be 50")
}

// attrMap builds a lookup from attribute key to value for easier assertion.
func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value
	}
	return m
}
