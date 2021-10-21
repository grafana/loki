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
	// TODO: Add limits. . .
	for tenant, limits := range oe.TenantLimitMap() {
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, limits.IngestionRateMB, "ingestion_rate_mb", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, limits.IngestionBurstSizeMB, "ingestion_burst_size_mb", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxQuerySeries), "max_query_series", tenant)
		ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, float64(limits.MaxChunksPerQuery), "max_chunks_per_query", tenant)
	}
}
