package compactor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// recordingLogger records the plan dumps that it receives, and closes dumped
// after the first one.
type recordingLogger struct {
	dumpsMu sync.Mutex
	dumps   []map[string]any

	dumped     chan struct{}
	dumpedOnce sync.Once
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{dumped: make(chan struct{})}
}

func (l *recordingLogger) Log(keyvals ...any) error {
	fields := make(map[string]any, len(keyvals)/2)
	for i := 0; i+1 < len(keyvals); i += 2 {
		fields[fmt.Sprint(keyvals[i])] = keyvals[i+1]
	}
	if fields["msg"] != "compaction task plan dump" {
		return nil
	}
	l.dumpsMu.Lock()
	l.dumps = append(l.dumps, fields)
	l.dumpsMu.Unlock()
	l.dumpedOnce.Do(func() { close(l.dumped) })
	return nil
}

func (l *recordingLogger) snapshot() []map[string]any {
	l.dumpsMu.Lock()
	defer l.dumpsMu.Unlock()
	return append([]map[string]any(nil), l.dumps...)
}

// decodePlanDump returns the plan in the plan field of a plan dump.
func decodePlanDump(t *testing.T, dump map[string]any) *physical.Plan {
	t.Helper()
	encoded, ok := dump["plan"].(string)
	require.True(t, ok, "the dump must hold the plan as a string")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	var msg physicalpb.Plan
	require.NoError(t, msg.Unmarshal(raw))
	plan, err := msg.MarshalPhysical()
	require.NoError(t, err)
	return plan
}

func TestPlanDispatcherPlanDump(t *testing.T) {
	newPlan := func(window int64) *physical.Plan {
		return buildIndexMergePlan("acme", time.Unix(0, window), indexMergeTask{spec: &compactionv2pb.TaskSpec{
			Runs: []*compactionv2pb.RunRef{
				{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "indexes/a", SectionIndex: 1}}},
				{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "indexes/b", SectionIndex: 2}}},
			},
		}})
	}
	newDispatcher := func(logger log.Logger, run runFunc, maxRetries int, slowTaskThreshold time.Duration) *planDispatcher {
		counter := newCoordinatorMetrics(prometheus.NewRegistry()).taskAttemptsTotal
		retryBackoff := backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
		return newPlanDispatcher(logger, run, 0, maxRetries, retryBackoff, slowTaskThreshold, counter)
	}
	rootOf := func(t *testing.T, plan *physical.Plan) physical.Node {
		t.Helper()
		root, err := plan.Root()
		require.NoError(t, err)
		return root
	}

	t.Run("logs the plan once with the first error when every attempt fails", func(t *testing.T) {
		logger := newRecordingLogger()
		plan := newPlan(0)
		runner := &fakeRunner{err: errors.New("worker disconnected")}
		d := newDispatcher(logger, runner.run, 2, time.Hour)

		_, err := d.RunToCompletion(t.Context(), "acme", []*physical.Plan{plan})
		require.Error(t, err)
		require.Len(t, runner.snapshot(), 3)

		dumps := logger.snapshot()
		require.Len(t, dumps, 1)
		require.Equal(t, "failed", dumps[0]["reason"])
		require.Equal(t, 1, dumps[0]["attempt"])
		dumpErr, ok := dumps[0]["err"].(error)
		require.True(t, ok, "the dump must hold the error of the attempt")
		require.ErrorContains(t, dumpErr, "worker disconnected")
		require.Equal(t, 2, dumps[0]["input_objects"])
		require.Equal(t, rootOf(t, plan), rootOf(t, decodePlanDump(t, dumps[0])))
	})

	t.Run("logs the plan while an attempt runs longer than the slow task threshold", func(t *testing.T) {
		logger := newRecordingLogger()
		plan := newPlan(0)
		d := newDispatcher(logger, func(ctx context.Context, _ workflow.Options, _ *physical.Plan) ([]v2.ResultArtifact, error) {
			select {
			case <-logger.dumped:
				return []v2.ResultArtifact{{Path: "indexes/out"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, 0, time.Millisecond)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, err := d.RunCancellable(ctx, "acme", []*physical.Plan{plan})
		require.NoError(t, err, "the attempt must see the dump before the timeout")

		dumps := logger.snapshot()
		require.Len(t, dumps, 1)
		require.Equal(t, "slow", dumps[0]["reason"])
		require.Equal(t, time.Millisecond, dumps[0]["running_for"])
		require.Equal(t, rootOf(t, plan), rootOf(t, decodePlanDump(t, dumps[0])))
	})

	t.Run("logs no plan when the attempt succeeds before the slow task threshold", func(t *testing.T) {
		logger := newRecordingLogger()
		runner := &fakeRunner{}
		d := newDispatcher(logger, runner.run, 0, time.Hour)

		_, err := d.RunToCompletion(t.Context(), "acme", []*physical.Plan{newPlan(0)})
		require.NoError(t, err)
		require.Empty(t, logger.snapshot())
	})

	t.Run("logs no plan for a slow attempt when the slow task threshold is zero", func(t *testing.T) {
		logger := newRecordingLogger()
		d := newDispatcher(logger, func(context.Context, workflow.Options, *physical.Plan) ([]v2.ResultArtifact, error) {
			time.Sleep(10 * time.Millisecond)
			return []v2.ResultArtifact{{Path: "indexes/out"}}, nil
		}, 0, 0)

		_, err := d.RunToCompletion(t.Context(), "acme", []*physical.Plan{newPlan(0)})
		require.NoError(t, err)
		require.Empty(t, logger.snapshot())
	})

	t.Run("logs only the plan that failed when a failure cuts a sibling attempt short", func(t *testing.T) {
		logger := newRecordingLogger()
		failing, sibling := newPlan(0), newPlan(1)
		d := newDispatcher(logger, func(ctx context.Context, _ workflow.Options, plan *physical.Plan) ([]v2.ResultArtifact, error) {
			if plan == failing {
				return nil, errors.New("boom")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}, 0, time.Hour)

		_, err := d.RunToCompletion(t.Context(), "acme", []*physical.Plan{failing, sibling})
		require.Error(t, err)

		dumps := logger.snapshot()
		require.Len(t, dumps, 1)
		require.Equal(t, rootOf(t, failing), rootOf(t, decodePlanDump(t, dumps[0])))
	})
}
