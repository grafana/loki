package worker

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestPackPipelineNodes(t *testing.T) {
	var (
		rootID = xcap.NewID()
		midID  = xcap.NewID()
		leafID = xcap.NewID()
		absent = xcap.NewID()
	)

	tests := []struct {
		name  string
		nodes []pipelineNode
		want  string
	}{
		{
			name: "root and child link by index",
			nodes: []pipelineNode{
				{OperatorID: rootID, OpType: "Limit", SelfDuration: 50 * time.Millisecond, BatchesOut: 10, BatchesIn: 4, RowsIn: 400, RowsOut: 1000},
				{OperatorID: leafID, ParentOperatorID: rootID, OpType: "Projection/PARSE_LOGFMT", SelfDuration: 30 * time.Millisecond, BatchesOut: 4, RowsOut: 400},
			},
			want: `[
				{"op":"Limit","parent":-1,"self_ms":50,"batches_in":4,"batches_out":10,"rows_in":400,"rows_out":1000},
				{"op":"Projection/PARSE_LOGFMT","parent":0,"self_ms":30,"batches_in":0,"batches_out":4,"rows_in":0,"rows_out":400}
			]`,
		},
		{
			name: "multi-level tree links to the right index",
			nodes: []pipelineNode{
				{OperatorID: rootID, OpType: "Limit"},
				{OperatorID: midID, ParentOperatorID: rootID, OpType: "Filter"},
				{OperatorID: leafID, ParentOperatorID: midID, OpType: "DataObjScan"},
			},
			want: `[
				{"op":"Limit","parent":-1,"self_ms":0,"batches_in":0,"batches_out":0,"rows_in":0,"rows_out":0},
				{"op":"Filter","parent":0,"self_ms":0,"batches_in":0,"batches_out":0,"rows_in":0,"rows_out":0},
				{"op":"DataObjScan","parent":1,"self_ms":0,"batches_in":0,"batches_out":0,"rows_in":0,"rows_out":0}
			]`,
		},
		{
			name: "parent not in set falls back to -1",
			nodes: []pipelineNode{
				{OperatorID: leafID, ParentOperatorID: absent, OpType: "DataObjScan"},
			},
			want: `[{"op":"DataObjScan","parent":-1,"self_ms":0,"batches_in":0,"batches_out":0,"rows_in":0,"rows_out":0}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(packPipelineNodes(tt.nodes))
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(b))
		})
	}
}

func TestLogPipelineTrace_NoNodesEmitsNothing(t *testing.T) {
	var buf bytes.Buffer
	logPipelineTrace(log.NewLogfmtLogger(&buf), nil)
	require.Empty(t, buf.String())
}

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
					RowsIn:           700, // 400 + 300
				},
				leftID: {
					OperatorID:       leftID,
					ParentOperatorID: rootID,
					OpType:           "Filter",
					SelfDuration:     5 * time.Millisecond, // 30 - 25
					BatchesOut:       4,
					BatchesIn:        2,
					RowsOut:          400,
					RowsIn:           200,
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
