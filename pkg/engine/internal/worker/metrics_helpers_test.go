package worker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestObserveOperatorCost(t *testing.T) {
	m := newMetrics()
	m.observeOperatorCost([]pipelineNode{
		{OpType: "DataObjScan", SelfDuration: 20 * time.Millisecond, RowsOut: 300},
		{OpType: "Projection/PARSE_LOGFMT", SelfDuration: 30 * time.Millisecond, RowsIn: 400, RowsOut: 250},
		{OpType: "DataObjScan", SelfDuration: 10 * time.Millisecond, RowsOut: 100},
	})

	require.Equal(t, float64(400), testutil.ToFloat64(m.operatorRowsOutTotal.WithLabelValues("DataObjScan")))
	require.Equal(t, float64(250), testutil.ToFloat64(m.operatorRowsOutTotal.WithLabelValues("Projection/PARSE_LOGFMT")))
	// Leaves have no operator input; the parser consumed its child's rows.
	require.Equal(t, float64(0), testutil.ToFloat64(m.operatorRowsInTotal.WithLabelValues("DataObjScan")))
	require.Equal(t, float64(400), testutil.ToFloat64(m.operatorRowsInTotal.WithLabelValues("Projection/PARSE_LOGFMT")))

	// Self-time is grouped by operator type and recorded in seconds: DataObjScan
	// gets 0.02 + 0.01 over two samples, the parser gets 0.03 over one.
	scanCount, scanSum := histogramCountSum(t, m.operatorSelfSeconds, "DataObjScan")
	require.Equal(t, uint64(2), scanCount)
	require.InDelta(t, 0.03, scanSum, 1e-9)

	parseCount, parseSum := histogramCountSum(t, m.operatorSelfSeconds, "Projection/PARSE_LOGFMT")
	require.Equal(t, uint64(1), parseCount)
	require.InDelta(t, 0.03, parseSum, 1e-9)
}

func histogramCountSum(t *testing.T, vec *prometheus.HistogramVec, lvs ...string) (uint64, float64) {
	t.Helper()
	obs, err := vec.GetMetricWithLabelValues(lvs...)
	require.NoError(t, err)
	var dm dto.Metric
	require.NoError(t, obs.(prometheus.Metric).Write(&dm))
	return dm.GetHistogram().GetSampleCount(), dm.GetHistogram().GetSampleSum()
}

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
