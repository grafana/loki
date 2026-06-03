package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestTaskTypeLabel(t *testing.T) {
	node := &physical.Limit{NodeID: ulid.Make()}

	tests := []struct {
		name string
		task *workflow.Task
		want string
	}{
		{
			name: "no sources is leaf",
			task: &workflow.Task{},
			want: taskTypeLeaf,
		},
		{
			name: "with sources is non-leaf",
			task: &workflow.Task{
				Sources: map[physical.Node][]*workflow.Stream{
					node: {{ULID: ulid.Make()}},
				},
			},
			want: taskTypeNonLeaf,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, taskTypeLabel(tt.task))
		})
	}
}

func TestStatusUpdateErrorClass(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: "none"},
		{name: "canceled", err: context.Canceled, want: "canceled"},
		{name: "wrapped canceled", err: fmt.Errorf("send: %w", context.Canceled), want: "canceled"},
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "conn closed", err: wire.ErrConnClosed, want: "conn_closed"},
		{name: "client error", err: wire.Errorf(http.StatusTooManyRequests, "busy"), want: "rejected"},
		{name: "server error", err: wire.Errorf(http.StatusInternalServerError, "boom"), want: "server_error"},
		{name: "wrapped server error", err: fmt.Errorf("send: %w", wire.Errorf(http.StatusBadGateway, "boom")), want: "server_error"},
		{name: "other", err: errors.New("nope"), want: "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, statusUpdateErrorClass(tt.err))
		})
	}
}
