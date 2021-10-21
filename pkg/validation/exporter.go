package validation

import (
	"github.com/prometheus/client_golang/prometheus"
)

type OverridesExporter struct {
	tenantLimits TenantLimits
	description  *prometheus.Desc
}

func NewOverridesExporter(tenantLimits TenantLimits) *OverridesExporter {
	return &OverridesExporter{
		tenantLimits: tenantLimits,
		description: prometheus.NewDesc(
			"loki_overrides",
			"Resource limit overrides applied to tenants",
			[]string{"limit_name", "user"},
			nil,
		),
	}
}

func (oe *OverridesExporter) TenantLimitMap() map[string]*Limits {
	m := make(map[string]*Limits)
	oe.tenantLimits.ForEachTenantLimit(func(userID string, limit *Limits) {
		m[userID] = limit
	})

	return m
}

func (oe *OverridesExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- oe.description
}

func (oe *OverridesExporter) Collect(ch chan<- prometheus.Metric) {
	// TODO(jordanrushing): Add more limits. . .
	for tenant, limits := range oe.TenantLimitMap() {
		// Distributor enforced limits
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, limits.IngestionRateMB, "ingestion_rate_mb", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, limits.IngestionBurstSizeMB, "ingestion_burst_size_mb", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxLabelNameLength), "max_label_name_length", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxLabelValueLength), "max_label_value_length", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxLabelNamesPerSeries), "max_label_names_per_series", tenant)
		// Ingester enforced limits
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxLocalStreamsPerUser), "max_local_streams_per_user", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxGlobalStreamsPerUser), "max_global_streams_per_user", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.PerStreamRateLimit), "per_stream_rate_limit", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.PerStreamRateLimitBurst), "per_stream_rate_limit_burst", tenant)
		// Querier enforced limits
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxChunksPerQuery), "max_chunks_per_query", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxQuerySeries), "max_query_series", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxQueryParallelism), "max_query_parallelism", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.CardinalityLimit), "cardinality_limit", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxStreamsMatchersPerQuery), "max_streams_matchers_per_query", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxConcurrentTailRequests), "max_concurrent_tail_requests", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxEntriesLimitPerQuery), "max_entries_limit_per_query", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxQueriersPerTenant), "max_queriers_per_tenant", tenant)
	}
}
