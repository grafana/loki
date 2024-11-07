package strategies

import (
	"fmt"

	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type Gap struct {
	Bounds v1.FingerprintBounds
	Series []model.Fingerprint
	Blocks []bloomshipper.BlockRef
}

// Task represents a task that is enqueued in the planner.
type Task struct {
	ID              string
	Table           config.DayTable
	Tenant          string
	OwnershipBounds v1.FingerprintBounds
	TSDB            tsdb.SingleTenantTSDBIdentifier
	Gaps            []Gap
}

func NewTask(
	table config.DayTable,
	tenant string,
	bounds v1.FingerprintBounds,
	tsdb tsdb.SingleTenantTSDBIdentifier,
	gaps []Gap,
) *Task {
	return &Task{
		ID:              fmt.Sprintf("%s-%s-%s-%d", table.Addr(), tenant, bounds.String(), len(gaps)),
		Table:           table,
		Tenant:          tenant,
		OwnershipBounds: bounds,
		TSDB:            tsdb,
		Gaps:            gaps,
	}
}
