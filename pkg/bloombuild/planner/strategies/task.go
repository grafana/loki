package strategies

import (
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
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
	*protos.Task
	// Override the protos.Task.Gaps field with gaps that use model.Fingerprint instead of v1.Series.
	Gaps []Gap
}

func NewTask(
	table config.DayTable,
	tenant string,
	bounds v1.FingerprintBounds,
	tsdb tsdb.SingleTenantTSDBIdentifier,
	gaps []Gap,
) *Task {
	return &Task{
		Task: protos.NewTask(table, tenant, bounds, tsdb, nil),
		Gaps: gaps,
	}
}
