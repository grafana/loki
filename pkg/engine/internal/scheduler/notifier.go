package scheduler

import (
	"context"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
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
// [workflow.TaskEventHandler], and for delivering best-effort wire messages to
// workers.
//
// Notifier is used to avoid deadlocks so notifications can be held without any
// mutexes held. Buffering best-effort worker messages here (rather than sending
// them inline) ensures the send — which can block on a full connection buffer —
// happens after the caller has released the scheduler's locks, so a slow or
// backed-up worker connection can't stall the whole scheduler.
type notifier struct {
	streamNotifications []streamNotification
	taskNotifications   []taskNotification
	messages            []pendingMessage
}

// AddStreamEvent buffers a stream event notification.
func (n *notifier) AddStreamEvent(notification streamNotification) {
	n.streamNotifications = append(n.streamNotifications, notification)
}

// AddTaskEvent buffers a task event notification.
func (n *notifier) AddTaskEvent(notification taskNotification) {
	n.taskNotifications = append(n.taskNotifications, notification)
}

// AddMessage buffers a best-effort message to send to a worker once the
// caller's locks have been released (see [notifier.Notify]).
func (n *notifier) AddMessage(peer *workerConn, msg wire.Message) {
	if peer == nil {
		return
	}
	n.messages = append(n.messages, pendingMessage{peer: peer, msg: msg})
}

// Notify handles all pending notifications and buffered worker messages.
//
// Messages are sent before invoking handlers to preserve the previous ordering
// where messages were dispatched to workers before the deferred handler
// callbacks ran.
func (n *notifier) Notify(ctx context.Context) {
	for _, m := range n.messages {
		_ = m.peer.SendMessageAsync(ctx, m.msg)
	}

	for _, ev := range n.streamNotifications {
		ev.Handler(ctx, ev.Stream, ev.NewState)
	}

	for _, ev := range n.taskNotifications {
		ev.Handler(ctx, ev.Task, ev.NewStatus)
	}
}
