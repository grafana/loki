package xcap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestXcapSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("xcap-test")

	ctx, capture := NewCapture(context.Background(), nil)
	pagesScanned := NewStatisticInt64("pages.scanned", AggregationTypeSum)
	rowsRead := NewStatisticInt64("rows.read", AggregationTypeSum)

	ctx, span1 := StartSpan(ctx, tracer, "DataObjScan",
		trace.WithAttributes(attribute.Int("num_targets", 5)),
	)

	span1.Record(pagesScanned.Observe(3)) // record directly to span

	// Region is retrievable from context.
	region1 := RegionFromContext(ctx)
	require.NotNil(t, region1)
	require.Same(t, span1.Region(), region1)

	// Record observations into the region.
	region1.Record(pagesScanned.Observe(7))
	region1.Record(rowsRead.Observe(100))

	ctx, span2 := StartSpan(ctx, tracer, "RangeAggregation") // child of first span

	region2 := RegionFromContext(ctx)
	require.NotNil(t, region2)
	require.Same(t, span2.Region(), region2)

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

func TestContextWithSpan(t *testing.T) {
	tp := noop.NewTracerProvider()

	t.Run("inject span and region into context", func(t *testing.T) {
		// create xcap span with a linked region.
		ctx, _ := NewCapture(context.Background(), nil)
		_, span := StartSpan(ctx, tp.Tracer("xcap-test"), "op")
		defer span.End()

		// Inject via ContextWithSpan into a fresh context (no prior region).
		fresh := ContextWithSpan(context.Background(), span)

		// Both the span and the region should be retrievable.
		require.Equal(t, span, trace.SpanFromContext(fresh))
		region := RegionFromContext(fresh)
		require.Equal(t, span.Region(), region)
	})

	t.Run("inject span only for nil capture", func(t *testing.T) {
		// StartSpan without a Capture produces an *xcap.Span with nil region.
		_, span := StartSpan(context.Background(), tp.Tracer("xcap-test"), "no-capture")
		defer span.End()

		fresh := ContextWithSpan(context.Background(), span)

		require.Equal(t, span, trace.SpanFromContext(fresh))
		require.Nil(t, RegionFromContext(fresh), "no region should be injected when span has nil region")
	})

	t.Run("inject should work for plain otel span", func(t *testing.T) {
		// Use a plain OTel span.
		_, span := tp.Tracer("xcap-test").Start(context.Background(), "plain")
		defer span.End()

		fresh := ContextWithSpan(context.Background(), span)

		require.Equal(t, span, trace.SpanFromContext(fresh))
		require.Nil(t, RegionFromContext(fresh), "no region should be injected for otel span")
	})
}

// attrMap builds a lookup from attribute key to value for easier assertion.
func attrMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(attrs))
	for _, a := range attrs {
		m[string(a.Key)] = a.Value
	}
	return m
}
