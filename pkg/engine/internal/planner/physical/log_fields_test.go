package physical

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

func TestLogFields(t *testing.T) {
	window := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	runs := []*compactionv2pb.RunRef{
		{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "a", SectionIndex: 0}, {ObjectPath: "a", SectionIndex: 1}}},
		{Sections: []*compactionv2pb.SectionRef{{ObjectPath: "b"}, {ObjectPath: "c"}}},
	}

	tests := []struct {
		name string
		node Node
		want []any
	}{
		{
			name: "index merge",
			node: &IndexMerge{Tenant: "t", ToCWindowStart: window.UnixNano(), Runs: runs},
			want: []any{"window", window, "input_objects", 3},
		},
		{
			name: "log merge",
			node: &LogMerge{Tenant: "t", ToCWindowStart: window.UnixNano(), Runs: runs, InputBytes: 1024},
			want: []any{"window", window, "input_objects", 3, "input_bytes", int64(1024)},
		},
		{
			name: "index filter",
			node: &IndexFilter{Tenant: "t"},
			want: nil,
		},
		{
			name: "deletion sweep",
			node: &DeletionSweep{Tenant: "t"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, LogFields(tt.node))
		})
	}
}
