package validation

import (
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/util/flagext"
)

type OverridesExporter struct {
	tenantLimits TenantLimits
	description  *prometheus.Desc
}

// TODO(jordanrushing): break out overrides from defaults?
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
	var metricValue float64
	var metricLabelValue string
	var rv reflect.Value

	for tenant, limits := range oe.TenantLimitMap() {
		rv = reflect.ValueOf(limits).Elem()
		for i := 0; i < rv.NumField(); i++ {
			switch rv.Field(i).Interface().(type) {
			case int, time.Duration:
				metricValue = float64(rv.Field(i).Int())
			case model.Duration:
				metricValue = float64(rv.Field(i).Interface().(model.Duration))
			case flagext.ByteSize:
				metricValue = float64(rv.Field(i).Uint())
			case float64:
				metricValue = rv.Field(i).Float()
			default:
				continue
			}
			metricLabelValue = rv.Type().Field(i).Tag.Get("yaml")

			ch <- prometheus.MustNewConstMetric(oe.description, prometheus.GaugeValue, metricValue, metricLabelValue, tenant)
		}
	}
}
