package physical

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

func TestIndexMerge_CloneIsDeepCopy(t *testing.T) {
	orig := &IndexMerge{
		NodeID:         ulid.Make(),
		Tenant:         "tenant-29",
		ToCWindowStart: 1715126400_000_000_000,
		Runs: []compactionv2pb.RunRef{
			{
				Sections: []compactionv2pb.SectionRef{
					{ObjectPath: "idx/a.idxobj", SectionIndex: 0, MinKey: []string{"a"}, MaxKey: []string{"f"}},
				},
			},
		},
		OutputIndexPath: "tenants/tenant-29/indexes/abc",
		TaskTTL:         10 * time.Minute,
	}

	clone := orig.Clone().(*IndexMerge)

	require.NotEqual(t, orig.ID(), clone.ID(), "Clone must produce a fresh ULID")

	// Mutate the clone; assert original is untouched. Cover both MinKey
	// and MaxKey: cloneRuns clones each via a separate slices.Clone call,
	// so a future regression that drops one but not the other should fail.
	clone.Runs[0].Sections[0].MinKey[0] = "MUTATED"
	clone.Runs[0].Sections[0].MaxKey[0] = "MUTATED"
	require.Equal(t, []string{"a"}, orig.Runs[0].Sections[0].MinKey,
		"Clone must deep-copy nested SectionRef.MinKey")
	require.Equal(t, []string{"f"}, orig.Runs[0].Sections[0].MaxKey,
		"Clone must deep-copy nested SectionRef.MaxKey")
}

// TestIndexMerge_Clone_TolerateZeroElements verifies cloneRuns does not
// panic on zero-value RunRef or SectionRef entries (the value-typed
// equivalent of the nil entries the pointer-shaped slices used to allow).
func TestIndexMerge_Clone_TolerateZeroElements(t *testing.T) {
	orig := &IndexMerge{
		NodeID: ulid.Make(),
		Runs: []compactionv2pb.RunRef{
			{},
			{Sections: []compactionv2pb.SectionRef{{}}},
		},
	}
	require.NotPanics(t, func() { _ = orig.Clone() })
}

func TestIndexMerge_Clone_AllowsNilRuns(t *testing.T) {
	orig := &IndexMerge{NodeID: ulid.Make()}
	cloned := orig.Clone().(*IndexMerge)
	require.Nil(t, cloned.Runs)
}
