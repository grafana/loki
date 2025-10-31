package scheduler

import (
	"context"

	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// streamNotification is a deferred call to a stream event handler.
type streamNotification struct {
	Handler  workflow.StreamEventHandler
	Stream   *workflow.Stream
	NewState workflow.StreamState
}

// taskNotification is a deferred call to a task event handler.
type taskNotification struct {
	Handler   workflow.TaskEventHandler
	Task      *workflow.Task
	NewStatus workflow.TaskStatus
}

// A notifier is responsible for invoking [workflow.StreamEventHandler] and
// [workflow.TaskEventHandler].
//
// Notifier is used to avoid deadlocks so notifications can be held without any
// mutexes held.
type notifier struct {
	streamNotifications []streamNotification
	taskNotifications   []taskNotification
}

// AddStreamEvent buffers a stream event notification.
func (n *notifier) AddStreamEvent(notification streamNotification) {
	n.streamNotifications = append(n.streamNotifications, notification)
}

// AddTaskEvent buffers a task event notification.
func (n *notifier) AddTaskEvent(notification taskNotification) {
	n.taskNotifications = append(n.taskNotifications, notification)
}

// Notify handles all pending notifications.
func (n *notifier) Notify(ctx context.Context) {
	for _, ev := range n.streamNotifications {
		ev.Handler(ctx, ev.Stream, ev.NewState)
	}

	for _, ev := range n.taskNotifications {
		ev.Handler(ctx, ev.Task, ev.NewStatus)
	}
}
