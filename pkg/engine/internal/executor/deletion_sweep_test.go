package executor

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestExecuteDeletionSweep(t *testing.T) {
	runLogged := func(t *testing.T, cfg Config, node *physical.DeletionSweep, logger log.Logger) (v2.ResultArtifact, error) {
		var graph dag.Graph[physical.Node]
		graph.Add(node)
		pipeline := Run(t.Context(), cfg, physical.FromGraph(graph), logger)
		defer pipeline.Close()
		if err := pipeline.Open(t.Context()); err != nil {
			return v2.ResultArtifact{}, err
		}
		rec, err := pipeline.Read(t.Context())
		if err != nil {
			return v2.ResultArtifact{}, err
		}
		return oneArtifact(t)(v2.DecodeResultArtifacts(rec))
	}
	run := func(t *testing.T, cfg Config, node *physical.DeletionSweep) (v2.ResultArtifact, error) {
		return runLogged(t, cfg, node, log.NewNopLogger())
	}

	newBucket := func(t *testing.T) objstore.Bucket {
		t.Helper()
		bucket, err := filesystem.NewBucket(t.TempDir())
		require.NoError(t, err)
		return bucket
	}

	t.Run("stores the file list of the tenant and reports its path", func(t *testing.T) {
		bucket := newBucket(t)

		artifact, err := run(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1"})
		require.NoError(t, err)
		require.Equal(t, "deletion/t1/files.jsonl.gz", artifact.Path)
		r, err := deletion.OpenFileList(t.Context(), bucket, "t1")
		require.NoError(t, err)
		require.NoError(t, r.Close())
	})

	// storeOldFileList stores a file list that a sweep compares with. Its
	// path is gone, so the sweep finds no deletion candidates.
	storeOldFileList := func(t *testing.T, bucket objstore.Bucket) {
		t.Helper()
		writer := deletion.NewFileListWriter(bucket, backoff.Config{MaxRetries: 1}, log.NewNopLogger())
		require.NoError(t, writer.Write(t.Context(), deletion.FileList{
			Tenant:    "t1",
			CreatedAt: time.Now().Add(-2 * deletion.MinSweepInterval),
			Paths:     []string{"objects/tenants/t1/00/gone"},
		}))
	}

	for _, tc := range []struct {
		name    string
		delete  bool
		wantLog string
	}{
		{"does a dry run when the node does not delete", false, `msg="deletion sweep dry run: would delete objects"`},
		{"deletes the deletion candidates when the node deletes", true, `msg="deletion sweep deleted candidates"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := newBucket(t)
			storeOldFileList(t, bucket)
			var logs bytes.Buffer
			cfg := Config{DataBucket: bucket, DeletionSweepMetrics: deletion.NewSweepMetrics(prometheus.NewRegistry())}

			_, err := runLogged(t, cfg, &physical.DeletionSweep{Tenant: "t1", Delete: tc.delete}, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
			require.NoError(t, err)
			require.Contains(t, logs.String(), tc.wantLog)
		})
	}

	t.Run("fails without a sweep when the node deletes and no metrics are configured", func(t *testing.T) {
		bucket := newBucket(t)
		storeOldFileList(t, bucket)
		var logs bytes.Buffer

		_, err := runLogged(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1", Delete: true}, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.ErrorContains(t, err, "metrics are required")
		require.NotContains(t, logs.String(), "deletion sweep running")
	})

	t.Run("fails with ErrMalformedFileList when the stored file list is malformed", func(t *testing.T) {
		bucket := newBucket(t)
		require.NoError(t, bucket.Upload(t.Context(), "deletion/t1/files.jsonl.gz", bytes.NewReader([]byte("not gzip"))))

		_, err := run(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorIs(t, err, deletion.ErrMalformedFileList)
	})

	t.Run("fails without a write to the index bucket when no data bucket is configured", func(t *testing.T) {
		indexBucket := newBucket(t)

		_, err := run(t, Config{Bucket: indexBucket}, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorContains(t, err, "no data object bucket configured")
		exists, err := indexBucket.Exists(t.Context(), "deletion/t1/files.jsonl.gz")
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("fails when the tenant is empty", func(t *testing.T) {
		_, err := run(t, Config{DataBucket: newBucket(t)}, &physical.DeletionSweep{})
		require.ErrorContains(t, err, "DeletionSweep: malformed plan")
	})
}
