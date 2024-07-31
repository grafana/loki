package strategies

import (
	"context"
	"fmt"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/strategies/splitkeyspace"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

const (
	SplitKeyspaceStrategy = "split"
)

type Limits interface {
	splitkeyspace.Limits
	BloomPlanningStrategy(tenantID string) string
}

type PlanningStrategy interface {
	// Plan returns a set of tasks for a given tenant-table tuple and TSDBs.
	Plan(
		ctx context.Context,
		table config.DayTable,
		tenant string,
		tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
		metas []bloomshipper.Meta,
	) ([]*protos.Task, error)
}

func NewStrategy(
	tenantID string,
	limits Limits,
	logger log.Logger,
) (PlanningStrategy, error) {
	strategy := limits.BloomPlanningStrategy(tenantID)

	switch strategy {
	case SplitKeyspaceStrategy:
		return splitkeyspace.NewSplitKeyspaceStrategy(limits, logger)
	default:
		return nil, fmt.Errorf("unknown bloom planning strategy (%s)", strategy)
	}
}
