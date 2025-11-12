package physical

import (
	"context"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func TestPrinter(t *testing.T) {
	t.Run("simple tree", func(t *testing.T) {
		p := &Plan{}

		limit := p.graph.Add(&Limit{})
		filter := p.graph.Add(&Filter{})
		scanSet := p.graph.Add(&ScanSet{
			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
				{Type: ScanTypeDataObject, DataObject: &DataObjScan{}},
			},
		})

		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanSet})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		p := &Plan{}

		limit1 := p.graph.Add(&Limit{})
		scan1 := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit1, Child: scan1})

		limit2 := p.graph.Add(&Limit{})
		scan2 := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit2, Child: scan2})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("multiple parents sharing the same child node", func(t *testing.T) {
		p := &Plan{}
		limit := p.graph.Add(&Limit{})
		filter1 := p.graph.Add(&Limit{})
		filter2 := p.graph.Add(&Limit{})
		scan := p.graph.Add(&DataObjScan{})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter1})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter2})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter1, Child: scan})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter2, Child: scan})

		repr := PrintAsTree(p)
		t.Log("\n" + repr)
	})

	t.Run("plan with metrics from captures", func(t *testing.T) {
		p := &Plan{}

		limit := p.graph.Add(&Limit{NodeID: ulid.Make()})
		filter := p.graph.Add(&Filter{NodeID: ulid.Make()})
		dataObjScan1 := &DataObjScan{NodeID: ulid.Make()}
		dataObjScan2 := &DataObjScan{NodeID: ulid.Make()}
		scanSet := p.graph.Add(&ScanSet{
			NodeID: ulid.Make(),
			Targets: []*ScanTarget{
				{Type: ScanTypeDataObject, DataObject: dataObjScan1},
				{Type: ScanTypeDataObject, DataObject: dataObjScan2},
			},
		})

		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: limit, Child: filter})
		_ = p.graph.AddEdge(dag.Edge[Node]{Parent: filter, Child: scanSet})

		// Create a capture with observations
		ctx, capture := xcap.NewCapture(context.Background(), nil)
		defer capture.End()

		bytesRead := xcap.NewStatisticInt64("bytes_read", xcap.AggregationTypeSum)
		rowsProcessed := xcap.NewStatisticInt64("rows_processed", xcap.AggregationTypeSum)

		_, limitScope := xcap.StartScope(ctx, scopeName(limit))
		limitScope.Record(bytesRead.Observe(1024))
		limitScope.Record(rowsProcessed.Observe(100))
		limitScope.End()

		_, filterScope := xcap.StartScope(ctx, scopeName(filter))
		filterScope.Record(bytesRead.Observe(2048))
		filterScope.Record(rowsProcessed.Observe(50))
		filterScope.End()

		_, dataObjScan1Scope := xcap.StartScope(ctx, scopeName(dataObjScan1))
		dataObjScan1Scope.Record(bytesRead.Observe(4096))
		dataObjScan1Scope.Record(rowsProcessed.Observe(200))
		dataObjScan1Scope.End()

		_, dataObjScan2Scope := xcap.StartScope(ctx, scopeName(dataObjScan2))
		dataObjScan2Scope.Record(bytesRead.Observe(8192))
		dataObjScan2Scope.Record(rowsProcessed.Observe(400))
		dataObjScan2Scope.End()

		repr := PrintAsTreeWithMetrics(p, capture)
		t.Log("\n" + repr)

		// Build expected output - note that node IDs are not in the output, only types
		// With two targets, we should see two DataObjScan nodes
		expected := `
Limit offset=0 limit=0
│   └── @metrics bytes_read=1024 rows_processed=100
└── Filter
    │   └── @metrics bytes_read=2048 rows_processed=50
    └── ScanSet num_targets=2
        ├── DataObjScan location= streams=0 section_id=0 projections=()
        │       ├── @max_time_range start=0001-01-01T00:00:00Z end=0001-01-01T00:00:00Z
        │       └── @metrics bytes_read=4096 rows_processed=200
        └── DataObjScan location= streams=0 section_id=0 projections=()
                ├── @max_time_range start=0001-01-01T00:00:00Z end=0001-01-01T00:00:00Z
                └── @metrics bytes_read=8192 rows_processed=400
`

		require.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(repr))
	})
}
