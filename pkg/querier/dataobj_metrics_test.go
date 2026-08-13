package querier

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestDataObjMetrics_record(t *testing.T) {
	fetched := func(m *dataObjMetrics, component string) float64 {
		return testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(component))
	}
	processed := func(m *dataObjMetrics, component string) float64 {
		return testutil.ToFloat64(m.processedUncompressedBytes.WithLabelValues(component))
	}

	t.Run("attributes fetched and processed bytes to each component", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		// metastore: fetched only (index reads decode no log rows).
		_, meta := xcap.StartRegion(ctx, "metastore.Sections")
		meta.Record(dataobj.StatObjectBytesDownloaded.Observe(100))

		_, streams := xcap.StartRegion(ctx, dataObjComponentStreamsReader)
		streams.Record(dataobj.StatObjectBytesDownloaded.Observe(30))
		streams.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(7))

		_, logsReader := xcap.StartRegion(ctx, logs.RegionRead)
		logsReader.Record(dataobj.StatObjectBytesDownloaded.Observe(20))
		logsReader.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(200))
		logsReader.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(5))

		m := newDataObjMetrics(nil)
		m.record(capture)

		require.Equal(t, 100.0, fetched(m, dataObjComponentMetastore))
		require.Equal(t, 30.0, fetched(m, dataObjComponentStreamsReader))
		require.Equal(t, 20.0, fetched(m, dataObjComponentLogsReader))
		require.Zero(t, processed(m, dataObjComponentMetastore))
		require.Equal(t, 7.0, processed(m, dataObjComponentStreamsReader))
		require.Equal(t, 205.0, processed(m, dataObjComponentLogsReader)) // primary + secondary row bytes
	})

	t.Run("nested regions roll up to their root component", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		// The metastore's index reader opens nested streams/pointers regions. Their bytes must attribute to
		// "metastore" (their root), not to "streams-reader".
		metaCtx, meta := xcap.StartRegion(ctx, "metastore.Sections")
		meta.Record(dataobj.StatObjectBytesDownloaded.Observe(40))
		_, nested := xcap.StartRegion(metaCtx, "streams.Reader.Read")
		nested.Record(dataobj.StatObjectBytesDownloaded.Observe(60))
		nested.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(9))

		m := newDataObjMetrics(nil)
		m.record(capture)

		require.Equal(t, 100.0, fetched(m, dataObjComponentMetastore), "the root and its nested region sum under metastore")
		require.Equal(t, 9.0, processed(m, dataObjComponentMetastore))
		require.Zero(t, fetched(m, dataObjComponentStreamsReader), "the nested streams region must not leak into streams-reader")
		require.Zero(t, fetched(m, dataObjComponentOther), "the nested region must roll up to its root, not scatter into other")
	})

	t.Run("unrecognized root region falls into other", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)
		_, region := xcap.StartRegion(ctx, "something.else")
		region.Record(dataobj.StatObjectBytesDownloaded.Observe(11))

		m := newDataObjMetrics(nil)
		m.record(capture)

		require.Equal(t, 11.0, fetched(m, dataObjComponentOther))
	})

	t.Run("nil capture and nil metrics are no-ops", func(t *testing.T) {
		m := newDataObjMetrics(nil)
		require.NotPanics(t, func() { m.record(nil) })

		_, capture := xcap.NewCapture(context.Background(), nil)
		var nilMetrics *dataObjMetrics
		require.NotPanics(t, func() { nilMetrics.record(capture) })
	})
}
