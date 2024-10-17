package strategies

import (
	"context"
	"fmt"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

const (
	SplitKeyspaceStrategyName = "split_keyspace_by_factor"
)

type Limits interface {
	BloomPlanningStrategy(tenantID string) string
	BloomSplitSeriesKeyspaceBy(tenantID string) int
}

type TSDBSet = map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries

type PlanningStrategy interface {
	// Plan returns a set of tasks for a given tenant-table tuple and TSDBs.
	Plan(ctx context.Context, table config.DayTable, tenant string, tsdbs TSDBSet, metas []bloomshipper.Meta) ([]*protos.Task, error)
}

func NewStrategy(
	tenantID string,
	limits Limits,
	logger log.Logger,
) (PlanningStrategy, error) {
	strategy := limits.BloomPlanningStrategy(tenantID)

	switch strategy {
	case SplitKeyspaceStrategyName:
		return NewSplitKeyspaceStrategy(limits, logger)
	default:
		return nil, fmt.Errorf("unknown bloom planning strategy (%s)", strategy)
	}
}
