package executor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TestIndexConsolidateStub_WritesEmptyIndexObject verifies the contract laid
// out by the spec for the stub: PUT an empty object at OutputIndexPath, no
// ToC swap, no marker delete, drain to EOF (i.e. task Completed).
func TestIndexConsolidateStub_WritesEmptyIndexObject(t *testing.T) {
	bucket := objstore.NewInMemBucket()

	// Pre-create the marker so we can later assert non-deletion.
	ctx := t.Context()
	require.NoError(t, bucket.Upload(ctx, "dataobj/compaction/in-flight/abc.json", bytes.NewReader([]byte("marker"))))

	node := &physical.IndexConsolidate{
		NodeID:                  ulid.Make(),
		Tenant:                  "29",
		ToCWindowStart:          1_715_000_000_000_000_000,
		CompactedLogObjectPaths: []string{"tenants/29/objects/a.dataobj"},
		SourceIndexPaths:        []string{"tenants/29/indexes/x.dataobj"},
		OutputIndexPath:         "tenants/29/indexes/out.dataobj",
		MarkerPath:              "dataobj/compaction/in-flight/abc.json",
		TaskTTL:                 30 * time.Minute,
	}

	plan := buildSingleNodePlan(t, node)

	c := &Context{
		plan:   plan,
		bucket: bucket,
		logger: log.NewNopLogger(),
	}

	pipeline := c.executeIndexConsolidateStub(ctx, node)

	// Drain to EOF — i.e. the framework will see the task as Completed.
	for {
		_, err := pipeline.Read(ctx)
		if errors.Is(err, EOF) || errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	pipeline.Close()

	// 1) The empty index object exists at OutputIndexPath.
	exists, err := bucket.Exists(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	require.True(t, exists, "expected stub to upload an object at OutputIndexPath")

	// 2) The object is empty (zero-byte) — explicit stub semantics.
	rc, err := bucket.Get(ctx, node.OutputIndexPath)
	require.NoError(t, err)
	defer rc.Close()
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, 0, len(body), "stub must write a zero-byte payload")

	// 3) The marker is NOT deleted.
	markerExists, err := bucket.Exists(ctx, node.MarkerPath)
	require.NoError(t, err)
	require.True(t, markerExists, "stub must NOT delete the marker")

	// 4) Bucket contains exactly two paths: the output index and the marker we pre-created.
	require.Len(t, bucket.Objects(), 2)
	_, hasOutput := bucket.Objects()[node.OutputIndexPath]
	require.True(t, hasOutput)
	_, hasMarker := bucket.Objects()[node.MarkerPath]
	require.True(t, hasMarker)
}

func TestIndexConsolidateStub_NoBucketReturnsError(t *testing.T) {
	node := &physical.IndexConsolidate{
		NodeID:          ulid.Make(),
		OutputIndexPath: "some/path",
	}
	plan := buildSingleNodePlan(t, node)

	c := &Context{plan: plan, logger: log.NewNopLogger()}
	pipeline := c.executeIndexConsolidateStub(t.Context(), node)
	_, err := pipeline.Read(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "no object store bucket")
}

func buildSingleNodePlan(t *testing.T, n physical.Node) *physical.Plan {
	t.Helper()
	var g dag.Graph[physical.Node]
	g.Add(n)
	return physical.FromGraph(g)
}
