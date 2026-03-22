package validation

import (
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type ExportedLimits interface {
	AllByUserID() map[string]*Limits
	DefaultLimits() *Limits
}

type OverridesExporter struct {
	overrides    ExportedLimits
	tenantDesc   *prometheus.Desc
	defaultsDesc *prometheus.Desc
}

// TODO(jordanrushing): break out overrides from defaults?
func NewOverridesExporter(overrides ExportedLimits) *OverridesExporter {
	return &OverridesExporter{
		overrides: overrides,
		tenantDesc: prometheus.NewDesc(
			"loki_overrides",
			"Resource limit overrides applied to tenants",
			[]string{"limit_name", "user"},
			nil,
		),
		defaultsDesc: prometheus.NewDesc(
			"loki_overrides_defaults",
			"Default values for resource limit overrides applied to tenants",
			[]string{"limit_name"},
			nil,
		),
	}
}

func (oe *OverridesExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- oe.tenantDesc
	ch <- oe.defaultsDesc
}

func (oe *OverridesExporter) Collect(ch chan<- prometheus.Metric) {
	extract := func(val reflect.Value, i int) (float64, bool) {
		switch val.Field(i).Interface().(type) {
		case int, time.Duration:
			return float64(val.Field(i).Int()), true
		case model.Duration:
			return float64(val.Field(i).Interface().(model.Duration)), true
		case uint, flagext.ByteSize:
			return float64(val.Field(i).Uint()), true
		case float64:
			return val.Field(i).Float(), true
		case bool:
			v := 0.0
			if val.Field(i).Bool() {
				v = 1.0
			}
			return v, true
		default:
			return 0, false
		}
	}

	defs := reflect.ValueOf(oe.overrides.DefaultLimits()).Elem()

	for i := 0; i < defs.NumField(); i++ {
		if v, ok := extract(defs, i); ok {
			metricLabelValue := defs.Type().Field(i).Tag.Get("yaml")
			ch <- prometheus.MustNewConstMetric(oe.defaultsDesc, prometheus.GaugeValue, v, metricLabelValue)
		}

	}

	for tenant, limits := range oe.overrides.AllByUserID() {
		rv := reflect.ValueOf(limits).Elem()
		for i := 0; i < rv.NumField(); i++ {

			v, ok := extract(rv, i)

			// Only report fields which are explicitly overridden
			if !ok || rv.Field(i).Interface() == defs.Field(i).Interface() {
				continue

			}

			metricLabelValue := rv.Type().Field(i).Tag.Get("yaml")
			ch <- prometheus.MustNewConstMetric(oe.tenantDesc, prometheus.GaugeValue, v, metricLabelValue, tenant)
		}
	}

}
