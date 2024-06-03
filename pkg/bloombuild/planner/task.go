package planner

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

type Task struct {
	*protos.Task

	// Tracking
	queueTime time.Time
	ctx       context.Context
}

func NewTask(ctx context.Context, queueTime time.Time, task *protos.Task) *Task {
	return &Task{
		Task:      task,
		ctx:       ctx,
		queueTime: queueTime,
	}
}
