package compactor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
)

// TestLogMergeTaskCount replays the exact planning path of compactTenantLogs
// against a real, on-disk compacted index file and reports how many LogMerge
// tasks would be dispatched for it.
//
// It is driven entirely by env vars so it can be pointed at any index file
// without editing the test; it skips when LOKI_INDEX_FILE is unset so it stays
// inert under `go test ./...`.
//
//	LOKI_INDEX_FILE               (required) path to the compacted index object
//	LOKI_TENANT                   (required) tenant whose stats sections to read
//	LOKI_LOG_MIN_COMPACTION_SIZE  min compactable size, e.g. "4MB" (default 4MB)
//	LOKI_LOG_MAX_RUNS_PER_TASK    K, runs per task (default 3)
//	LOKI_EXPECTED_TASKS           if set, assert the computed count equals it
//
// Example:
//
//	LOKI_INDEX_FILE=/tmp/index.obj LOKI_TENANT=1234 \
//	LOKI_LOG_MIN_COMPACTION_SIZE=4MB LOKI_LOG_MAX_RUNS_PER_TASK=3 \
//	go test ./pkg/engine/compactor -run TestLogMergeTaskCount -v
func TestLogMergeTaskCount(t *testing.T) {
	idxFile := os.Getenv("LOKI_INDEX_FILE")
	if idxFile == "" {
		t.Skip("set LOKI_INDEX_FILE to a compacted index object to run this test")
	}
	tenant := os.Getenv("LOKI_TENANT")
	require.NotEmpty(t, tenant, "LOKI_TENANT must be set")

	var minSize flagext.Bytes
	if raw := os.Getenv("LOKI_LOG_MIN_COMPACTION_SIZE"); raw != "" {
		require.NoError(t, minSize.Set(raw), "parsing LOKI_LOG_MIN_COMPACTION_SIZE")
	} else {
		require.NoError(t, minSize.Set("4MB"))
	}

	k := 3
	if raw := os.Getenv("LOKI_LOG_MAX_RUNS_PER_TASK"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		require.NoError(t, err, "parsing LOKI_LOG_MAX_RUNS_PER_TASK")
		k = parsed
	}
	require.Positive(t, k, "LOKI_LOG_MAX_RUNS_PER_TASK must be > 0")

	// A filesystem bucket rooted at the file's directory lets logSectionRefsFor
	// (which opens via dataobj.FromBucket) read the object by its base name.
	bucket, err := filesystem.NewBucket(filepath.Dir(idxFile))
	require.NoError(t, err)

	ctx := context.Background()
	refs, sortSchema, err := logSectionRefsFor(ctx, bucket, tenant, filepath.Base(idxFile))
	require.NoError(t, err, "reading log section refs")

	// Mirror compactTenantLogs: CalculateRuns -> IsTerminal -> Plan.
	runs := v2.CalculateRuns(refs)

	var totalSize uint64
	for _, r := range runs {
		totalSize += r.Size()
	}

	numTasks := 0
	terminal := v2.IsTerminal(runs, uint64(minSize))
	if !terminal {
		numTasks = len(v2.Plan(runs, tenant, k, sortSchema))
	}

	t.Logf("index=%s tenant=%s", idxFile, tenant)
	t.Logf("sections=%d runs(P)=%d total_size=%d min_compaction_size=%d max_runs_per_task(K)=%d",
		len(refs), len(runs), totalSize, uint64(minSize), k)
	t.Logf("terminal=%t -> log_merge_tasks=%d", terminal, numTasks)

	if raw := os.Getenv("LOKI_EXPECTED_TASKS"); raw != "" {
		want, err := strconv.Atoi(raw)
		require.NoError(t, err, "parsing LOKI_EXPECTED_TASKS")
		require.Equal(t, want, numTasks, "computed LogMerge task count")
	}
}
