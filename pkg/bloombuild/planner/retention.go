package planner

import (
	"context"
	"flag"
	"math"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	storageconfig "github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/validation"
)

type RetentionConfig struct {
	Enabled         bool `yaml:"enabled"`
	MaxLookbackDays int  `yaml:"max_lookback_days" doc:"hidden"`
}

func (cfg *RetentionConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+".enabled", false, "Enable bloom retention.")
	f.IntVar(&cfg.MaxLookbackDays, prefix+".max-lookback-days", 365, "Max lookback days for retention.")
}

func (cfg *RetentionConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if cfg.MaxLookbackDays < 1 {
		return errors.New("max lookback days must be a positive number")
	}
	return nil
}

type RetentionLimits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
	AllByUserID() map[string]*validation.Limits
	DefaultLimits() *validation.Limits
}

type RetentionManager struct {
	cfg        RetentionConfig
	limits     RetentionLimits
	bloomStore bloomshipper.StoreBase
	metrics    *Metrics
	logger     log.Logger
	lastDayRun storageconfig.DayTime

	// For testing
	now func() model.Time
}

func NewRetentionManager(
	cfg RetentionConfig,
	limits RetentionLimits,
	bloomStore bloomshipper.StoreBase,
	metrics *Metrics,
	logger log.Logger,
) *RetentionManager {
	return &RetentionManager{
		cfg:        cfg,
		limits:     limits,
		bloomStore: bloomStore,
		metrics:    metrics,
		logger:     log.With(logger, "subcomponent", "retention-manager"),
		now:        model.Now,
		lastDayRun: storageconfig.NewDayTime(0),
	}
}

func (r *RetentionManager) Apply(ctx context.Context) error {
	if !r.cfg.Enabled {
		level.Debug(r.logger).Log("msg", "retention is disabled")
		return nil
	}

	start := r.now()
	today := storageconfig.NewDayTime(start)
	if !today.After(r.lastDayRun) {
		// We've already run retention for today
		return nil
	}

	level.Info(r.logger).Log("msg", "Applying retention", "today", today.String(), "lastDayRun", r.lastDayRun.String())
	r.metrics.retentionRunning.Set(1)
	defer r.metrics.retentionRunning.Set(0)

	tenantsRetention := retentionByTenant(r.limits)
	r.reportTenantsExceedingLookback(tenantsRetention)

	defaultLimits := r.limits.DefaultLimits()
	defaultRetention := findLongestRetention(time.Duration(defaultLimits.RetentionPeriod), defaultLimits.StreamRetention)

	smallestRetention := smallestEnabledRetention(defaultRetention, tenantsRetention)
	if smallestRetention == 0 {
		level.Debug(r.logger).Log("msg", "no retention period set for any tenant, skipping retention")
		return nil
	}

	// Start day is today minus the smallest retention period.
	// Note that the last retention day is exclusive. E.g. 30 days retention means we keep 30 days of data,
	// thus we start deleting data from the 31st day onwards.
	startDay := storageconfig.NewDayTime(today.Add(-smallestRetention)).Dec()
	// End day is today minus the max lookback days
	endDay := storageconfig.NewDayTime(today.Add(-time.Duration(r.cfg.MaxLookbackDays) * 24 * time.Hour))

	var daysProcessed int
	tenantsRetentionApplied := make(map[string]struct{}, 100)
	for day := startDay; day.After(endDay); day = day.Dec() {
		dayLogger := log.With(r.logger, "day", day.String())
		bloomClient, err := r.bloomStore.Client(day.ModelTime())
		if err != nil {
			level.Error(dayLogger).Log("msg", "failed to get bloom store client", "err", err)
			break
		}
		objectClient := bloomClient.ObjectClient()

		tenants, err := r.bloomStore.TenantFilesForInterval(
			ctx, bloomshipper.NewInterval(day.Bounds()),
			func(tenant string, _ client.StorageObject) bool {
				// Filter out tenants whose retention hasn't expired yet
				globalRetention := r.limits.RetentionPeriod(tenant)
				streamRetention := r.limits.StreamRetention(tenant)
				tenantRetention := findLongestRetention(globalRetention, streamRetention)
				expirationDay := storageconfig.NewDayTime(today.Add(-tenantRetention))
				return day.Before(expirationDay)
			},
		)
		if err != nil {
			r.metrics.retentionTime.WithLabelValues(statusFailure).Observe(time.Since(start.Time()).Seconds())
			r.metrics.retentionDaysPerIteration.WithLabelValues(statusFailure).Observe(float64(daysProcessed))
			r.metrics.retentionTenantsPerIteration.WithLabelValues(statusFailure).Observe(float64(len(tenantsRetentionApplied)))
			return errors.Wrap(err, "getting users for period")
		}

		if len(tenants) == 0 {
			// No tenants for this day means we can break here since previous
			// retention iterations have already deleted all tenants
			break
		}

		for tenant, objects := range tenants {
			if len(objects) == 0 {
				continue
			}

			tenantLogger := log.With(dayLogger, "tenant", tenant)
			level.Info(tenantLogger).Log("msg", "applying retention to tenant", "keys", len(objects))

			// Note: we cannot delete the tenant directory directly because it is not an
			// actual key in the object store. Instead, we need to delete all keys one by one.
			for _, object := range objects {
				if err := objectClient.DeleteObject(ctx, object.Key); err != nil {
					r.metrics.retentionTime.WithLabelValues(statusFailure).Observe(time.Since(start.Time()).Seconds())
					r.metrics.retentionDaysPerIteration.WithLabelValues(statusFailure).Observe(float64(daysProcessed))
					r.metrics.retentionTenantsPerIteration.WithLabelValues(statusFailure).Observe(float64(len(tenantsRetentionApplied)))
					return errors.Wrapf(err, "deleting key %s", object.Key)
				}
			}

			tenantsRetentionApplied[tenant] = struct{}{}
		}

		daysProcessed++
	}

	r.lastDayRun = today
	r.metrics.retentionTime.WithLabelValues(statusSuccess).Observe(time.Since(start.Time()).Seconds())
	r.metrics.retentionDaysPerIteration.WithLabelValues(statusSuccess).Observe(float64(daysProcessed))
	r.metrics.retentionTenantsPerIteration.WithLabelValues(statusSuccess).Observe(float64(len(tenantsRetentionApplied)))
	level.Info(r.logger).Log("msg", "finished applying retention", "daysProcessed", daysProcessed, "tenants", len(tenantsRetentionApplied))

	return nil
}

func (r *RetentionManager) reportTenantsExceedingLookback(retentionByTenant map[string]time.Duration) {
	if len(retentionByTenant) == 0 {
		r.metrics.retentionTenantsExceedingLookback.Set(0)
		return
	}

	var tenantsExceedingLookback int
	for tenant, retention := range retentionByTenant {
		if retention > time.Duration(r.cfg.MaxLookbackDays)*24*time.Hour {
			level.Warn(r.logger).Log("msg", "tenant retention exceeds max lookback days", "tenant", tenant, "retention", retention.String())
		}
		tenantsExceedingLookback++
	}

	r.metrics.retentionTenantsExceedingLookback.Set(float64(tenantsExceedingLookback))
}

func findLongestRetention(globalRetention time.Duration, streamRetention []validation.StreamRetention) time.Duration {
	if len(streamRetention) == 0 {
		return globalRetention
	}

	maxStreamRetention := slices.MaxFunc(streamRetention, func(a, b validation.StreamRetention) int {
		return int(a.Period - b.Period)
	})

	if time.Duration(maxStreamRetention.Period) > globalRetention {
		return time.Duration(maxStreamRetention.Period)
	}
	return globalRetention
}

func retentionByTenant(limits RetentionLimits) map[string]time.Duration {
	all := limits.AllByUserID()
	if len(all) == 0 {
		return nil
	}

	retentions := make(map[string]time.Duration, len(all))
	for tenant, lim := range all {
		retention := findLongestRetention(time.Duration(lim.RetentionPeriod), lim.StreamRetention)
		if retention == 0 {
			continue
		}
		retentions[tenant] = retention
	}

	return retentions
}

// smallestEnabledRetention returns the smallest retention period across all tenants and the default.
func smallestEnabledRetention(defaultRetention time.Duration, perTenantRetention map[string]time.Duration) time.Duration {
	if len(perTenantRetention) == 0 {
		return defaultRetention
	}

	smallest := time.Duration(math.MaxInt64)
	if defaultRetention != 0 {
		smallest = defaultRetention
	}

	for _, retention := range perTenantRetention {
		// Skip unlimited retention
		if retention == 0 {
			continue
		}

		if retention < smallest {
			smallest = retention
		}
	}

	if smallest == time.Duration(math.MaxInt64) {
		// No tenant nor defaults configures a retention
		return 0
	}

	return smallest
}
