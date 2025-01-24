package planner

import (
	"context"
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

type TaskMeta struct {
	resultsChannel chan *protos.TaskResult

	// Tracking
	timesEnqueued atomic.Int64
	queueTime     time.Time
	ctx           context.Context
}

type QueueTask struct {
	*protos.ProtoTask
	*TaskMeta
}

func NewQueueTask(
	ctx context.Context,
	queueTime time.Time,
	task *protos.ProtoTask,
	resultsChannel chan *protos.TaskResult,
) *QueueTask {
	return &QueueTask{
		ProtoTask: task,
		TaskMeta: &TaskMeta{
			resultsChannel: resultsChannel,
			ctx:            ctx,
			queueTime:      queueTime,
		},
	}
}
