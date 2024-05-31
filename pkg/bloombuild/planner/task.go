package planner

import (
	"context"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"time"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

type QueueTask struct {
	*protos.Task

	resultsChannel chan *TaskResult

	// Tracking
	timesEnqueued int
	queueTime     time.Time
	ctx           context.Context
}

func NewTask(
	ctx context.Context,
	queueTime time.Time,
	task *protos.Task,
	resultsChannel chan *TaskResult,
) *QueueTask {
	return &QueueTask{
		Task:           task,
		resultsChannel: resultsChannel,
		ctx:            ctx,
		queueTime:      queueTime,
	}
}

type TaskResult struct {
	metas []bloomshipper.MetaRef
}

func NewTaskResult(metas []bloomshipper.MetaRef) *TaskResult {
	return &TaskResult{
		metas: metas,
	}
}
