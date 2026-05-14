package physical

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

func TestLogMerge_CloneIsDeepCopy(t *testing.T) {
	orig := &LogMerge{
		NodeID:         ulid.Make(),
		Tenant:         "tenant-29",
		ToCWindowStart: 1715126400_000_000_000,
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: "objs/a.dataobj", SectionIndex: 0, MinKey: []string{"a"}, MaxKey: []string{"f"}},
				},
			},
		},
		SourceIndexPaths: []string{"idx/x.idx"},
		OutputPath:       "tenants/tenant-29/objects/abc",
		TaskTTL:          10 * time.Minute,
	}

	clone := orig.Clone().(*LogMerge)

	require.NotEqual(t, orig.ID(), clone.ID(), "Clone must produce a fresh ULID")

	clone.Runs[0].Sections[0].MinKey[0] = "MUTATED"
	clone.SourceIndexPaths[0] = "MUTATED"

	require.Equal(t, []string{"a"}, orig.Runs[0].Sections[0].MinKey,
		"Clone must deep-copy nested SectionRefs")
	require.Equal(t, "idx/x.idx", orig.SourceIndexPaths[0],
		"Clone must deep-copy SourceIndexPaths")
}

func TestLogMerge_Clone_AllowsNilRuns(t *testing.T) {
	orig := &LogMerge{NodeID: ulid.Make()}
	cloned := orig.Clone().(*LogMerge)
	require.Nil(t, cloned.Runs)
	require.Nil(t, cloned.SourceIndexPaths)
}

func TestLogMerge_Type(t *testing.T) {
	require.Equal(t, NodeTypeLogMerge, (&LogMerge{}).Type())
	require.Equal(t, "LogMerge", NodeTypeLogMerge.String())
}
