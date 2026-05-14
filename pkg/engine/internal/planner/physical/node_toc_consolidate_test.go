package physical

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestTableOfContentsConsolidate_Type(t *testing.T) {
	n := &TableOfContentsConsolidate{NodeID: ulid.Make()}
	require.Equal(t, NodeTypeTableOfContentsConsolidate, n.Type())
	require.Equal(t, "TableOfContentsConsolidate", n.Type().String())
}

func TestTableOfContentsConsolidate_Clone(t *testing.T) {
	src := &TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		Tenant:           "29",
		ToCWindowStart:   1_715_000_000_000_000_000,
		RemoveIndexPaths: []string{"tenants/29/indexes/old-a.dataobj", "tenants/29/indexes/old-b.dataobj"},
		AddIndexPaths:    []string{"tenants/29/indexes/new-a.dataobj"},
		TaskTTL:          30 * time.Second,
	}

	clone, ok := src.Clone().(*TableOfContentsConsolidate)
	require.True(t, ok)
	require.NotEqual(t, src.NodeID, clone.NodeID, "Clone must produce a new ULID")
	require.Equal(t, src.Tenant, clone.Tenant)
	require.Equal(t, src.ToCWindowStart, clone.ToCWindowStart)
	require.Equal(t, src.TaskTTL, clone.TaskTTL)
	require.Equal(t, src.RemoveIndexPaths, clone.RemoveIndexPaths)
	require.Equal(t, src.AddIndexPaths, clone.AddIndexPaths)

	// Mutating the clone's slices must not affect the source — i.e. deep copy.
	clone.RemoveIndexPaths[0] = "MUTATED"
	require.NotEqual(t, "MUTATED", src.RemoveIndexPaths[0])
	clone.AddIndexPaths[0] = "MUTATED"
	require.NotEqual(t, "MUTATED", src.AddIndexPaths[0])
}

func TestTableOfContentsConsolidate_Clone_PreservesEmptyButNonNilSlices(t *testing.T) {
	src := &TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		RemoveIndexPaths: []string{},
		AddIndexPaths:    []string{},
	}
	clone := src.Clone().(*TableOfContentsConsolidate)
	require.NotNil(t, clone.RemoveIndexPaths, "empty-but-non-nil slice must clone as empty-but-non-nil, not nil")
	require.NotNil(t, clone.AddIndexPaths, "empty-but-non-nil slice must clone as empty-but-non-nil, not nil")
	require.Len(t, clone.RemoveIndexPaths, 0)
	require.Len(t, clone.AddIndexPaths, 0)
}

func TestTableOfContentsConsolidate_CacheKeyIsEmpty(t *testing.T) {
	// TableOfContentsConsolidate runs alone in a Phase-2 workflow and is
	// never part of a cacheable query fragment.
	n := &TableOfContentsConsolidate{NodeID: ulid.Make()}
	require.Equal(t, "", n.CacheKey(context.Background()))
}
