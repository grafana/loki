package physical

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

func TestIndexConsolidate_Type(t *testing.T) {
	n := &IndexConsolidate{NodeID: ulid.Make()}
	require.Equal(t, NodeTypeIndexConsolidate, n.Type())
	require.Equal(t, "IndexConsolidate", n.Type().String())
}

func TestIndexConsolidate_Clone(t *testing.T) {
	src := &IndexConsolidate{
		NodeID:                  ulid.Make(),
		Tenant:                  "29",
		ToCWindowStart:          1_715_000_000_000_000_000,
		CompactedLogObjectPaths: []string{"tenants/29/objects/a.dataobj", "tenants/29/objects/b.dataobj"},
		SourceIndexPaths:        []string{"tenants/29/indexes/x.dataobj"},
		OutputIndexPath:         "tenants/29/indexes/out.dataobj",
		MarkerPath:              "dataobj/compaction/in-flight/abc.json",
		TaskTTL:                 30 * time.Minute,
	}

	clone, ok := src.Clone().(*IndexConsolidate)
	require.True(t, ok)
	require.NotEqual(t, src.NodeID, clone.NodeID, "Clone must produce a new ULID")
	require.Equal(t, src.Tenant, clone.Tenant)
	require.Equal(t, src.ToCWindowStart, clone.ToCWindowStart)
	require.Equal(t, src.OutputIndexPath, clone.OutputIndexPath)
	require.Equal(t, src.MarkerPath, clone.MarkerPath)
	require.Equal(t, src.TaskTTL, clone.TaskTTL)
	require.Equal(t, src.CompactedLogObjectPaths, clone.CompactedLogObjectPaths)
	require.Equal(t, src.SourceIndexPaths, clone.SourceIndexPaths)

	// Mutating the clone's slices must not affect the source — i.e. deep copy.
	clone.CompactedLogObjectPaths[0] = "MUTATED"
	require.NotEqual(t, "MUTATED", src.CompactedLogObjectPaths[0])
	clone.SourceIndexPaths[0] = "MUTATED"
	require.NotEqual(t, "MUTATED", src.SourceIndexPaths[0])
}

func TestIndexConsolidate_Clone_PreservesEmptyButNonNilSlices(t *testing.T) {
	src := &IndexConsolidate{
		NodeID:                  ulid.Make(),
		CompactedLogObjectPaths: []string{},
		SourceIndexPaths:        []string{},
	}
	clone := src.Clone().(*IndexConsolidate)
	require.NotNil(t, clone.CompactedLogObjectPaths, "empty-but-non-nil slice must clone as empty-but-non-nil, not nil")
	require.NotNil(t, clone.SourceIndexPaths, "empty-but-non-nil slice must clone as empty-but-non-nil, not nil")
	require.Len(t, clone.CompactedLogObjectPaths, 0)
	require.Len(t, clone.SourceIndexPaths, 0)
}

func TestIndexConsolidate_CacheKeyIsEmpty(t *testing.T) {
	// IndexConsolidate runs alone in a Phase-2 workflow and is never part of a
	// cacheable query fragment.
	n := &IndexConsolidate{NodeID: ulid.Make()}
	require.Equal(t, "", n.CacheKey(context.Background()))
}
