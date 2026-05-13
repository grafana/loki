package physical

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// TestCompactionMerge_CloneIsDeepCopy verifies that Clone() produces a node
// whose Runs and SectionRefs can be mutated without affecting the original.
// This is the only non-trivial behavior in the node; the other interface
// methods are pure getters.
func TestCompactionMerge_CloneIsDeepCopy(t *testing.T) {
	orig := &CompactionMerge{
		NodeID:         ulid.Make(),
		Tenant:         "tenant-29",
		ToCWindowStart: 1715126400_000_000_000,
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: "objs/a.dataobj", SectionIndex: 0, MinKey: "a", MaxKey: "f"},
				},
			},
		},
		SourceIndexPaths: []string{"idx/x.idx"},
		OutputPath:       "tenants/tenant-29/objects/abc",
		TaskTTL:          10 * time.Minute,
	}

	clone := orig.Clone().(*CompactionMerge)

	require.NotEqual(t, orig.ID(), clone.ID(), "Clone must produce a fresh ULID")

	// Mutate the clone's nested structures; assert original is untouched.
	clone.Runs[0].Sections[0].MinKey = "MUTATED"
	clone.SourceIndexPaths[0] = "MUTATED"

	require.Equal(t, "a", orig.Runs[0].Sections[0].MinKey,
		"Clone must deep-copy nested SectionRefs; observed shallow alias")
	require.Equal(t, "idx/x.idx", orig.SourceIndexPaths[0],
		"Clone must deep-copy SourceIndexPaths")
}

// TestCompactionMerge_Clone_AllowsNilRuns mirrors
// TestPointersScan_Clone_AllowsNilSelector: a node whose nilable slice
// field is nil must clone to a node whose corresponding field is also
// nil (no make-empty-slice surprise).
func TestCompactionMerge_Clone_AllowsNilRuns(t *testing.T) {
	orig := &CompactionMerge{NodeID: ulid.Make()}
	cloned := orig.Clone().(*CompactionMerge)
	require.Nil(t, cloned.Runs)
	require.Nil(t, cloned.SourceIndexPaths)
}
