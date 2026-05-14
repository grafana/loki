package executor

import (
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

// TestTableOfContentsConsolidateStub_DrainsImmediatelyWithNoSideEffects
// verifies the spec contract for the stub: no ToC swap, no bucket write,
// no marker handling. The pipeline drains to EOF on the first Read so the
// framework records the task as Completed.
func TestTableOfContentsConsolidateStub_DrainsImmediatelyWithNoSideEffects(t *testing.T) {
	bucket := objstore.NewInMemBucket()

	node := &physical.TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		Tenant:           "29",
		ToCWindowStart:   1_715_000_000_000_000_000,
		RemoveIndexPaths: []string{"tenants/29/indexes/old-a.dataobj"},
		AddIndexPaths:    []string{"tenants/29/indexes/new-a.dataobj"},
		TaskTTL:          30 * time.Second,
	}

	plan := buildSingleNodePlan(t, node)
	c := &Context{
		plan:   plan,
		bucket: bucket,
		logger: log.NewNopLogger(),
	}

	ctx := t.Context()
	pipeline := c.executeTableOfContentsConsolidateStub(ctx, node)

	// First Read should report EOF — i.e. the framework records Completed.
	rb, err := pipeline.Read(ctx)
	require.True(t, errors.Is(err, EOF) || errors.Is(err, io.EOF), "expected EOF on first Read, got %v", err)
	require.Nil(t, rb)
	pipeline.Close()

	// The stub must NOT have written anything to the bucket.
	require.Empty(t, bucket.Objects(), "stub must not touch the bucket")
}

// TestTableOfContentsConsolidateStub_RunsWithNilBucket verifies that the stub
// is callable even when no bucket is configured. In v1.0 the real executor
// will need a metastore writer but not a bucket directly; the stub doesn't
// need either.
func TestTableOfContentsConsolidateStub_RunsWithNilBucket(t *testing.T) {
	node := &physical.TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		Tenant:           "29",
		ToCWindowStart:   1_715_000_000_000_000_000,
		RemoveIndexPaths: []string{"some/path"},
		AddIndexPaths:    []string{"another/path"},
	}
	plan := buildSingleNodePlan(t, node)
	c := &Context{plan: plan, logger: log.NewNopLogger()}

	pipeline := c.executeTableOfContentsConsolidateStub(t.Context(), node)
	_, err := pipeline.Read(t.Context())
	require.True(t, errors.Is(err, EOF) || errors.Is(err, io.EOF), "expected EOF, got %v", err)
}

func buildSingleNodePlan(t *testing.T, n physical.Node) *physical.Plan {
	t.Helper()
	var g dag.Graph[physical.Node]
	g.Add(n)
	return physical.FromGraph(g)
}
