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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: "idx/a.idxobj", SectionIndex: 0, MinKey: []string{"a"}, MaxKey: []string{"f"}},
				},
			},
		},
		OutputIndexPath: "tenants/tenant-29/indexes/abc",
		TaskTTL:         10 * time.Minute,
	}

	clone := orig.Clone().(*IndexMerge)

	require.NotEqual(t, orig.ID(), clone.ID(), "Clone must produce a fresh ULID")

	// Mutate the clone; assert original is untouched.
	clone.Runs[0].Sections[0].MinKey[0] = "MUTATED"
	require.Equal(t, []string{"a"}, orig.Runs[0].Sections[0].MinKey,
		"Clone must deep-copy nested SectionRefs")
}

func TestIndexMerge_Clone_AllowsNilRuns(t *testing.T) {
	orig := &IndexMerge{NodeID: ulid.Make()}
	cloned := orig.Clone().(*IndexMerge)
	require.Nil(t, cloned.Runs)
}

func TestIndexMerge_Type(t *testing.T) {
	require.Equal(t, NodeTypeIndexMerge, (&IndexMerge{}).Type())
	require.Equal(t, "IndexMerge", NodeTypeIndexMerge.String())
}
