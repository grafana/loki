package dataobjread

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/bucketstat"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestNewMetrics(t *testing.T) {
	t.Run("it creates a series at zero for every component and operation", func(t *testing.T) {
		m := NewMetrics(nil)

		require.Equal(t, 4*3, testutil.CollectAndCount(m.objectStoreRequests))
		require.Equal(t, 4*3, testutil.CollectAndCount(m.objectStoreRequestsFailed))
		require.Equal(t, 4, testutil.CollectAndCount(m.fetchedCompressedBytes))
		require.Equal(t, 4*4, testutil.CollectAndCount(m.metadataBytes))
		require.Equal(t, 4*4, testutil.CollectAndCount(m.metadataRequests))
		metadataSeries := [][2]string{{"cache", "prefetch"}, {"storage", "prefetch"}, {"cache", "on_demand"}, {"storage", "on_demand"}}
		for _, component := range []string{"metastore", "streams-reader", "logs-reader", "other"} {
			require.Zero(t, testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(component)))
			for _, series := range metadataSeries {
				require.Zero(t, testutil.ToFloat64(m.metadataBytes.WithLabelValues(component, series[0], series[1])))
				require.Zero(t, testutil.ToFloat64(m.metadataRequests.WithLabelValues(component, series[0], series[1])))
			}
			for _, operation := range []string{"attributes", "get", "get_range"} {
				require.Zero(t, testutil.ToFloat64(m.objectStoreRequests.WithLabelValues(component, operation)))
				require.Zero(t, testutil.ToFloat64(m.objectStoreRequestsFailed.WithLabelValues(component, operation)))
			}
		}
	})
}

func TestMetrics_Record(t *testing.T) {
	requests := func(m *Metrics, component, operation string) float64 {
		return testutil.ToFloat64(m.objectStoreRequests.WithLabelValues(component, operation))
	}
	failed := func(m *Metrics, component, operation string) float64 {
		return testutil.ToFloat64(m.objectStoreRequestsFailed.WithLabelValues(component, operation))
	}
	fetched := func(m *Metrics, component string) float64 {
		return testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(component))
	}
	metadataBytes := func(m *Metrics, component, source, phase string) float64 {
		return testutil.ToFloat64(m.metadataBytes.WithLabelValues(component, source, phase))
	}
	metadataRequests := func(m *Metrics, component, source, phase string) float64 {
		return testutil.ToFloat64(m.metadataRequests.WithLabelValues(component, source, phase))
	}

	t.Run("it attributes requests and bytes to the component of each root region", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, metastoreRegion := xcap.StartRegion(ctx, "metastore.Sections")
		metastoreRegion.Record(bucketstat.StatObjectRequestsGet.Observe(1))
		metastoreRegion.Record(bucketstat.StatObjectRequestsGetRange.Observe(3))
		metastoreRegion.Record(bucketstat.StatObjectBytesDownloaded.Observe(100))

		_, streamsRegion := xcap.StartRegion(ctx, regionStreamsReader)
		streamsRegion.Record(bucketstat.StatObjectRequestsAttributes.Observe(2))
		streamsRegion.Record(bucketstat.StatObjectBytesDownloaded.Observe(30))

		_, logsRegion := xcap.StartRegion(ctx, logs.RegionRead)
		logsRegion.Record(bucketstat.StatObjectRequestsGetRange.Observe(5))
		logsRegion.Record(bucketstat.StatObjectBytesDownloaded.Observe(20))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 1.0, requests(m, "metastore", "get"))
		require.Equal(t, 3.0, requests(m, "metastore", "get_range"))
		require.Zero(t, requests(m, "metastore", "attributes"))
		require.Equal(t, 2.0, requests(m, "streams-reader", "attributes"))
		require.Equal(t, 5.0, requests(m, "logs-reader", "get_range"))
		require.Equal(t, 100.0, fetched(m, "metastore"))
		require.Equal(t, 30.0, fetched(m, "streams-reader"))
		require.Equal(t, 20.0, fetched(m, "logs-reader"))
		require.Zero(t, fetched(m, "other"))
	})

	t.Run("it counts a failed request in the request total and in the failed counter", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region := xcap.StartRegion(ctx, "metastore.Sections")
		region.Record(bucketstat.StatObjectRequestsGet.Observe(3))
		region.Record(bucketstat.StatObjectRequestFailuresGet.Observe(1))
		region.Record(bucketstat.StatObjectRequestsGetRange.Observe(2))
		region.Record(bucketstat.StatObjectRequestFailuresGetRange.Observe(2))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 3.0, requests(m, "metastore", "get"))
		require.Equal(t, 1.0, failed(m, "metastore", "get"))
		require.Equal(t, 2.0, requests(m, "metastore", "get_range"))
		require.Equal(t, 2.0, failed(m, "metastore", "get_range"), "an operation whose requests all failed still counts them")
		require.Zero(t, failed(m, "metastore", "attributes"))
	})

	t.Run("it adds no failed request when every request succeeded", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region := xcap.StartRegion(ctx, "metastore.Sections")
		region.Record(bucketstat.StatObjectRequestsGet.Observe(4))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 4.0, requests(m, "metastore", "get"))
		require.Zero(t, failed(m, "metastore", "get"))
	})

	t.Run("it attributes a failed request to the component of its root region", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, streamsRegion := xcap.StartRegion(ctx, regionStreamsReader)
		streamsRegion.Record(bucketstat.StatObjectRequestsGetRange.Observe(2))
		streamsRegion.Record(bucketstat.StatObjectRequestFailuresGetRange.Observe(1))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 1.0, failed(m, "streams-reader", "get_range"))
		require.Zero(t, failed(m, "metastore", "get_range"))
		require.Zero(t, failed(m, "logs-reader", "get_range"))
	})

	t.Run("it adds up the regions of one component", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, first := xcap.StartRegion(ctx, "metastore.Sections")
		first.Record(bucketstat.StatObjectRequestsGetRange.Observe(2))
		first.Record(bucketstat.StatObjectBytesDownloaded.Observe(10))
		_, second := xcap.StartRegion(ctx, "metastore.GetIndexes")
		second.Record(bucketstat.StatObjectRequestsGetRange.Observe(4))
		second.Record(bucketstat.StatObjectBytesDownloaded.Observe(5))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 6.0, requests(m, "metastore", "get_range"))
		require.Equal(t, 15.0, fetched(m, "metastore"))
	})

	t.Run("a nested region counts for the component of its root, whatever its own name", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		metastoreCtx, root := xcap.StartRegion(ctx, "metastore.Sections")
		root.Record(bucketstat.StatObjectBytesDownloaded.Observe(40))
		nestedCtx, nested := xcap.StartRegion(metastoreCtx, "streams.Reader.Read")
		nested.Record(bucketstat.StatObjectRequestsGetRange.Observe(4))
		nested.Record(bucketstat.StatObjectBytesDownloaded.Observe(60))
		_, deeper := xcap.StartRegion(nestedCtx, "pointers.Reader.Read")
		deeper.Record(bucketstat.StatObjectRequestsGet.Observe(1))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 4.0, requests(m, "metastore", "get_range"))
		require.Equal(t, 1.0, requests(m, "metastore", "get"))
		require.Equal(t, 100.0, fetched(m, "metastore"))
		require.Zero(t, requests(m, "streams-reader", "get_range"), "the nested streams region must not count as the streams reader")
		require.Zero(t, fetched(m, "other"), "a nested region must follow its root, not land in other")
	})

	t.Run("it attributes the metadata reads to the component of each root region", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(t.Context(), nil)

		_, streamsRegion := xcap.StartRegion(ctx, regionStreamsReader)
		streamsRegion.Record(metadata.StatMetadataBytesFromStorage.Observe(100))
		streamsRegion.Record(metadata.StatMetadataRequestsToStorage.Observe(2))
		streamsRegion.Record(metadata.StatMetadataBytesFromCache.Observe(40))
		streamsRegion.Record(metadata.StatMetadataRequestsToCache.Observe(4))
		streamsRegion.Record(metadata.StatMetadataPrefetchBytesFromCache.Observe(300))
		streamsRegion.Record(metadata.StatMetadataPrefetchRequestsToCache.Observe(1))
		streamsRegion.Record(metadata.StatMetadataPrefetchBytesFromStorage.Observe(70))
		streamsRegion.Record(metadata.StatMetadataPrefetchRequestsToStorage.Observe(2))

		_, logsRegion := xcap.StartRegion(ctx, logs.RegionRead)
		logsRegion.Record(metadata.StatMetadataRequestsToStorage.Observe(0))
		logsRegion.Record(metadata.StatMetadataPrefetchRequestsToCache.Observe(1))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 100.0, metadataBytes(m, "streams-reader", "storage", "on_demand"))
		require.Equal(t, 2.0, metadataRequests(m, "streams-reader", "storage", "on_demand"))
		require.Equal(t, 40.0, metadataBytes(m, "streams-reader", "cache", "on_demand"))
		require.Equal(t, 4.0, metadataRequests(m, "streams-reader", "cache", "on_demand"))
		require.Equal(t, 300.0, metadataBytes(m, "streams-reader", "cache", "prefetch"))
		require.Equal(t, 1.0, metadataRequests(m, "streams-reader", "cache", "prefetch"))
		require.Equal(t, 70.0, metadataBytes(m, "streams-reader", "storage", "prefetch"))
		require.Equal(t, 2.0, metadataRequests(m, "streams-reader", "storage", "prefetch"))
		require.Zero(t, metadataBytes(m, "logs-reader", "storage", "on_demand"))
		require.Zero(t, metadataRequests(m, "logs-reader", "storage", "on_demand"))
		require.Zero(t, metadataRequests(m, "logs-reader", "cache", "on_demand"))
		require.Zero(t, metadataBytes(m, "logs-reader", "cache", "prefetch"))
		require.Equal(t, 1.0, metadataRequests(m, "logs-reader", "cache", "prefetch"))
		require.Zero(t, metadataBytes(m, "logs-reader", "storage", "prefetch"))
		require.Zero(t, metadataRequests(m, "logs-reader", "storage", "prefetch"))
		require.Zero(t, metadataBytes(m, "metastore", "cache", "prefetch"))
		require.Equal(t, 4*4, testutil.CollectAndCount(m.metadataBytes), "the bytes that a prefetched range served have no series, because the prefetch phase counted them")
	})

	t.Run("it adds the metadata reads of a nested region to its root", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(t.Context(), nil)

		metastoreCtx, root := xcap.StartRegion(ctx, "metastore.Sections")
		_, nested := xcap.StartRegion(metastoreCtx, "streams.Reader.Read")
		nested.Record(metadata.StatMetadataBytesFromStorage.Observe(5))
		nested.Record(metadata.StatMetadataRequestsToStorage.Observe(1))
		root.Record(metadata.StatMetadataPrefetchBytesFromCache.Observe(300))
		root.Record(metadata.StatMetadataPrefetchRequestsToCache.Observe(1))
		nested.Record(metadata.StatMetadataPrefetchBytesFromCache.Observe(40))
		nested.Record(metadata.StatMetadataPrefetchRequestsToCache.Observe(2))
		nested.Record(metadata.StatMetadataBytesFromCache.Observe(9))
		nested.Record(metadata.StatMetadataRequestsToCache.Observe(1))
		nested.Record(metadata.StatMetadataPrefetchBytesFromStorage.Observe(70))
		nested.Record(metadata.StatMetadataPrefetchRequestsToStorage.Observe(2))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 5.0, metadataBytes(m, "metastore", "storage", "on_demand"))
		require.Equal(t, 1.0, metadataRequests(m, "metastore", "storage", "on_demand"))
		require.Equal(t, 340.0, metadataBytes(m, "metastore", "cache", "prefetch"))
		require.Equal(t, 3.0, metadataRequests(m, "metastore", "cache", "prefetch"))
		require.Equal(t, 70.0, metadataBytes(m, "metastore", "storage", "prefetch"))
		require.Equal(t, 2.0, metadataRequests(m, "metastore", "storage", "prefetch"))
		require.Equal(t, 9.0, metadataBytes(m, "metastore", "cache", "on_demand"))
		require.Equal(t, 1.0, metadataRequests(m, "metastore", "cache", "on_demand"))
		require.Zero(t, metadataBytes(m, "streams-reader", "cache", "prefetch"))
	})

	t.Run("a root region with an unknown name counts as other", func(t *testing.T) {
		ctx, capture := xcap.NewCapture(context.Background(), nil)

		_, region := xcap.StartRegion(ctx, "something.else")
		region.Record(bucketstat.StatObjectRequestsGet.Observe(2))
		region.Record(bucketstat.StatObjectBytesDownloaded.Observe(11))

		m := NewMetrics(nil)
		m.Record(capture)

		require.Equal(t, 2.0, requests(m, "other", "get"))
		require.Equal(t, 11.0, fetched(m, "other"))
	})

	t.Run("a nil capture and a nil receiver do nothing", func(t *testing.T) {
		_, capture := xcap.NewCapture(context.Background(), nil)

		require.NotPanics(t, func() { NewMetrics(nil).Record(nil) })
		require.NotPanics(t, func() { (*Metrics)(nil).Record(capture) })
	})
}
