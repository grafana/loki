package physical

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

type unnamedNode struct{}

func (unnamedNode) ID() ulid.ULID  { return ulid.ULID{} }
func (unnamedNode) Type() NodeType { return NodeTypeInvalid }
func (unnamedNode) isNode()        {}

func TestTaskName(t *testing.T) {
	tests := []struct {
		name     string
		node     Node
		wantName string
		wantOK   bool
	}{
		{name: "returns index-merge for an IndexMerge", node: &IndexMerge{}, wantName: "index-merge", wantOK: true},
		{name: "returns log-merge for a LogMerge", node: &LogMerge{}, wantName: "log-merge", wantOK: true},
		{name: "returns index-filter for an IndexFilter", node: &IndexFilter{}, wantName: "index-filter", wantOK: true},
		{name: "returns deletion-sweep for a DeletionSweep", node: &DeletionSweep{}, wantName: "deletion-sweep", wantOK: true},
		{name: "returns false for a node type without a task name", node: unnamedNode{}, wantName: "", wantOK: false},
		{name: "returns false for a nil node", node: nil, wantName: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := TaskName(tt.node)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantName, name)
		})
	}
}
