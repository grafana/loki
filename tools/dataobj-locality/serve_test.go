package main

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestWindowCollectors_RouteTenantAndClass(t *testing.T) {
	sec, closer := buildPostingsSection(t, []postings.LabelObservation{{
		ObjectPath: "/logs/a", SectionIndex: 0, ColumnName: "app", LabelValue: "api",
		StreamID: 1, Timestamp: time.Unix(0, 0).UTC(), UncompressedSize: 100,
	}})
	defer closer()

	groups := newWindowCollectors()
	opts := collectorOptions{byNameValue: true}
	sec.Tenant = "tenant-a"
	require.NoError(t, groups.foldSection(context.Background(), sec, "indexes/tenants/tenant-a/abc", 0, opts))
	sec.Tenant = "tenant-b"
	require.NoError(t, groups.foldSection(context.Background(), sec, "indexes/def", 0, opts))

	require.Contains(t, groups.postings, "tenant-a\x00compacted")
	require.Contains(t, groups.postings, "tenant-b\x00uncompacted")
	require.Contains(t, groups.paths["compacted"], "indexes/tenants/tenant-a/abc")
	require.Contains(t, groups.paths["uncompacted"], "indexes/def")
}

func TestLocalityCollector_ProjectsSnapshot(t *testing.T) {
	collector := newLocalityCollector()
	collector.setSnapshot(localitySnapshot{
		windows: map[string]windowScanSnapshot{
			"2026-07-13T00:00:00Z": {
				window: "2026-07-13T00:00:00Z",
				postings: []postingsSnapshot{{
					window: "2026-07-13T00:00:00Z", tenant: "tenant-a", class: "compacted",
					sections: 3, spread: newHistogramSnapshot([]float64{1, 2, 8}, postingsSpreadBuckets),
				}},
				logs: []logsSnapshot{{
					window: "2026-07-13T00:00:00Z", tenant: "tenant-a",
					spread:        newHistogramSnapshot([]float64{1, 2}, logsSpreadBuckets),
					sectionSpread: newHistogramSnapshot([]float64{1, 3}, logsSectionSpreadBuckets),
					logsSections:  4,
					idealSections: 2,
				}},
				objects: map[string]classTotals{
					"compacted": {objects: 2, bytes: 123},
				},
			},
		},
		lastRun: 1, runDuration: 2, windowsScanned: 1,
	})

	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))
	count := testutil.CollectAndCount(collector, "dataobj_locality_postings_section_spread")
	require.Equal(t, 1, count)
	families, err := registry.Gather()
	require.NoError(t, err)

	postings := metricFamily(t, families, "dataobj_locality_postings_section_spread")
	require.Len(t, postings.Metric, 1)
	require.Equal(t, uint64(3), postings.Metric[0].Histogram.GetSampleCount())
	require.Equal(t, 11.0, postings.Metric[0].Histogram.GetSampleSum())

	readAmp := metricFamily(t, families, "dataobj_locality_logs_read_amplification")
	require.Equal(t, 2.0, readAmp.Metric[0].Gauge.GetValue())

	logsSections := metricFamily(t, families, "dataobj_locality_logs_section_spread")
	require.Equal(t, uint64(2), logsSections.Metric[0].Histogram.GetSampleCount())
	require.Equal(t, 4.0, logsSections.Metric[0].Histogram.GetSampleSum())

	objects := metricFamily(t, families, "dataobj_locality_index_objects")
	require.Equal(t, 2.0, objects.Metric[0].Gauge.GetValue())
}

func TestLocalityCollector_ReplacesAndPrunesWindows(t *testing.T) {
	collector := newLocalityCollector()
	first := windowScanSnapshot{
		window: "2026-07-12T00:00:00Z",
		objects: map[string]classTotals{
			"compacted": {objects: 1},
		},
	}
	second := windowScanSnapshot{
		window: "2026-07-13T00:00:00Z",
		objects: map[string]classTotals{
			"uncompacted": {objects: 2},
		},
	}
	collector.setWindow(first.window, first)
	collector.setWindow(second.window, second)
	collector.setWindow(first.window, windowScanSnapshot{
		window:  first.window,
		objects: map[string]classTotals{"uncompacted": {objects: 3}},
	})
	collector.completeScan(map[string]struct{}{first.window: {}}, time.Second)

	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))
	families, err := registry.Gather()
	require.NoError(t, err)

	objects := metricFamily(t, families, "dataobj_locality_index_objects")
	require.Len(t, objects.Metric, 1)
	require.Equal(t, 3.0, objects.Metric[0].Gauge.GetValue())
}

func TestNextTick_UTCAlignment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		now    time.Time
		period time.Duration
		want   time.Time
	}{
		{"three-hours", time.Date(2026, 7, 13, 1, 2, 0, 0, time.UTC), 3 * time.Hour, time.Date(2026, 7, 13, 3, 0, 0, 0, time.UTC)},
		{"at-schedule-point", time.Date(2026, 7, 13, 3, 0, 0, 0, time.UTC), 3 * time.Hour, time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC)},
		{"non-divisor-resets", time.Date(2026, 7, 13, 23, 30, 0, 0, time.UTC), 5 * time.Hour, time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, nextTick(tc.now, tc.period))
		})
	}
}

func metricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}
