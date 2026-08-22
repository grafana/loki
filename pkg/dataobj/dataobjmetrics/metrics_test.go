package dataobjmetrics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestMetrics_Record(t *testing.T) {
	fetched := func(m *Metrics, component string) float64 {
		return testutil.ToFloat64(m.FetchedCompressedBytes.WithLabelValues(component))
	}
	processed := func(m *Metrics, component string) float64 {
		return testutil.ToFloat64(m.ProcessedUncompressedBytes.WithLabelValues(component))
	}
	requests := func(m *Metrics, component, operation string) float64 {
		return testutil.ToFloat64(m.ObjectStoreRequests.WithLabelValues(component, operation))
	}

	t.Run("attributes fetched and processed bytes to each component", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		// metastore: fetched only (index reads decode no log rows).
		_, meta := xcap.StartRegion(ctx, "metastore.Sections")
		meta.Record(dataobj.StatObjectBytesDownloaded.Observe(100))

		_, streams := xcap.StartRegion(ctx, ComponentStreamsReader)
		streams.Record(dataobj.StatObjectBytesDownloaded.Observe(30))
		streams.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(7))

		_, logsReader := xcap.StartRegion(ctx, logs.RegionRead)
		logsReader.Record(dataobj.StatObjectBytesDownloaded.Observe(20))
		logsReader.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(200))
		logsReader.Record(dataobj.StatDatasetSecondaryRowBytes.Observe(5))

		m := New(nil)
		m.Record(capture)

		require.Equal(t, 100.0, fetched(m, ComponentMetastore))
		require.Equal(t, 30.0, fetched(m, ComponentStreamsReader))
		require.Equal(t, 20.0, fetched(m, ComponentLogsReader))
		require.Zero(t, processed(m, ComponentMetastore))
		require.Equal(t, 7.0, processed(m, ComponentStreamsReader))
		require.Equal(t, 205.0, processed(m, ComponentLogsReader)) // primary + secondary row bytes
	})

	t.Run("object-store requests attribute by component and operation", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		// metastore: an Attributes HEAD plus several range reads.
		_, meta := xcap.StartRegion(ctx, "metastore.Sections")
		meta.Record(dataobj.StatObjectRequestsAttributes.Observe(1))
		meta.Record(dataobj.StatObjectRequestsGetRange.Observe(3))

		_, streams := xcap.StartRegion(ctx, ComponentStreamsReader)
		streams.Record(dataobj.StatObjectRequestsGet.Observe(2))

		m := New(nil)
		m.Record(capture)

		require.Equal(t, 1.0, requests(m, ComponentMetastore, OperationAttributes))
		require.Equal(t, 3.0, requests(m, ComponentMetastore, OperationGetRange))
		require.Zero(t, requests(m, ComponentMetastore, OperationGet))
		require.Equal(t, 2.0, requests(m, ComponentStreamsReader, OperationGet))
		require.Zero(t, requests(m, ComponentOther, OperationGet))
	})

	t.Run("nested regions roll up to their root component", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		// The metastore's index reader opens nested streams/pointers regions. Their bytes and requests
		// must attribute to "metastore" (their root), not to "streams-reader".
		metaCtx, meta := xcap.StartRegion(ctx, "metastore.Sections")
		meta.Record(dataobj.StatObjectBytesDownloaded.Observe(40))
		_, nested := xcap.StartRegion(metaCtx, "streams.Reader.Read")
		nested.Record(dataobj.StatObjectBytesDownloaded.Observe(60))
		nested.Record(dataobj.StatDatasetPrimaryRowBytes.Observe(9))
		nested.Record(dataobj.StatObjectRequestsGetRange.Observe(4))

		m := New(nil)
		m.Record(capture)

		require.Equal(t, 100.0, fetched(m, ComponentMetastore), "the root and its nested region sum under metastore")
		require.Equal(t, 9.0, processed(m, ComponentMetastore))
		require.Equal(t, 4.0, requests(m, ComponentMetastore, OperationGetRange), "the nested region's requests roll up to metastore")
		require.Zero(t, fetched(m, ComponentStreamsReader), "the nested streams region must not leak into streams-reader")
		require.Zero(t, fetched(m, ComponentOther), "the nested region must roll up to its root, not scatter into other")
	})

	t.Run("unrecognized root region falls into other", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)
		_, region := xcap.StartRegion(ctx, "something.else")
		region.Record(dataobj.StatObjectBytesDownloaded.Observe(11))

		m := New(nil)
		m.Record(capture)

		require.Equal(t, 11.0, fetched(m, ComponentOther))
	})

	t.Run("nil capture and nil metrics are no-ops", func(t *testing.T) {
		m := New(nil)
		require.NotPanics(t, func() { m.Record(nil) })

		_, capture := xcap.NewCapture(context.Background(), nil)
		var nilMetrics *Metrics
		require.NotPanics(t, func() { nilMetrics.Record(capture) })
	})
}
