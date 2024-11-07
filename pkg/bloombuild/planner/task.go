package planner

import (
	"context"
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/strategies"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

type QueueTask struct {
	*strategies.Task

	// We use forSeries in ToProtoTask to get the chunks for the series in the gaps.
	forSeries common.ClosableForSeries

	resultsChannel chan *protos.TaskResult

	// Tracking
	timesEnqueued atomic.Int64
	queueTime     time.Time
	ctx           context.Context
}

func NewQueueTask(
	ctx context.Context,
	queueTime time.Time,
	task *strategies.Task,
	forSeries common.ClosableForSeries,
	resultsChannel chan *protos.TaskResult,
) *QueueTask {
	return &QueueTask{
		Task:           task,
		resultsChannel: resultsChannel,
		ctx:            ctx,
		queueTime:      queueTime,
		forSeries:      forSeries,
	}
}

// ToProtoTask converts a Task to a ProtoTask.
// It will use the opened TSDB to get the chunks for the series in the gaps.
func (t *QueueTask) ToProtoTask(ctx context.Context) (*protos.ProtoTask, error) {
	return t.Task.ToProtoTask(ctx, t.forSeries)
}
