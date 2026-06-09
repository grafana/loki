package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestBuildPipelineNodes(t *testing.T) {
	// Stable IDs so expectations can reference them by name. external is never
	// added to the input set, so any region parented to it has no operator
	// parent.
	var (
		external = xcap.NewID()
		rootID   = xcap.NewID()
		leftID   = xcap.NewID()
		rightID  = xcap.NewID()
		grandID  = xcap.NewID()
	)

	tests := []struct {
		name  string
		input []pipelineRegionStat
		want  map[xcap.ID]pipelineNode
	}{
		{
			name: "operator tree with self-time, batches_in and parent linkage",
			input: []pipelineRegionStat{
				// Root operator; its parent is a non-operator region.
				{ID: rootID, ParentID: external, Name: "Limit.Read", ReadCalls: 10, RowsOut: 1000, ReadDuration: 100 * time.Millisecond},
				// Two direct children of root.
				{ID: leftID, ParentID: rootID, Name: "Filter.Read", ReadCalls: 4, RowsOut: 400, ReadDuration: 30 * time.Millisecond},
				{ID: rightID, ParentID: rootID, Name: "DataObjScan.Read", ReadCalls: 3, RowsOut: 300, ReadDuration: 20 * time.Millisecond},
				// Grandchild under the left child.
				{ID: grandID, ParentID: leftID, Name: "Projection.Read", ReadCalls: 2, RowsOut: 200, ReadDuration: 25 * time.Millisecond},
			},
			want: map[xcap.ID]pipelineNode{
				rootID: {
					OperatorID:       rootID,
					ParentOperatorID: xcap.ID{}, // parent is a non-operator region.
					OpType:           "Limit",
					SelfDuration:     50 * time.Millisecond, // 100 - (30 + 20)
					BatchesOut:       10,
					BatchesIn:        7, // 4 + 3
					RowsOut:          1000,
				},
				leftID: {
					OperatorID:       leftID,
					ParentOperatorID: rootID,
					OpType:           "Filter",
					SelfDuration:     5 * time.Millisecond, // 30 - 25
					BatchesOut:       4,
					BatchesIn:        2,
					RowsOut:          400,
				},
				rightID: {
					OperatorID:       rightID,
					ParentOperatorID: rootID,
					OpType:           "DataObjScan",
					SelfDuration:     20 * time.Millisecond, // no children
					BatchesOut:       3,
					BatchesIn:        0,
					RowsOut:          300,
				},
				grandID: {
					OperatorID:       grandID,
					ParentOperatorID: leftID,
					OpType:           "Projection",
					SelfDuration:     25 * time.Millisecond, // no children
					BatchesOut:       2,
					BatchesIn:        0,
					RowsOut:          200,
				},
			},
		},
		{
			name: "self-time clamps at zero when children exceed parent",
			input: []pipelineRegionStat{
				{ID: rootID, ParentID: external, Name: "Merge.Read", ReadCalls: 1, ReadDuration: 10 * time.Millisecond},
				{ID: leftID, ParentID: rootID, Name: "Filter.Read", ReadCalls: 1, ReadDuration: 40 * time.Millisecond},
			},
			want: map[xcap.ID]pipelineNode{
				rootID: {
					OperatorID:   rootID,
					OpType:       "Merge",
					SelfDuration: 0, // 10 - 40, clamped
					BatchesOut:   1,
					BatchesIn:    1,
				},
				leftID: {
					OperatorID:       leftID,
					ParentOperatorID: rootID,
					OpType:           "Filter",
					SelfDuration:     40 * time.Millisecond,
					BatchesOut:       1,
				},
			},
		},
		{
			name:  "no regions yields no nodes",
			input: nil,
			want:  map[xcap.ID]pipelineNode{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPipelineNodes(tt.input)

			// Exactly one record per input region.
			require.Len(t, got, len(tt.want), "one record per node")

			byID := make(map[xcap.ID]pipelineNode, len(got))
			for _, n := range got {
				_, dup := byID[n.OperatorID]
				require.False(t, dup, "duplicate record for operator %s", n.OperatorID)
				byID[n.OperatorID] = n
			}
			require.Equal(t, tt.want, byID)
		})
	}
}
