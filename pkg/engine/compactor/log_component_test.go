package compactor

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// logBuffer collects log output. It is safe for concurrent use, so a test can
// read it while services still log.
type logBuffer struct {
	bufMu sync.Mutex
	buf   bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.bufMu.Lock()
	defer b.bufMu.Unlock()
	return b.buf.Write(p)
}

// lines returns the non-empty lines of b.
func (b *logBuffer) lines() []string {
	b.bufMu.Lock()
	defer b.bufMu.Unlock()
	var lines []string
	for _, line := range strings.Split(b.buf.String(), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func requireOneComponent(t *testing.T, line, want string) {
	t.Helper()
	require.Equal(t, 1, strings.Count(line, "component="), line)
	require.Contains(t, line, "component="+want, line)
}

func TestLogComponent(t *testing.T) {
	newPlanner := func(t *testing.T, logs *logBuffer) *Planner {
		t.Helper()
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.Enabled = true
		cfg.DeletionSweep.Enabled = true
		bucket := objstore.NewInMemBucket()
		p, err := New(PlannerParams{
			Config:          cfg,
			IndexBucket:     bucket,
			DataBucket:      objstore.NewInMemBucket(),
			MetastoreWriter: metastore.NewTableOfContentsWriter(bucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, log.NewNopLogger(), metastore.NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil)),
			Limits:          newFakeLimits(),
			Logger:          log.NewLogfmtLogger(logs),
		})
		require.NoError(t, err)
		return p
	}

	t.Run("each part of the planner logs its own component once", func(t *testing.T) {
		var logs logBuffer
		p := newPlanner(t, &logs)

		tests := []struct {
			name   string
			logger log.Logger
			want   string
		}{
			{name: "planner", logger: p.logger, want: "compaction-planner"},
			{name: "coordinator", logger: p.coordinator.logger, want: "window-scanner"},
			{name: "index dispatcher", logger: p.coordinator.indexDispatcher.logger, want: "window-scanner"},
			{name: "log dispatcher", logger: p.coordinator.logDispatcher.logger, want: "window-scanner"},
			{name: "pair dispatcher", logger: p.coordinator.pairDispatcher.logger, want: "pair-planner"},
			{name: "pair compactor", logger: p.coordinator.newPairCompactor("acme").logger, want: "pair-planner"},
			{name: "pair planner", logger: p.coordinator.newPairPlanner(nil).logger, want: "pair-planner"},
			{name: "deletion sweep scheduler", logger: p.coordinator.sweeps.logger, want: "deletion-sweep-scheduler"},
		}
		for _, tt := range tests {
			require.NoError(t, tt.logger.Log("msg", "test"), tt.name)
			lines := logs.lines()
			require.NotEmpty(t, lines, tt.name)
			requireOneComponent(t, lines[len(lines)-1], tt.want)
		}
	})

	t.Run("planner logs one component on every line from start to stop", func(t *testing.T) {
		var logs logBuffer
		p := newPlanner(t, &logs)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		require.NoError(t, services.StartAndAwaitRunning(ctx, p))
		require.NoError(t, services.StopAndAwaitTerminated(ctx, p))

		lines := logs.lines()
		for _, line := range lines {
			require.Equal(t, 1, strings.Count(line, "component="), line)
		}
		joined := strings.Join(lines, "\n")
		require.Contains(t, joined, "component=compaction-planner")
		require.Contains(t, joined, "component=window-scanner")
	})

	t.Run("worker logs one component on every line from start to stop", func(t *testing.T) {
		var logs logBuffer
		w, err := NewWorker(WorkerParams{
			TenantOverrides: fakeTenantOverrides{},
			Config:          validWorkerCfg(),
			IndexBucket:     objstore.NewInMemBucket(),
			DataBucket:      objstore.NewInMemBucket(),
			ScratchStore:    scratch.NewMemory(),
			Logger:          log.NewLogfmtLogger(&logs),
			Registerer:      prometheus.NewRegistry(),
		})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		require.NoError(t, services.StartAndAwaitRunning(ctx, w.Service()))
		require.Eventually(t, func() bool { return len(logs.lines()) > 0 }, 5*time.Second, 10*time.Millisecond)
		require.NoError(t, services.StopAndAwaitTerminated(ctx, w.Service()))

		for _, line := range logs.lines() {
			requireOneComponent(t, line, "compaction-worker")
		}
	})
}
