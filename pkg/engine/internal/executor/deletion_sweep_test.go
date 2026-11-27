package executor

import (
	"bytes"
	"os"
	"path/filepath"
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
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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

	newRootBucket := func(t *testing.T, root string) objstore.Bucket {
		t.Helper()
		bucket, err := filesystem.NewBucket(root)
		require.NoError(t, err)
		return bucket
	}
	newBucket := func(t *testing.T) objstore.Bucket {
		t.Helper()
		return newRootBucket(t, t.TempDir())
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

	orphan := "objects/tenants/t1/00/orphan"
	// storeCandidate stores orphan, an object that no ToC reaches, and a file
	// list that holds it. A sweep then finds orphan as a deletion candidate.
	storeCandidate := func(t *testing.T, root string, bucket objstore.Bucket) {
		t.Helper()
		old := time.Now().Add(-2 * deletion.MinObjectAge)
		storeReachableIndex(t, root, bucket, "t1", "indexes/tenants/t1/aa/index", old)
		require.NoError(t, bucket.Upload(t.Context(), orphan, bytes.NewReader([]byte("x"))))
		require.NoError(t, os.Chtimes(filepath.Join(root, orphan), old, old))
		writer := deletion.NewFileListWriter(bucket, backoff.Config{MaxRetries: 1}, log.NewNopLogger())
		require.NoError(t, writer.Write(t.Context(), deletion.FileList{
			Tenant:    "t1",
			CreatedAt: time.Now().Add(-2 * deletion.MinSweepInterval),
			Paths:     []string{orphan},
		}))
	}
	requireOrphanExists := func(t *testing.T, bucket objstore.Bucket, want bool) {
		t.Helper()
		exists, err := bucket.Exists(t.Context(), orphan)
		require.NoError(t, err)
		require.Equal(t, want, exists)
	}

	t.Run("logs examples of the deletion candidates and deletes nothing when the node does not delete", func(t *testing.T) {
		root := t.TempDir()
		bucket := newRootBucket(t, root)
		storeCandidate(t, root, bucket)
		var logs bytes.Buffer
		cfg := Config{DataBucket: bucket, DeletionSweepMetrics: deletion.NewSweepMetrics(prometheus.NewRegistry())}

		_, err := runLogged(t, cfg, &physical.DeletionSweep{Tenant: "t1"}, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)
		require.Contains(t, logs.String(), `tenant=t1 msg="deletion sweep candidate" kind=log path=`+orphan+" last_modified=")
		require.NotContains(t, logs.String(), `msg="deletion sweep deleted candidates (logs max 5 failures)"`)
		require.Contains(t, logs.String(), `tenant=t1 msg="deletion sweep finished" outcome=scanned dry_run=true candidates=1 duration=`)
		requireOrphanExists(t, bucket, true)
	})

	t.Run("deletes the deletion candidates and logs no examples when the node deletes", func(t *testing.T) {
		root := t.TempDir()
		bucket := newRootBucket(t, root)
		storeCandidate(t, root, bucket)
		var logs bytes.Buffer
		cfg := Config{DataBucket: bucket, DeletionSweepMetrics: deletion.NewSweepMetrics(prometheus.NewRegistry())}

		_, err := runLogged(t, cfg, &physical.DeletionSweep{Tenant: "t1", Delete: true}, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.NoError(t, err)
		require.Contains(t, logs.String(), `tenant=t1 msg="deletion sweep deleted candidates (logs max 5 failures)" candidates=1 deleted_log_objects=1`)
		require.NotContains(t, logs.String(), "deletion sweep candidate")
		require.Contains(t, logs.String(), `tenant=t1 msg="deletion sweep finished" outcome=scanned dry_run=false candidates=1 duration=`)
		requireOrphanExists(t, bucket, false)
	})

	t.Run("fails without a sweep when the node deletes and no metrics are configured", func(t *testing.T) {
		root := t.TempDir()
		bucket := newRootBucket(t, root)
		storeCandidate(t, root, bucket)
		var logs bytes.Buffer

		_, err := runLogged(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1", Delete: true}, log.NewLogfmtLogger(log.NewSyncWriter(&logs)))
		require.ErrorContains(t, err, "metrics are required")
		require.NotContains(t, logs.String(), "deletion sweep running")
		requireOrphanExists(t, bucket, true)
	})

	t.Run("fails with ErrMalformedFileList when the stored file list is malformed", func(t *testing.T) {
		bucket := newBucket(t)
		require.NoError(t, bucket.Upload(t.Context(), "deletion/t1/files.jsonl.gz", bytes.NewReader([]byte("not gzip"))))

		_, err := run(t, Config{DataBucket: bucket}, &physical.DeletionSweep{Tenant: "t1"})
		require.ErrorIs(t, err, deletion.ErrMalformedFileList)
	})

	t.Run("fails without a write to the index bucket when no data bucket is configured", func(t *testing.T) {
		indexBucket := newBucket(t)

		_, err := run(t, Config{IndexBucket: indexBucket}, &physical.DeletionSweep{Tenant: "t1"})
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

// storeReachableIndex stores an index object of tenant at indexPath in the
// index storage of bucket, and adds it to the ToC of tenant, so that a sweep
// finds a reachable object. root is the directory of bucket. The index object
// was last modified at modified.
func storeReachableIndex(t *testing.T, root string, bucket objstore.Bucket, tenant, indexPath string, modified time.Time) {
	t.Helper()
	builder, err := indexobj.NewBuilder(tenant, metastore.DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil), nil)
	require.NoError(t, err)
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), nil)))
	require.NoError(t, builder.AppendStat("objects/tenants/"+tenant+"/ff/not-stored", 0, 0, nil, modified, modified, 1, 1))
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	r, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer r.Close()
	objectPath := metastore.IndexStoragePrefix + "/" + indexPath
	require.NoError(t, bucket.Upload(t.Context(), objectPath, r))
	require.NoError(t, os.Chtimes(filepath.Join(root, objectPath), modified, modified))

	toc := metastore.NewTableOfContentsWriter(metastore.NewIndexBucket(bucket), metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, log.NewNopLogger(), metastore.NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	require.NoError(t, toc.WriteEntry(t.Context(), tenant, metastore.TableOfContentsEntry{
		Path:      indexPath,
		StartTime: modified,
		EndTime:   modified.Add(time.Minute),
	}))
}
