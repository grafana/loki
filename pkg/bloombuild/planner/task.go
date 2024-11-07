package planner

import (
	"context"
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/strategies"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

type QueueTask struct {
	*strategies.Task

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
	resultsChannel chan *protos.TaskResult,
) *QueueTask {
	return &QueueTask{
		Task:           task,
		resultsChannel: resultsChannel,
		ctx:            ctx,
		queueTime:      queueTime,
	}
}
