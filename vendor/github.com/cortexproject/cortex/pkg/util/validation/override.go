package validation

import (
	"os"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	yaml "gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util"
)

var overridesReloadSuccess = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cortex_overrides_last_reload_successful",
	Help: "Whether the last overrides reload attempt was successful.",
})

func init() {
	overridesReloadSuccess.Set(1) // Default to 1
}

// When we load YAML from disk, we want the various per-customer limits
// to default to any values specified on the command line, not default
// command line values.  This global contains those values.  I (Tom) cannot
// find a nicer way I'm afraid.
var defaultLimits Limits

// Overrides periodically fetch a set of per-user overrides, and provides convenience
// functions for fetching the correct value.
type Overrides struct {
	Defaults     Limits
	overridesMtx sync.RWMutex
	overrides    map[string]*Limits
	quit         chan struct{}
}

// NewOverrides makes a new Overrides.
// We store the supplied limits in a global variable to ensure per-tenant limits
// are defaulted to those values.  As such, the last call to NewOverrides will
// become the new global defaults.
func NewOverrides(defaults Limits) (*Overrides, error) {
	defaultLimits = defaults

	if defaults.PerTenantOverrideConfig == "" {
		level.Info(util.Logger).Log("msg", "per-tenant overides disabled")
		return &Overrides{
			Defaults:  defaults,
			overrides: map[string]*Limits{},
			quit:      make(chan struct{}),
		}, nil
	}

	overrides, err := loadOverrides(defaults.PerTenantOverrideConfig)
	if err != nil {
		return nil, err
	}

	o := &Overrides{
		Defaults:  defaults,
		overrides: overrides,
		quit:      make(chan struct{}),
	}

	go o.loop()
	return o, nil
}

func (o *Overrides) loop() {
	ticker := time.NewTicker(o.Defaults.PerTenantOverridePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			overrides, err := loadOverrides(o.Defaults.PerTenantOverrideConfig)
			if err != nil {
				overridesReloadSuccess.Set(0)
				level.Error(util.Logger).Log("msg", "failed to reload overrides", "err", err)
				continue
			}
			overridesReloadSuccess.Set(1)

			o.overridesMtx.Lock()
			o.overrides = overrides
			o.overridesMtx.Unlock()
		case <-o.quit:
			return
		}
	}
}

// Stop background reloading of overrides.
func (o *Overrides) Stop() {
	close(o.quit)
}

func loadOverrides(filename string) (map[string]*Limits, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var overrides struct {
		Overrides map[string]*Limits `yaml:"overrides"`
	}

	decoder := yaml.NewDecoder(f)
	decoder.SetStrict(true)
	if err := decoder.Decode(&overrides); err != nil {
		return nil, err
	}

	return overrides.Overrides, nil
}

func (o *Overrides) getBool(userID string, f func(*Limits) bool) bool {
	o.overridesMtx.RLock()
	defer o.overridesMtx.RUnlock()
	override, ok := o.overrides[userID]
	if !ok {
		return f(&o.Defaults)
	}
	return f(override)
}

func (o *Overrides) getInt(userID string, f func(*Limits) int) int {
	o.overridesMtx.RLock()
	defer o.overridesMtx.RUnlock()
	override, ok := o.overrides[userID]
	if !ok {
		return f(&o.Defaults)
	}
	return f(override)
}

func (o *Overrides) getFloat(userID string, f func(*Limits) float64) float64 {
	o.overridesMtx.RLock()
	defer o.overridesMtx.RUnlock()
	override, ok := o.overrides[userID]
	if !ok {
		return f(&o.Defaults)
	}
	return f(override)
}

func (o *Overrides) getDuration(userID string, f func(*Limits) time.Duration) time.Duration {
	o.overridesMtx.RLock()
	defer o.overridesMtx.RUnlock()
	override, ok := o.overrides[userID]
	if !ok {
		return f(&o.Defaults)
	}
	return f(override)
}

func (o *Overrides) getString(userID string, f func(*Limits) string) string {
	o.overridesMtx.RLock()
	defer o.overridesMtx.RUnlock()
	override, ok := o.overrides[userID]
	if !ok {
		return f(&o.Defaults)
	}
	return f(override)
}

// IngestionRate returns the limit on ingester rate (samples per second).
func (o *Overrides) IngestionRate(userID string) float64 {
	return o.getFloat(userID, func(l *Limits) float64 {
		return l.IngestionRate
	})
}

// IngestionBurstSize returns the burst size for ingestion rate.
func (o *Overrides) IngestionBurstSize(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.IngestionBurstSize
	})
}

// AcceptHASamples returns whether the distributor should track and accept samples from HA replicas for this user.
func (o *Overrides) AcceptHASamples(userID string) bool {
	return o.getBool(userID, func(l *Limits) bool {
		return l.AcceptHASamples
	})
}

// HAReplicaLabel returns the replica label to look for when deciding whether to accept a sample from a Prometheus HA replica.
func (o *Overrides) HAReplicaLabel(userID string) string {
	return o.getString(userID, func(l *Limits) string {
		return l.HAReplicaLabel
	})
}

// HAClusterLabel returns the cluster label to look for when deciding whether to accept a sample from a Prometheus HA replica.
func (o *Overrides) HAClusterLabel(userID string) string {
	return o.getString(userID, func(l *Limits) string {
		return l.HAClusterLabel
	})
}

// MaxLabelNameLength returns maximum length a label name can be.
func (o *Overrides) MaxLabelNameLength(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxLabelNameLength
	})
}

// MaxLabelValueLength returns maximum length a label value can be. This also is
// the maximum length of a metric name.
func (o *Overrides) MaxLabelValueLength(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxLabelValueLength
	})
}

// MaxLabelNamesPerSeries returns maximum number of label/value pairs timeseries.
func (o *Overrides) MaxLabelNamesPerSeries(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxLabelNamesPerSeries
	})
}

// RejectOldSamples returns true when we should reject samples older than certain
// age.
func (o *Overrides) RejectOldSamples(userID string) bool {
	return o.getBool(userID, func(l *Limits) bool {
		return l.RejectOldSamples
	})
}

// RejectOldSamplesMaxAge returns the age at which samples should be rejected.
func (o *Overrides) RejectOldSamplesMaxAge(userID string) time.Duration {
	return o.getDuration(userID, func(l *Limits) time.Duration {
		return l.RejectOldSamplesMaxAge
	})
}

// CreationGracePeriod is misnamed, and actually returns how far into the future
// we should accept samples.
func (o *Overrides) CreationGracePeriod(userID string) time.Duration {
	return o.getDuration(userID, func(l *Limits) time.Duration {
		return l.CreationGracePeriod
	})
}

// MaxSeriesPerQuery returns the maximum number of series a query is allowed to hit.
func (o *Overrides) MaxSeriesPerQuery(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxSeriesPerQuery
	})
}

// MaxSamplesPerQuery returns the maximum number of samples in a query (from the ingester).
func (o *Overrides) MaxSamplesPerQuery(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxSamplesPerQuery
	})
}

// MaxSeriesPerUser returns the maximum number of series a user is allowed to store.
func (o *Overrides) MaxSeriesPerUser(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxSeriesPerUser
	})
}

// MaxSeriesPerMetric returns the maximum number of series allowed per metric.
func (o *Overrides) MaxSeriesPerMetric(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxSeriesPerMetric
	})
}

// MaxChunksPerQuery returns the maximum number of chunks allowed per query.
func (o *Overrides) MaxChunksPerQuery(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxChunksPerQuery
	})
}

// MaxQueryLength returns the limit of the length (in time) of a query.
func (o *Overrides) MaxQueryLength(userID string) time.Duration {
	return o.getDuration(userID, func(l *Limits) time.Duration {
		return l.MaxQueryLength
	})
}

// MaxQueryParallelism returns the limit to the number of sub-queries the
// frontend will process in parallel.
func (o *Overrides) MaxQueryParallelism(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.MaxQueryParallelism
	})
}

// EnforceMetricName whether to enforce the presence of a metric name.
func (o *Overrides) EnforceMetricName(userID string) bool {
	return o.getBool(userID, func(l *Limits) bool {
		return l.EnforceMetricName
	})
}

// CardinalityLimit whether to enforce the presence of a metric name.
func (o *Overrides) CardinalityLimit(userID string) int {
	return o.getInt(userID, func(l *Limits) int {
		return l.CardinalityLimit
	})
}
