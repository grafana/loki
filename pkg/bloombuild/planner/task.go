package planner

import (
	"context"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"time"
)

type Task struct {
	*protos.Task

	// Tracking
	queueTime time.Time
	ctx       context.Context
}

func NewQueueTask(ctx context.Context, queueTime time.Time, task *protos.Task) *Task {
	return &Task{
		Task:      task,
		ctx:       ctx,
		queueTime: queueTime,
	}
}
