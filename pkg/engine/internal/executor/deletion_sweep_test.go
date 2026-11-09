package executor

import (
	"bytes"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

func TestExecuteDeletionSweep(t *testing.T) {
	run := func(t *testing.T, cfg Config, node *physical.DeletionSweep) (v2.ResultArtifact, error) {
		var graph dag.Graph[physical.Node]
		graph.Add(node)
		pipeline := Run(t.Context(), cfg, physical.FromGraph(graph), log.NewNopLogger())
		defer pipeline.Close()
		if err := pipeline.Open(t.Context()); err != nil {
			return v2.ResultArtifact{}, err
		}
		rec, err := pipeline.Read(t.Context())
		if err != nil {
			return v2.ResultArtifact{}, err
		}
		var artifact v2.ResultArtifact
		require.NoError(t, artifact.FromRecordBatch(rec))
		return artifact, nil
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

	t.Run("fails with ErrMalformedFileList when the stored file list is malformed", func(t *testing.T) {
		bucket := newBucket(t)
		require.NoError(t, bucket.Upload(t.Context(), "deletion/t1/files.jsonl.gz", bytes.NewReader([]byte("not gzip"))))

		_, err := run(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorIs(t, err, deletion.ErrMalformedFileList)
	})

	t.Run("fails when no bucket is configured", func(t *testing.T) {
		_, err := run(t, Config{}, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorContains(t, err, "no object store bucket configured")
	})

	t.Run("fails when the tenant is empty", func(t *testing.T) {
		_, err := run(t, Config{DataBucket: newBucket(t)}, &physical.DeletionSweep{})
		require.ErrorContains(t, err, "DeletionSweep: malformed plan")
	})
}
