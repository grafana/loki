package strategies

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

const (
	SplitKeyspaceStrategyName          = "split_keyspace_by_factor"
	SplitBySeriesChunkSizeStrategyName = "split_by_series_chunks_size"
)

type Limits interface {
	BloomPlanningStrategy(tenantID string) string
	SplitKeyspaceStrategyLimits
	ChunkSizeStrategyLimits
}

type TSDBSet = map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries

type PlanningStrategy interface {
	Name() string
	// Plan returns a set of tasks for a given tenant-table tuple and TSDBs.
	Plan(ctx context.Context, table config.DayTable, tenant string, tsdbs TSDBSet, metas []bloomshipper.Meta) ([]*protos.Task, error)
}

type Metrics struct {
	*ChunkSizeStrategyMetrics
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		ChunkSizeStrategyMetrics: NewChunkSizeStrategyMetrics(reg),
	}
}

type Factory struct {
	limits  Limits
	logger  log.Logger
	metrics *Metrics
}

func NewFactory(
	limits Limits,
	metrics *Metrics,
	logger log.Logger,
) *Factory {
	return &Factory{
		limits:  limits,
		logger:  logger,
		metrics: metrics,
	}
}

func (f *Factory) GetStrategy(tenantID string) (PlanningStrategy, error) {
	strategy := f.limits.BloomPlanningStrategy(tenantID)

	switch strategy {
	case SplitKeyspaceStrategyName:
		return NewSplitKeyspaceStrategy(f.limits, f.logger)
	case SplitBySeriesChunkSizeStrategyName:
		return NewChunkSizeStrategy(f.limits, f.metrics.ChunkSizeStrategyMetrics, f.logger)
	default:
		return nil, fmt.Errorf("unknown bloom planning strategy (%s)", strategy)
	}
}
