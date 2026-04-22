package workflow

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// TODO(rfratto): Once physical plans can be serializable, we should be able to
// write these tests using serialized physical plans rather than constructing
// them with the DAG.
//
// That would also allow for writing tests using txtar[^1], which may be easier
// to write and maintain.
//
// [^1]: https://research.swtch.com/testing

func Test_planWorkflow(t *testing.T) {
	t.Run("no pipeline breakers", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]
		physicalGraph.Add(&physical.DataObjScan{
			MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
		})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 1, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│
│ DataObjScan location= streams=0 section_id=0 projections=()
│     └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("ends with one pipeline breaker", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			scan = physicalGraph.Add(&physical.DataObjScan{
				MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
			})
			rangeAgg = physicalGraph.Add(&physical.RangeAggregation{
				Start: time.Unix(30, 0).UTC(),
				End:   time.Unix(45, 0).UTC(),
			})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 1, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z
│
│ RangeAggregation operation=invalid start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│ └── DataObjScan location= streams=0 section_id=0 projections=()
│         └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("split on pipeline breaker", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			scan = physicalGraph.Add(&physical.DataObjScan{
				MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
			})
			rangeAgg = physicalGraph.Add(&physical.RangeAggregation{
				Start: time.Unix(30, 0).UTC(),
				End:   time.Unix(45, 0).UTC(),
			})
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scan})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

		physicalPlan := physical.FromGraph(physicalGraph)
		physicalPlan, err := physical.WrapWithBatching(physicalPlan, 500)
		require.NoError(t, err)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 2, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         └── @source stream=00000000000000000000000003
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000003
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location= streams=0 section_id=0 projections=()
│             └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))

		t.Run("with caching", func(t *testing.T) {
			ulidGen := ulidGenerator{}

			graph, err := planWorkflow("", physicalPlan, cacheParams{
				enabled:                 true,
				taskCacheMaxSizeBytes:   1 * 1024 * 1024,
				dataObjScanMaxSizeBytes: 0,
			}, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, 2, graph.Len())
			requireUniqueStreams(t, graph)
			generateConsistentULIDs(&ulidGen, graph)

			expectOutput := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         └── @source stream=00000000000000000000000003
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=0dbee591f4bc143d key= |>>| Batching |>>| RangeAggregation{operation=invalid,start=1970-01-01T00:00:30Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000003
│ └── Batching batch_size=500
│     └── RangeAggregation operation=invalid start=1970-01-01T00:00:30Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=309bb327739e663a key= |>>| DataObjScan{location=,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│             └── DataObjScan location= streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
`)

			actualOutput := Sprint(&Workflow{graph: graph})
			require.Equal(t, strings.TrimSpace(expectOutput), strings.TrimSpace(actualOutput))
		})
	})

	t.Run("split on parallelize", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
			rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{
				Start: time.Unix(5, 0).UTC(),
				End:   time.Unix(45, 0).UTC(),
			})

			parallelize = physicalGraph.Add(&physical.Parallelize{})

			filter  = physicalGraph.Add(&physical.Filter{})
			project = physicalGraph.Add(
				&physical.Projection{
					Expressions: []physical.Expression{
						&physical.VariadicExpr{
							Op: types.VariadicOpParseLogfmt,
							Expressions: []physical.Expression{
								&physical.ColumnExpr{
									Ref: semconv.ColumnIdentMessage.ColumnRef(),
								},
							},
						},
					},
					All:    true,
					Expand: true,
				},
			)
			scanSet = physicalGraph.Add(&physical.ScanSet{
				Targets: []*physical.ScanTarget{
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "a",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
						},
					},
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "b",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(20, 0).UTC(), End: time.Unix(60, 0).UTC()},
						},
					},
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "c",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(0, 0).UTC(), End: time.Unix(50, 0).UTC()},
						},
					},
				},
			})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallelize})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: filter})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: filter, Child: project})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: project, Child: scanSet})

		physicalPlan := physical.FromGraph(physicalGraph)
		physicalPlan, err := physical.WrapWithBatching(physicalPlan, 500)
		require.NoError(t, err)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 5, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         └── @source stream=00000000000000000000000006
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000006
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         ├── @source stream=00000000000000000000000007
│         ├── @source stream=00000000000000000000000008
│         └── @source stream=00000000000000000000000009
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000007
│ └── Filter
│     └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│         └── DataObjScan location=a streams=0 section_id=0 projections=()
│                 └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000004
│ @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000008
│ └── Filter
│     └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│         └── DataObjScan location=b streams=0 section_id=0 projections=()
│                 └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
┌ Task 00000000000000000000000005
│ @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:50Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000009
│ └── Filter
│     └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│         └── DataObjScan location=c streams=0 section_id=0 projections=()
│                 └── @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:50Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))

		t.Run("with caching", func(t *testing.T) {
			ulidGen := ulidGenerator{}

			graph, err := planWorkflow("", physicalPlan, cacheParams{
				enabled:                 true,
				taskCacheMaxSizeBytes:   1 * 1024 * 1024,
				dataObjScanMaxSizeBytes: 0,
			}, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, 5, graph.Len())
			requireUniqueStreams(t, graph)
			generateConsistentULIDs(&ulidGen, graph)

			expectOutput := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         └── @source stream=00000000000000000000000006
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000006
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         ├── @source stream=00000000000000000000000007
│         ├── @source stream=00000000000000000000000008
│         └── @source stream=00000000000000000000000009
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=c993ba951c1c73ef key= |>>| Batching |>>| Filter{predicates=[]} |>>| Projection{all=true,mode=expand,expressions=[PARSE_LOGFMT(builtin.message)]} |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000007
│ └── Batching batch_size=500
│     └── Filter
│         └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│             └── Cache max_cacheable_size=0 B hashed_key=4a6082f4d1b54ead key= |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│                 └── DataObjScan location=a streams=0 section_id=0 projections=()
│                         └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000004
│ @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=9332cb1201f497a1 key= |>>| Batching |>>| Filter{predicates=[]} |>>| Projection{all=true,mode=expand,expressions=[PARSE_LOGFMT(builtin.message)]} |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│ │   └── @sink stream=00000000000000000000000008
│ └── Batching batch_size=500
│     └── Filter
│         └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│             └── Cache max_cacheable_size=0 B hashed_key=57460db08f81adaf key= |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│                 └── DataObjScan location=b streams=0 section_id=0 projections=()
│                         └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
┌ Task 00000000000000000000000005
│ @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:50Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=aea367473a8b42dc key= |>>| Batching |>>| Filter{predicates=[]} |>>| Projection{all=true,mode=expand,expressions=[PARSE_LOGFMT(builtin.message)]} |>>| DataObjScan{location=c,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:00Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000009
│ └── Batching batch_size=500
│     └── Filter
│         └── Projection all=true expand=(PARSE_LOGFMT(builtin.message))
│             └── Cache max_cacheable_size=0 B hashed_key=61a13f0daaf5f8fe key= |>>| DataObjScan{location=c,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:00Z,max_time_range_end=1970-01-01T00:00:50Z}
│                 └── DataObjScan location=c streams=0 section_id=0 projections=()
│                         └── @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:50Z
└
`)

			actualOutput := Sprint(&Workflow{graph: graph})
			require.Equal(t, strings.TrimSpace(expectOutput), strings.TrimSpace(actualOutput))
		})
	})

	t.Run("split vector aggregation parallelism", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var (
			physicalGraph dag.Graph[physical.Node]
			vecAgg        = physicalGraph.Add(&physical.VectorAggregation{
				Operation: types.VectorAggregationTypeSum,
			})
			parallelize = physicalGraph.Add(&physical.Parallelize{})
			rangeAgg    = physicalGraph.Add(&physical.RangeAggregation{
				Operation: types.RangeAggregationTypeCount,
				Start:     time.Unix(5, 0).UTC(),
				End:       time.Unix(45, 0).UTC(),
			})
			scanSet = physicalGraph.Add(&physical.ScanSet{
				Targets: []*physical.ScanTarget{
					{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
						Location:     "a",
						MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()}},
					},
					{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
						Location:     "b",
						MaxTimeRange: physical.TimeRange{Start: time.Unix(20, 0).UTC(), End: time.Unix(60, 0).UTC()}},
					},
				},
			})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vecAgg, Child: parallelize})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: rangeAgg})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scanSet})

		physicalPlan := physical.FromGraph(physicalGraph)
		physicalPlan, err := physical.WrapWithBatching(physicalPlan, 500)
		require.NoError(t, err)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 3, graph.Len()) // 1 global + 2 local (one per scan target)
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		// The workflow should have:
		// - Task 1: Global VectorAgg reading from 2 streams (one per partition)
		// - Task 2: RangeAgg + Scan for partition "a"
		// - Task 3: RangeAgg + Scan for partition "b"
		// All task fragments are wrapped with a Batching node.
		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         ├── @source stream=00000000000000000000000004
│         └── @source stream=00000000000000000000000005
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000004
│ └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location=a streams=0 section_id=0 projections=()
│             └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000005
│ └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location=b streams=0 section_id=0 projections=()
│             └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))

		t.Run("with caching", func(t *testing.T) {
			ulidGen := ulidGenerator{}

			graph, err := planWorkflow("", physicalPlan, cacheParams{
				enabled:                 true,
				taskCacheMaxSizeBytes:   1 * 1024 * 1024,
				dataObjScanMaxSizeBytes: 0,
			}, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, 3, graph.Len())
			requireUniqueStreams(t, graph)
			generateConsistentULIDs(&ulidGen, graph)

			expectOutput := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         ├── @source stream=00000000000000000000000004
│         └── @source stream=00000000000000000000000005
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=502e8c56ecf762f4 key= |>>| Batching |>>| RangeAggregation{operation=count,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000004
│ └── Batching batch_size=500
│     └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=4a6082f4d1b54ead key= |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│             └── DataObjScan location=a streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=9888d9ec9f19e11e key= |>>| Batching |>>| RangeAggregation{operation=count,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│ │   └── @sink stream=00000000000000000000000005
│ └── Batching batch_size=500
│     └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=57460db08f81adaf key= |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│             └── DataObjScan location=b streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
`)

			actualOutput := Sprint(&Workflow{graph: graph})
			require.Equal(t, strings.TrimSpace(expectOutput), strings.TrimSpace(actualOutput))
		})
	})

	t.Run("Predicates on the ScanSet are combined with the predicate pushed down to the DataObjScan", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		parallelize := physicalGraph.Add(&physical.Parallelize{})
		topk := physicalGraph.Add(&physical.TopK{
			SortBy:    &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
			Ascending: false,
			K:         100,
		})
		scanSet := physicalGraph.Add(&physical.ScanSet{
			Predicates: []physical.Expression{
				&physical.BinaryExpr{
					Left:  &physical.ColumnExpr{Ref: semconv.ColumnIdentTimestamp.ColumnRef()},
					Right: physical.NewLiteral(time.Unix(5, 0).UTC().UnixNano()),
					Op:    types.BinaryOpGt,
				},
			},
			Targets: []*physical.ScanTarget{
				{
					Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
						Location:     "a",
						MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
						Predicates: []physical.Expression{
							&physical.BinaryExpr{
								Left:  &physical.ColumnExpr{Ref: semconv.ColumnIdentError.ColumnRef()},
								Right: physical.NewLiteral(""),
								Op:    types.BinaryOpNeq,
							},
						},
					},
				},
			},
		})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: topk, Child: parallelize})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scanSet})

		physicalPlan := physical.FromGraph(physicalGraph)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 2, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│
│ TopK sort_by=builtin.timestamp ascending=false nulls_first=false k=100
│     └── @source stream=00000000000000000000000003
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│
│ DataObjScan location=a streams=0 section_id=0 projections=() predicate[0]=GT(builtin.timestamp, 5000000000) predicate[1]=NEQ(generated.__error__, "")
│     ├── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
│     └── @sink stream=00000000000000000000000003
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))
	})

	t.Run("clamp scanset predicates and rangeaggregation", func(t *testing.T) {
		ulidGen := ulidGenerator{}

		var physicalGraph dag.Graph[physical.Node]

		var (
			vectorAgg = physicalGraph.Add(&physical.VectorAggregation{})
			rangeAgg  = physicalGraph.Add(&physical.RangeAggregation{
				Start: time.Unix(5, 0).UTC(),
				End:   time.Unix(45, 0).UTC(),
			})

			parallelize = physicalGraph.Add(&physical.Parallelize{})

			predA = &physical.BinaryExpr{Left: &physical.ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral(types.Timestamp(60 * 1000 * 1000 * 1000)), Op: types.BinaryOpLt}
			predB = &physical.BinaryExpr{Left: &physical.ColumnExpr{Ref: types.ColumnRef{Column: types.ColumnNameBuiltinTimestamp, Type: types.ColumnTypeBuiltin}},
				Right: physical.NewLiteral(types.Timestamp(18 * 1000 * 1000 * 1000)), Op: types.BinaryOpGt}

			scanSet = physicalGraph.Add(&physical.ScanSet{
				Targets: []*physical.ScanTarget{
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "a",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
							Predicates:   []physical.Expression{predA},
						},
					},
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "b",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(20, 0).UTC(), End: time.Unix(60, 0).UTC()},
							Predicates:   []physical.Expression{predB},
						},
					},
					{
						Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
							Location:     "c",
							MaxTimeRange: physical.TimeRange{Start: time.Unix(0, 0).UTC(), End: time.Unix(40, 0).UTC()},
						},
					},
				},
			})
		)

		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: parallelize})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: rangeAgg})
		_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scanSet})

		physicalPlan := physical.FromGraph(physicalGraph)
		physicalPlan, err := physical.WrapWithBatching(physicalPlan, 500)
		require.NoError(t, err)

		graph, err := planWorkflow("", physicalPlan, cacheParams{}, log.NewNopLogger())
		require.NoError(t, err)
		require.Equal(t, 4, graph.Len())
		requireUniqueStreams(t, graph)
		generateConsistentULIDs(&ulidGen, graph)

		expectOuptut := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         ├── @source stream=00000000000000000000000005
│         ├── @source stream=00000000000000000000000006
│         └── @source stream=00000000000000000000000007
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000005
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location=a streams=0 section_id=0 projections=() predicate[0]=LTE(builtin.timestamp, 1970-01-01T00:00:50Z)
│             └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000006
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location=b streams=0 section_id=0 projections=() predicate[0]=GTE(builtin.timestamp, 1970-01-01T00:00:20Z)
│             └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
┌ Task 00000000000000000000000004
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ │   └── @sink stream=00000000000000000000000007
│ └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│     └── DataObjScan location=c streams=0 section_id=0 projections=()
│             └── @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:40Z
└
`)

		actualOutput := Sprint(&Workflow{graph: graph})
		require.Equal(t, strings.TrimSpace(expectOuptut), strings.TrimSpace(actualOutput))

		t.Run("with caching", func(t *testing.T) {
			ulidGen := ulidGenerator{}

			graph, err := planWorkflow("", physicalPlan, cacheParams{enabled: true, taskCacheMaxSizeBytes: 1 * 1024 * 1024, dataObjScanMaxSizeBytes: 1 * 1024 * 1024}, log.NewNopLogger())
			require.NoError(t, err)
			require.Equal(t, 4, graph.Len())
			requireUniqueStreams(t, graph)
			generateConsistentULIDs(&ulidGen, graph)

			expectOutput := strings.TrimSpace(`
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=invalid group_by=()
│         ├── @source stream=00000000000000000000000005
│         ├── @source stream=00000000000000000000000006
│         └── @source stream=00000000000000000000000007
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=beb71f7dcfcd7095 key= |>>| Batching |>>| RangeAggregation{operation=invalid,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[LTE(builtin.timestamp, 1970-01-01T00:00:50Z)],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000005
│ └── Batching batch_size=500
│     └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=1.0 MiB hashed_key=cfbb813ac32eac14 key= |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[LTE(builtin.timestamp, 1970-01-01T00:00:50Z)],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│             └── DataObjScan location=a streams=0 section_id=0 projections=() predicate[0]=LTE(builtin.timestamp, 1970-01-01T00:00:50Z)
│                     └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└
┌ Task 00000000000000000000000003
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=edb84a0ddd883413 key= |>>| Batching |>>| RangeAggregation{operation=invalid,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[GTE(builtin.timestamp, 1970-01-01T00:00:20Z)],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│ │   └── @sink stream=00000000000000000000000006
│ └── Batching batch_size=500
│     └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=1.0 MiB hashed_key=ec866af4d1638d82 key= |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[GTE(builtin.timestamp, 1970-01-01T00:00:20Z)],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│             └── DataObjScan location=b streams=0 section_id=0 projections=() predicate[0]=GTE(builtin.timestamp, 1970-01-01T00:00:20Z)
│                     └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└
┌ Task 00000000000000000000000004
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=ea0acf82724b9cfa key= |>>| Batching |>>| RangeAggregation{operation=invalid,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=c,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:00Z,max_time_range_end=1970-01-01T00:00:40Z}
│ │   └── @sink stream=00000000000000000000000007
│ └── Batching batch_size=500
│     └── RangeAggregation operation=invalid start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=1.0 MiB hashed_key=acadc504b7eeee93 key= |>>| DataObjScan{location=c,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:00Z,max_time_range_end=1970-01-01T00:00:40Z}
│             └── DataObjScan location=c streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:00Z end=1970-01-01T00:00:40Z
└
`)

			actualOutput := Sprint(&Workflow{graph: graph})
			require.Equal(t, strings.TrimSpace(expectOutput), strings.TrimSpace(actualOutput))
		})
	})

}

// requireUniqueStreams asserts that for each stream found in g, that stream has
// exactly one reader (from Task.Sources) and exactly one writer (from
// Task.Sinks).
func requireUniqueStreams(t testing.TB, g dag.Graph[*Task]) {
	t.Helper()

	type streamInfo struct {
		Reader physical.Node
		Writer physical.Node
	}

	streamInfos := make(map[*Stream]*streamInfo)

	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			for node, streams := range task.Sources {
				for _, stream := range streams {
					info, ok := streamInfos[stream]
					if !ok {
						info = new(streamInfo)
						streamInfos[stream] = info
					}

					require.Nil(t, info.Reader, "each stream must have exactly one reader")
					info.Reader = node
				}
			}

			for node, streams := range task.Sinks {
				for _, stream := range streams {
					info, ok := streamInfos[stream]
					if !ok {
						info = new(streamInfo)
						streamInfos[stream] = info
					}

					require.Nil(t, info.Writer, "each stream must have exactly one writer")
					info.Writer = node
				}
			}

			return nil
		}, dag.PreOrderWalk)
	}

	for _, info := range streamInfos {
		require.NotNil(t, info.Reader, "each stream must have exactly one reader")
		require.NotNil(t, info.Writer, "each stream must have exactly one writer")
	}
}

// generateConsistentULIDs reassigns ULIDs to all tasks and streams in the graph
// using the ULID generator for consistent output.
func generateConsistentULIDs(gen *ulidGenerator, g dag.Graph[*Task]) {
	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			task.ULID = gen.Make()
			return nil
		}, dag.PreOrderWalk)
	}

	// For easier debugging, we do a second pass for all the streams. That way
	// we know stream ULIDs start after task ULIDs.
	for _, root := range g.Roots() {
		_ = g.Walk(root, func(task *Task) error {
			for _, streams := range task.Sources {
				for _, stream := range streams {
					stream.ULID = gen.Make()
				}
			}
			return nil
		}, dag.PreOrderWalk)
	}
}

// Test_eliminateEmptyCachedTasks verifies that tasks whose cached result is
// empty are removed from the workflow graph at plan time.
func Test_pruneCachedTasks(t *testing.T) {
	// Create a physical plan:
	//    VectorAggregation --> Parallelize --> RangeAggr --> ScanSet{DataAbjScan(A), DataAbjScan(B)}
	// That will translate into three tasks:
	//   - Task 1 (root): global VectorAggregation
	//   - Task 2 (leaf): RangeAggregation + DataObjScan for location "a"
	//   - Task 3 (leaf): RangeAggregation + DataObjScan for location "b"
	var physicalGraph dag.Graph[physical.Node]
	var (
		vecAgg      = physicalGraph.Add(&physical.VectorAggregation{Operation: types.VectorAggregationTypeSum})
		parallelize = physicalGraph.Add(&physical.Parallelize{})
		rangeAgg    = physicalGraph.Add(&physical.RangeAggregation{
			Operation: types.RangeAggregationTypeCount,
			Start:     time.Unix(5, 0).UTC(),
			End:       time.Unix(45, 0).UTC(),
		})
		scanSet = physicalGraph.Add(&physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
					Location:     "a",
					MaxTimeRange: physical.TimeRange{Start: time.Unix(10, 0).UTC(), End: time.Unix(50, 0).UTC()},
				}},
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{
					Location:     "b",
					MaxTimeRange: physical.TimeRange{Start: time.Unix(20, 0).UTC(), End: time.Unix(60, 0).UTC()},
				}},
			},
		})
	)
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: vecAgg, Child: parallelize})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: rangeAgg})
	_ = physicalGraph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: scanSet})
	physicalPlan := physical.FromGraph(physicalGraph)
	physicalPlan, err := physical.WrapWithBatching(physicalPlan, 500)
	require.NoError(t, err)

	cacheP := cacheParams{
		enabled:               true,
		taskCacheMaxSizeBytes: 1 << 20,
		// dataObjScanMaxSizeBytes=0: DataObjScan Cache nodes are still injected
		// (with max_cacheable_size=0 B) so their keys can be used for elimination.
		dataObjScanMaxSizeBytes:     0,
		pruneEmptyCachedTasks:       true,
		nonEmptyCachedTasksMaxBytes: math.MaxUint64,
	}

	// Extract raw cache keys for every task in graph traversal order.
	// tasks[0] = root (VecAgg, no graph parents), tasks[1] = leaf "a", tasks[2] = leaf "b".
	// Each entry maps cache-type name → raw key for that task.
	baseGraph, err := planWorkflow("", physicalPlan, cacheP, log.NewNopLogger())
	require.NoError(t, err)
	require.Equal(t, 3, baseGraph.Len())

	// Collect cache keys from all tasks in the workflow
	var tasks []map[physical.TaskCacheName]string
	for _, root := range baseGraph.Roots() {
		_ = baseGraph.Walk(root, func(task *Task) error {
			keys := make(map[physical.TaskCacheName]string)
			if fragRoot, err := task.Fragment.Root(); err == nil {
				if cn, ok := fragRoot.(*physical.Cache); ok {
					keys[cn.CacheName] = cn.Key
				}
			}
			for n := range task.Fragment.Graph().Nodes() {
				if cn, ok := n.(*physical.Cache); ok && cn.CacheName == physical.TaskCacheDataObjScanResult {
					keys[cn.CacheName] = cn.Key
				}
			}
			tasks = append(tasks, keys)
			return nil
		}, dag.PreOrderWalk)
	}
	require.Len(t, tasks, 3, "expected three tasks in traversal order")
	require.Len(t, tasks[0], 0, "task[0] must not have any cache keys")
	require.Contains(t, tasks[1], physical.TaskCacheLogsScanRangeAggr, "task[1] must have a task-level cache key")
	require.Contains(t, tasks[2], physical.TaskCacheLogsScanRangeAggr, "task[2] must have a task-level cache key")
	require.Contains(t, tasks[1], physical.TaskCacheDataObjScanResult, "task[1] must have a DataObjScan cache key")
	require.Contains(t, tasks[2], physical.TaskCacheDataObjScanResult, "task[2] must have a DataObjScan cache key")

	baseUlidGen := ulidGenerator{}
	generateConsistentULIDs(&baseUlidGen, baseGraph)
	originalPlan := strings.TrimSpace(Sprint(&Workflow{graph: baseGraph}))

	for _, tc := range []struct {
		name              string
		payloads          map[string][]byte // raw cache key → payload; absent keys = cache miss
		maxNonEmptyBytes  *uint64           // nil = use cacheP default (math.MaxUint64)
		pruneFetchTimeout time.Duration     // 0 = no timeout
		cacheOverride     cache.Cache       // if non-nil, used instead of building from payloads
		expected          string            // expected Sprint output after elimination
	}{
		{
			// All cache keys miss → no elimination; plan is identical to baseGraph.
			name:     "cache miss",
			payloads: nil,
			expected: originalPlan,
		},
		{
			// Both task-level cache keys return non-empty with pruneCachedTasks=true →
			// both leaves eliminated; root gets @cachedSource buffers=2.
			name: "cache non-empty",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
			},
			expected: `
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         └── @cachedSource buffers=2 size=18 B
└`,
		},
		{
			// Task "a" task-level cache returns empty → task "a" eliminated;
			// root and task "b" remain.
			name: "one task empty",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheLogsScanRangeAggr]: emptyResultPayload(),
			},
			expected: `
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         └── @source stream=00000000000000000000000003
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=9888d9ec9f19e11e key= |>>| Batching |>>| RangeAggregation{operation=count,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│ │   └── @sink stream=00000000000000000000000003
│ └── Batching batch_size=500
│     └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=57460db08f81adaf key= |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│             └── DataObjScan location=b streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└`,
		},
		{
			// Task "a" returns empty, task "b" returns non-empty with pruneCachedTasks=true →
			// both leaves eliminated; root gets @cachedSource buffers=1.
			name: "cache one empty one non-empty",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheLogsScanRangeAggr]: emptyResultPayload(),
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
			},
			expected: `
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         └── @cachedSource buffers=1 size=9 B
└`,
		},
		{
			// Scan-level cache non-empty should NOT eliminate the task: the parent
			// expects task-level (aggregated) results, not raw scan data.
			// Plan is identical to the full base graph.
			name: "cache non-empty at scan",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheDataObjScanResult]: nonEmptyResultPayload(),
			},
			expected: originalPlan,
		},
		{
			// Scan-level cache empty should NOT eliminate the task: only root-level
			// cache hits are checked. Plan is identical to the full base graph.
			name: "cache empty at scan",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheDataObjScanResult]: emptyResultPayload(),
			},
			expected: originalPlan,
		},
		{
			// Task "a" is a cache miss → stays as network stream source.
			// Task "b" is non-empty with pruneCachedTasks=true → eliminated; root gets @cachedSource keys=1.
			// Root shows both @source (task "a" stream) and @cachedSource (task "b" result).
			name: "cache one miss one non-empty",
			payloads: map[string][]byte{
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
			},
			expected: `
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         ├── @source stream=00000000000000000000000003
│         └── @cachedSource buffers=1 size=9 B
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=502e8c56ecf762f4 key= |>>| Batching |>>| RangeAggregation{operation=count,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│ │   └── @sink stream=00000000000000000000000003
│ └── Batching batch_size=500
│     └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=4a6082f4d1b54ead key= |>>| DataObjScan{location=a,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:10Z,max_time_range_end=1970-01-01T00:00:50Z}
│             └── DataObjScan location=a streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:10Z end=1970-01-01T00:00:50Z
└`,
		},
		{
			// Both task-level caches return empty → both leaves eliminated; the root
			// is cascade-eliminated because all its sources are gone.
			name: "both tasks empty",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheLogsScanRangeAggr]: emptyResultPayload(),
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: emptyResultPayload(),
			},
			expected: "Empty",
		},
		{
			// Fetch times out mid-flight: task "a" is returned as a partial result
			// (empty hit) before the deadline; task "b" is missed. Task "a" is still
			// eliminated from the partial result; task "b" stays scheduled.
			name: "fetch timeout with partial results",
			cacheOverride: func() cache.Cache {
				mc := newWorkflowMockCache()
				mc.storeHashed(cache.HashKey(tasks[1][physical.TaskCacheLogsScanRangeAggr]), emptyResultPayload())
				return &timeoutMockCache{workflowMockCache: *mc}
			}(),
			pruneFetchTimeout: time.Second,
			expected: `
┌ Task 00000000000000000000000001
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Batching batch_size=500
│ └── VectorAggregation operation=sum group_by=()
│         └── @source stream=00000000000000000000000003
└
┌ Task 00000000000000000000000002
│ @max_time_range start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z
│
│ Cache max_cacheable_size=1.0 MiB hashed_key=9888d9ec9f19e11e key= |>>| Batching |>>| RangeAggregation{operation=count,start=1970-01-01T00:00:05Z,end=1970-01-01T00:00:45Z,step=0s,range=0s,grouping=by=[],max_series=0} |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│ │   └── @sink stream=00000000000000000000000003
│ └── Batching batch_size=500
│     └── RangeAggregation operation=count start=1970-01-01T00:00:05Z end=1970-01-01T00:00:45Z step=0s range=0s group_by=()
│         └── Cache max_cacheable_size=0 B hashed_key=57460db08f81adaf key= |>>| DataObjScan{location=b,section=0,stream_ids=[],projections=[],predicates=[],max_time_range_start=1970-01-01T00:00:20Z,max_time_range_end=1970-01-01T00:01:00Z}
│             └── DataObjScan location=b streams=0 section_id=0 projections=()
│                     └── @max_time_range start=1970-01-01T00:00:20Z end=1970-01-01T00:01:00Z
└`,
		},
		{
			// maxNonEmptyBytes=0 disables non-empty pruning even when both tasks hit.
			// Neither leaf is eliminated; plan is identical to baseGraph.
			name: "non-empty pruning disabled when max size is 0",
			payloads: map[string][]byte{
				tasks[1][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
			},
			maxNonEmptyBytes: ptr[uint64](0),
			expected:         originalPlan,
		},
		{
			// Budget (5 B) is smaller than a single cached result (9 B) → the result
			// is skipped by the greedy bin-packing loop; the task stays scheduled.
			name: "non-empty result skipped when it exceeds size budget",
			payloads: map[string][]byte{
				tasks[2][physical.TaskCacheLogsScanRangeAggr]: nonEmptyResultPayload(),
			},
			maxNonEmptyBytes: ptr[uint64](5),
			expected:         originalPlan,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ulidGen := ulidGenerator{}

			p := cacheP
			if tc.maxNonEmptyBytes != nil {
				p.nonEmptyCachedTasksMaxBytes = *tc.maxNonEmptyBytes
			}
			if tc.pruneFetchTimeout != 0 {
				p.pruneFetchTimeout = tc.pruneFetchTimeout
			}
			if tc.cacheOverride != nil {
				p.registry = executor.NewTestTaskCacheRegistry(map[physical.TaskCacheName]cache.Cache{
					physical.TaskCacheLogsScanRangeAggr: tc.cacheOverride,
					physical.TaskCacheDataObjScanResult: tc.cacheOverride,
				})
			} else if len(tc.payloads) > 0 {
				mc := newWorkflowMockCache()
				for rawKey, payload := range tc.payloads {
					mc.storeHashed(cache.HashKey(rawKey), payload)
				}
				p.registry = executor.NewTestTaskCacheRegistry(map[physical.TaskCacheName]cache.Cache{
					physical.TaskCacheLogsScanRangeAggr: mc,
					physical.TaskCacheDataObjScanResult: mc,
				})
			}

			g, err := planWorkflow("", physicalPlan, p, log.NewNopLogger())
			require.NoError(t, err)
			requireUniqueStreams(t, g)
			generateConsistentULIDs(&ulidGen, g)

			actualOutput := Sprint(&Workflow{graph: g})
			require.Equal(t, strings.TrimSpace(tc.expected), strings.TrimSpace(actualOutput))
		})
	}
}

// Test_pruneCachedTasks_cascadeProtection verifies that a task with a non-empty
// cache hit is not lost when all of its children have empty cache hits.
//
// Graph: root → intermediate (non-empty hit) → [leaf_a (empty), leaf_b (empty)]
//
// Without the fix, the empty-elimination pass cascade-eliminates intermediate
// (because its two children are both eliminated and CachedSources hasn't been
// wired yet), so root never gets intermediate's cached result.
// With the fix, wireCachedSources runs before the empty pass, so root's
// CachedSources is populated before the cascade check runs.
func Test_pruneCachedTasks_cascadeProtection(t *testing.T) {
	const cacheName = physical.TaskCacheLogsScanRangeAggr

	// Build minimal single-node fragment graphs so that Fragment.Root() returns
	// the expected physical node type.
	makeFragment := func(root physical.Node) *physical.Plan {
		var g dag.Graph[physical.Node]
		g.Add(root)
		return physical.FromGraph(g)
	}

	// Physical nodes that serve as map keys for Sources/Sinks/CachedSources.
	rootNode := &physical.VectorAggregation{}
	intermNode := &physical.Cache{CacheName: cacheName, Key: "interm-key"}
	leafANode := &physical.Cache{CacheName: cacheName, Key: "leaf-a-key"}
	leafBNode := &physical.Cache{CacheName: cacheName, Key: "leaf-b-key"}

	// Streams that connect the tasks.
	streamAB := &Stream{ULID: ulid.Make()} // intermediate → root
	streamA := &Stream{ULID: ulid.Make()}  // leaf_a → intermediate
	streamB := &Stream{ULID: ulid.Make()}  // leaf_b → intermediate

	taskRoot := &Task{
		ULID:     ulid.Make(),
		Fragment: makeFragment(rootNode),
		Sources:  map[physical.Node][]*Stream{rootNode: {streamAB}},
	}
	taskInterm := &Task{
		ULID:     ulid.Make(),
		Fragment: makeFragment(intermNode),
		Sources:  map[physical.Node][]*Stream{intermNode: {streamA, streamB}},
		Sinks:    map[physical.Node][]*Stream{intermNode: {streamAB}},
	}
	taskLeafA := &Task{
		ULID:     ulid.Make(),
		Fragment: makeFragment(leafANode),
		Sinks:    map[physical.Node][]*Stream{leafANode: {streamA}},
	}
	taskLeafB := &Task{
		ULID:     ulid.Make(),
		Fragment: makeFragment(leafBNode),
		Sinks:    map[physical.Node][]*Stream{leafBNode: {streamB}},
	}

	// Assemble DAG: root ← intermediate ← [leaf_a, leaf_b]
	var graphTasks dag.Graph[*Task]
	graphTasks.Add(taskRoot)
	graphTasks.Add(taskInterm)
	graphTasks.Add(taskLeafA)
	graphTasks.Add(taskLeafB)
	require.NoError(t, graphTasks.AddEdge(dag.Edge[*Task]{Parent: taskRoot, Child: taskInterm}))
	require.NoError(t, graphTasks.AddEdge(dag.Edge[*Task]{Parent: taskInterm, Child: taskLeafA}))
	require.NoError(t, graphTasks.AddEdge(dag.Edge[*Task]{Parent: taskInterm, Child: taskLeafB}))

	// Cache: intermediate → non-empty, leaf_a → empty, leaf_b → empty.
	mc := newWorkflowMockCache()
	mc.storeHashed(cache.HashKey(intermNode.Key), nonEmptyResultPayload())
	mc.storeHashed(cache.HashKey(leafANode.Key), emptyResultPayload())
	mc.storeHashed(cache.HashKey(leafBNode.Key), emptyResultPayload())

	p := &planner{
		graph: graphTasks,
		streamWriters: map[*Stream]*Task{
			streamAB: taskInterm,
			streamA:  taskLeafA,
			streamB:  taskLeafB,
		},
	}
	cacheOpts := cacheParams{
		enabled:                     true,
		pruneEmptyCachedTasks:       true,
		nonEmptyCachedTasksMaxBytes: math.MaxUint64,
		registry: executor.NewTestTaskCacheRegistry(map[physical.TaskCacheName]cache.Cache{
			cacheName: mc,
		}),
	}

	require.NoError(t, pruneCachedTasks(p, cacheOpts, log.NewNopLogger()))

	// Root and intermediate are the only tasks that should remain.
	// leaf_a and leaf_b are eliminated (empty hits); intermediate is also
	// eliminated but its result is wired into root's CachedSources.
	require.Equal(t, 1, p.graph.Len(), "only root should remain")
	roots := p.graph.Roots()
	require.Len(t, roots, 1)
	require.Equal(t, taskRoot, roots[0], "root must still be in graph")

	// Root must have received intermediate's cached result.
	require.NotEmpty(t, taskRoot.CachedSources, "root must have CachedSources wired")
	require.Empty(t, taskRoot.Sources, "stream from intermediate must be gone from Sources")
}

// workflowMockCache is a simple in-memory cache.Cache for workflow planning tests.
type workflowMockCache struct {
	data map[string][]byte
}

func newWorkflowMockCache() *workflowMockCache {
	return &workflowMockCache{data: make(map[string][]byte)}
}

func (m *workflowMockCache) storeHashed(hashedKey string, buf []byte) {
	m.data[hashedKey] = buf
}

func (m *workflowMockCache) Store(_ context.Context, keys []string, bufs [][]byte) error {
	for i, k := range keys {
		m.data[k] = bufs[i]
	}
	return nil
}

func (m *workflowMockCache) Fetch(_ context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	for _, k := range keys {
		if buf, ok := m.data[k]; ok {
			found = append(found, k)
			bufs = append(bufs, buf)
		} else {
			missing = append(missing, k)
		}
	}
	return
}

func (m *workflowMockCache) Stop() {}

func (m *workflowMockCache) GetCacheType() stats.CacheType { return stats.TaskResultCache }

// timeoutMockCache wraps workflowMockCache and always returns context.DeadlineExceeded
// alongside whatever results were found, simulating a cache backend that times out
// mid-flight after returning partial results.
type timeoutMockCache struct {
	workflowMockCache
}

func (m *timeoutMockCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	found, bufs, missing, _ = m.workflowMockCache.Fetch(ctx, keys)
	err = context.DeadlineExceeded
	return
}

// ptr returns a pointer to v. Used to set optional test-case fields inline.
func ptr[T any](v T) *T { return &v }

// emptyResultPayload returns a zero-length buffer which is treated as an empty cached result.
func emptyResultPayload() []byte { return []byte{} }

// nonEmptyResultPayload returns a non-empty buffer so that the cache entry is not treated as empty.
func nonEmptyResultPayload() []byte {
	// Wire format: [8 bytes count big-endian] [1 byte codec]
	return []byte{0, 0, 0, 0, 0, 0, 0, 1, 0}
}

type ulidGenerator struct {
	lastCounter uint64
}

func (g *ulidGenerator) Make() ulid.ULID {
	g.lastCounter++
	value := g.lastCounter

	// Manually set ULID bytes - counter in the last 8 bytes, zeros in first 8
	var ulidBytes [16]byte
	// Put the counter value in the last 8 bytes (big-endian)
	for i := range 8 {
		ulidBytes[15-i] = byte(value >> (8 * i))
	}
	return ulidBytes
}
