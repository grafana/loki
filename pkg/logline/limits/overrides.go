package limits

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/validation"
)

// maxLimits reports the maximum of each limit across all tenants. Values are
// computed on every call so runtime config reloads apply without a listener.
// Callers read them once per scheduling cycle, so the iteration is cheap.
type maxLimits struct {
	inner *validation.Overrides

	retentionGauge prometheus.Gauge
}

// NewMaxLimits returns Limits backed by Loki's overrides. Each value is the
// maximum across all tenants because logline indexes are shared across
// tenants.
func NewMaxLimits(ov *validation.Overrides, reg prometheus.Registerer) Limits {
	return &maxLimits{
		inner: ov,
		retentionGauge: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "logline_limits_retention_period_seconds",
			Help: "Maximum retention_period across all tenants, in seconds. Zero means unlimited.",
		}),
	}
}

// RetentionPeriod returns the longest retention period across tenants. The
// default comes from limits_config; per-tenant overrides can only increase it.
func (m *maxLimits) RetentionPeriod() time.Duration {
	rp := m.computeMax(
		func(l *validation.Limits) time.Duration { return time.Duration(l.RetentionPeriod) },
		m.inner.RetentionPeriod(""),
	)
	m.retentionGauge.Set(rp.Seconds())
	return rp
}

// computeMax returns the maximum of defaultVal and the field extracted by fn
// from every tenant's limits. Per-tenant zero values are skipped (treated as
// "use the default"). The result is always at least defaultVal.
func (m *maxLimits) computeMax(fn func(*validation.Limits) time.Duration, defaultVal time.Duration) time.Duration {
	maxVal := defaultVal
	for _, l := range m.inner.AllByUserID() {
		if l == nil {
			continue
		}
		if v := fn(l); v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}
