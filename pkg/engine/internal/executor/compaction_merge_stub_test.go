package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TestCompactionMergeStub_WritesEmptyObject verifies the stub's contract:
//   - Writes a zero-byte object at the node's OutputPath.
//   - Returns a pipeline that signals EOF on first Read.
//
// This exercises the stub executor (compaction_merge_stub.go). The real
// K-way merge is a separate concern.
func TestCompactionMergeStub_WritesEmptyObject(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	node := &physical.CompactionMerge{
		NodeID:           ulid.Make(),
		Tenant:           "tenant-29",
		ToCWindowStart:   1_715_126_400_000_000_000,
		OutputPath:       "tenants/tenant-29/objects/abc",
		SourceIndexPaths: []string{"idx/x.idx"},
		TaskTTL:          time.Minute,
	}

	g := dag.Graph[physical.Node]{}
	g.Add(node)
	plan := physical.FromGraph(g)

	c := &Context{bucket: bucket, plan: plan}
	pipe := c.execute(ctx, node)
	t.Cleanup(pipe.Close)

	// Side effect must be deferred to Open(): the object must not exist
	// before Open is called. Guards against a future regression that
	// uploads eagerly in executeCompactionMergeStub.
	existsBeforeOpen, err := bucket.Exists(ctx, node.OutputPath)
	require.NoError(t, err)
	require.False(t, existsBeforeOpen, "stub must defer upload to Open")

	// Open triggers the side-effect (upload).
	require.NoError(t, pipe.Open(ctx))

	_, err = pipe.Read(ctx)
	require.True(t, errors.Is(err, EOF), "want EOF, got %v", err)

	exists, err := bucket.Exists(ctx, node.OutputPath)
	require.NoError(t, err)
	require.True(t, exists, "stub did not write OutputPath %q", node.OutputPath)

	attrs, err := bucket.Attributes(ctx, node.OutputPath)
	require.NoError(t, err)
	require.Equal(t, int64(0), attrs.Size, "stub must write a zero-byte object")
}
