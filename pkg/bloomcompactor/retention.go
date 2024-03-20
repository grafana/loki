package bloomcompactor

import (
	"context"
	"flag"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	storageconfig "github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/validation"
)

type retentionSharding interface {
	OwnsRetention() (bool, error)
}

type firstTokenRetentionSharding struct {
	ring           ring.ReadRing
	ringLifeCycler *ring.BasicLifecycler
}

func newFirstTokenRetentionSharding(ring ring.ReadRing, ringLifeCycler *ring.BasicLifecycler) *firstTokenRetentionSharding {
	return &firstTokenRetentionSharding{
		ring:           ring,
		ringLifeCycler: ringLifeCycler,
	}
}

// OwnsRetention returns true if the compactor should apply retention.
// This is determined by checking if the compactor owns the smaller token in the ring.
// Note that during a ring topology change, more than one compactor may attempt to apply retention.
// This is fine since retention consists on deleting old data which should be idempotent.
func (s *firstTokenRetentionSharding) OwnsRetention() (bool, error) {
	rs, err := s.ring.GetAllHealthy(RingOp)
	if err != nil {
		return false, errors.Wrap(err, "getting ring healthy instances")
	}
	if len(rs.Instances) == 0 {
		return false, errors.New("no healthy instances in ring")
	}

	// Lookup the instance with smaller token
	instance := slices.MinFunc(rs.Instances, func(a, b ring.InstanceDesc) int {
		smallerA := slices.Min(a.GetTokens())
		smallerB := slices.Min(b.GetTokens())
		if smallerA < smallerB {
			return -1
		}
		if smallerA > smallerB {
			return 1
		}
		return 0
	})

	return instance.GetId() == s.ringLifeCycler.GetInstanceID(), nil
}

type RetentionConfig struct {
	Enabled         bool `yaml:"enabled"`
	MaxLookbackDays int  `yaml:"max_lookback_days"`
}

func (cfg *RetentionConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "bloom-compactor.retention.enabled", false, "Enable bloom retention.")
	f.IntVar(&cfg.MaxLookbackDays, "bloom-compactor.retention.max-lookback-days", 365, "Max lookback days for retention.")
}

type RetentionLimits interface {
	RetentionPeriod(userID string) time.Duration
	StreamRetention(userID string) []validation.StreamRetention
}

type RetentionManager struct {
	cfg        RetentionConfig
	limits     RetentionLimits
	bloomStore bloomshipper.Store
	sharding   retentionSharding
	metrics    *Metrics
	logger     log.Logger
	lastDayRun storageconfig.DayTime

	// For testing
	now func() model.Time
}

func NewRetentionManager(
	cfg RetentionConfig,
	limits RetentionLimits,
	bloomStore bloomshipper.Store,
	sharding retentionSharding,
	metrics *Metrics,
	logger log.Logger,
) *RetentionManager {
	return &RetentionManager{
		cfg:        cfg,
		limits:     limits,
		bloomStore: bloomStore,
		sharding:   sharding,
		metrics:    metrics,
		logger:     log.With(logger, "subcomponent", "retention-manager"),
		now:        model.Now,
		lastDayRun: storageconfig.NewDayTime(model.Earliest),
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

	ownsRetention, err := r.sharding.OwnsRetention()
	if err != nil {
		return errors.Wrap(err, "checking if compactor owns retention")
	}
	if !ownsRetention {
		level.Debug(r.logger).Log("msg", "this compactor doesn't own retention")
		return nil
	}

	level.Info(r.logger).Log("msg", "Applying retention")
	r.metrics.retentionRunning.Set(1)
	defer r.metrics.retentionRunning.Set(0)

	// We iterate through up to r.cfg.MaxLookbackDays days unless it's set to 0
	// In that case, we iterate through all days
	var daysProcessed int
	day := today
	for i := 1; i <= r.cfg.MaxLookbackDays || r.cfg.MaxLookbackDays == 0; i++ {
		day = day.Dec()
		dayLogger := log.With(r.logger, "day", day.String())
		bloomClient, err := r.bloomStore.Client(day.ModelTime())
		if err != nil {
			level.Error(dayLogger).Log("msg", "failed to get bloom store client", "err", err)
			break
		}
		objectClient := bloomClient.ObjectClient()

		tenants, err := r.bloomStore.TenantFilesForInterval(ctx, bloomshipper.NewInterval(day.Bounds()))
		if err != nil {
			r.metrics.retentionTime.WithLabelValues(statusFailure).Observe(time.Since(start.Time()).Seconds())
			r.metrics.retentionDaysPerIteration.WithLabelValues(statusFailure).Observe(float64(daysProcessed))
			return errors.Wrap(err, "getting users for period")
		}

		if len(tenants) == 0 {
			// No tenants for this day means we can break here since previous
			// retention iterations have already deleted all tenants
			break
		}

		for tenant, objectKeys := range tenants {
			tenantRetention := findLongestRetention(tenant, r.limits)
			expirationDay := storageconfig.NewDayTime(today.Add(-tenantRetention))
			if !day.Before(expirationDay) {
				continue
			}

			tenantLogger := log.With(dayLogger, "tenant", tenant, "retention", tenantRetention)
			level.Info(tenantLogger).Log("msg", "applying retention to tenant")

			// Note: we cannot delete the tenant directory directly because it is not an
			// actual key in the object store. Instead, we need to delete all keys one by one.
			for _, key := range objectKeys {
				if err := objectClient.DeleteObject(ctx, key); err != nil {
					r.metrics.retentionTime.WithLabelValues(statusFailure).Observe(time.Since(start.Time()).Seconds())
					r.metrics.retentionDaysPerIteration.WithLabelValues(statusFailure).Observe(float64(daysProcessed))
					return errors.Wrapf(err, "deleting key %s", key)
				}
			}
		}

		daysProcessed++
	}

	level.Info(r.logger).Log("msg", "finished applying retention")
	r.lastDayRun = today
	r.metrics.retentionTime.WithLabelValues(statusSuccess).Observe(time.Since(start.Time()).Seconds())
	r.metrics.retentionDaysPerIteration.WithLabelValues(statusSuccess).Observe(float64(daysProcessed))

	return nil
}

func findLongestRetention(tenant string, limits RetentionLimits) time.Duration {
	globalRetention := limits.RetentionPeriod(tenant)
	streamRetention := limits.StreamRetention(tenant)
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
