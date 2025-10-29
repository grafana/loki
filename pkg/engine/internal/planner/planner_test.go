package planner

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

type TestQuery struct {
	statement      string
	start, end     time.Time
	step, interval time.Duration
	direction      logproto.Direction
	limit          uint32
}

// Direction implements logql.Params.
func (q *TestQuery) Direction() logproto.Direction {
	return q.direction
}

// End implements logql.Params.
func (q *TestQuery) End() time.Time {
	return q.end
}

// Start implements logql.Params.
func (q *TestQuery) Start() time.Time {
	return q.start
}

// Limit implements logql.Params.
func (q *TestQuery) Limit() uint32 {
	return q.limit
}

// QueryString implements logql.Params.
func (q *TestQuery) QueryString() string {
	return q.statement
}

// GetExpression implements logql.Params.
func (q *TestQuery) GetExpression() syntax.Expr {
	return syntax.MustParseExpr(q.statement)
}

// CachingOptions implements logql.Params.
func (q *TestQuery) CachingOptions() resultscache.CachingOptions {
	panic("unimplemented")
}

// GetStoreChunks implements logql.Params.
func (q *TestQuery) GetStoreChunks() *logproto.ChunkRefGroup {
	panic("unimplemented")
}

// Interval implements logql.Params.
func (q *TestQuery) Interval() time.Duration {
	panic("unimplemented")
}

// Shards implements logql.Params.
func (q *TestQuery) Shards() []string {
	return []string{"0_of_1"} // 0_of_1 == noShard
}

// Step implements logql.Params.
func (q *TestQuery) Step() time.Duration {
	return q.step
}

var _ logql.Params = (*TestQuery)(nil)

type TestMetastore struct{}

// DataObjects implements metastore.Metastore.
func (t *TestMetastore) DataObjects(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

// Labels implements metastore.Metastore.
func (t *TestMetastore) Labels(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

// Sections implements metastore.Metastore.
func (t *TestMetastore) Sections(_ context.Context, start time.Time, end time.Time, _ []*labels.Matcher, _ []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error) {
	dur := end.Sub(start)
	return []*metastore.DataobjSectionDescriptor{
		{
			SectionKey: metastore.SectionKey{
				ObjectPath: "objects/00/0000000000.dataobj",
				SectionIdx: 0,
			},
			StreamIDs: []int64{1, 3, 5, 7, 9},
			RowCount:  1000,
			Size:      1 << 10,
			Start:     start,
			End:       end.Add(dur / -2),
		},
		{
			SectionKey: metastore.SectionKey{
				ObjectPath: "objects/00/0000000000.dataobj",
				SectionIdx: 1,
			},
			StreamIDs: []int64{1, 3, 5, 7, 9},
			RowCount:  1000,
			Size:      1 << 10,
			Start:     end.Add(dur / -2),
			End:       end,
		},
	}, nil
}

// StreamIDs implements metastore.Metastore.
func (t *TestMetastore) StreamIDs(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, [][]int64, []int, error) {
	panic("unimplemented")
}

// Streams implements metastore.Metastore.
func (t *TestMetastore) Streams(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]*labels.Labels, error) {
	panic("unimplemented")
}

// Values implements metastore.Metastore.
func (t *TestMetastore) Values(_ context.Context, _ time.Time, _ time.Time, _ ...*labels.Matcher) ([]string, error) {
	panic("unimplemented")
}

var _ metastore.Metastore = (*TestMetastore)(nil)

func TestFullQueryPlanning(t *testing.T) {
	metastore := &TestMetastore{}
	testCases := []struct {
		comment  string
		query    string
		expected string
	}{
		{
			comment: "log: limited query",
			query:   `{app="foo"}`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
            └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                └── ScanSet num_targets=2 predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689600000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},}
                        ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                        └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
				`,
		},
		{
			comment: "log: filter query",
			query:   `{app="foo"} | label_foo="bar" |= "baz"`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
            └── Filter predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_EQ,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:label_foo,Type:COLUMN_TYPE_AMBIGUOUS,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:bar,},},},},},},},}
                └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                    └── ScanSet num_targets=2 predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689600000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},} predicate[2]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_MATCH_SUBSTR,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:message,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:baz,},},},},},},},}
                            ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                            └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
	`,
		},
		{
			comment: "log: parse and filter",
			query:   `{app="foo"} |= "bar" | logfmt | level="error"`,
			expected: `
Limit offset=0 limit=1000
└── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
    └── Parallelize
        └── TopK sort_by=&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,} ascending=false nulls_first=false k=1000
            └── Filter predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_EQ,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:level,Type:COLUMN_TYPE_AMBIGUOUS,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:error,},},},},},},},}
                └── ColumnCompat src=COLUMN_TYPE_PARSED dst=COLUMN_TYPE_PARSED collision=COLUMN_TYPE_LABEL
                    └── Parse kind=PARSE_OP_LOGFMT
                        └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                            └── ScanSet num_targets=2 predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689600000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},} predicate[2]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_MATCH_SUBSTR,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:message,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:bar,},},},},},},},}
                                    ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                                    └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
			`,
		},
		{
			// This tests a bunch of optimistaion scearios:
			// - GroupBy pushdown from vector aggregation into range aggregation
			// - Filter node pushdown blocked by parse
			// - Projection pushdown with range aggregation, filter and unwrap as sources, scan and parse as sinks.
			comment: "metric: parse, unwrap and aggregate",
			query:   `sum by (bar) (sum_over_time({app="foo"} | logfmt | request_duration != "" | unwrap duration(request_duration)[1m]))`,
			expected: `
AggregateVector operation=AGGREGATE_VECTOR_OP_SUM group_by=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,})
└── AggregateRange operation=AGGREGATE_RANGE_OP_SUM start=2025-01-01T00:00:00Z end=2025-01-01T01:00:00Z step=0s range=1m0s partition_by=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,})
    └── Parallelize
        └── Projection all=true expand=(&Expression{Kind:&Expression_UnaryExpression{UnaryExpression:&UnaryExpression{Op:UNARY_OP_CAST_DURATION,Value:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:request_duration,Type:COLUMN_TYPE_AMBIGUOUS,},},},},},})
            └── Filter predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_NEQ,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:request_duration,Type:COLUMN_TYPE_AMBIGUOUS,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:,},},},},},},},}
                └── ColumnCompat src=COLUMN_TYPE_PARSED dst=COLUMN_TYPE_PARSED collision=COLUMN_TYPE_LABEL
                    └── Parse kind=PARSE_OP_LOGFMT requested_keys=(bar, request_duration)
                        └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                            └── ScanSet num_targets=2 projections=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,}, &ColumnExpression{Name:message,Type:COLUMN_TYPE_BUILTIN,}, &ColumnExpression{Name:request_duration,Type:COLUMN_TYPE_AMBIGUOUS,}, &ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,}) predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689540000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},}
                                    ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                                    └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
			`,
		},
		{
			comment: `metric: multiple parse stages`,
			query:   `sum(count_over_time({app="foo"} | detected_level="error" | json | logfmt | drop __error__,__error_details__[1m]))`,
			expected: `
AggregateVector operation=AGGREGATE_VECTOR_OP_SUM
└── AggregateRange operation=AGGREGATE_RANGE_OP_COUNT start=2025-01-01T00:00:00Z end=2025-01-01T01:00:00Z step=0s range=1m0s
    └── Parallelize
        └── Projection all=true drop=(&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:__error__,Type:COLUMN_TYPE_AMBIGUOUS,},},}, &Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:__error_details__,Type:COLUMN_TYPE_AMBIGUOUS,},},})
            └── ColumnCompat src=COLUMN_TYPE_PARSED dst=COLUMN_TYPE_PARSED collision=COLUMN_TYPE_LABEL
                └── Parse kind=PARSE_OP_JSON
                    └── ColumnCompat src=COLUMN_TYPE_PARSED dst=COLUMN_TYPE_PARSED collision=COLUMN_TYPE_LABEL
                        └── Parse kind=PARSE_OP_LOGFMT
                            └── Filter predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_EQ,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:detected_level,Type:COLUMN_TYPE_AMBIGUOUS,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_StringLiteral{StringLiteral:&StringLiteral{Value:error,},},},},},},},}
                                └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                                    └── ScanSet num_targets=2 projections=(&ColumnExpression{Name:detected_level,Type:COLUMN_TYPE_AMBIGUOUS,}, &ColumnExpression{Name:message,Type:COLUMN_TYPE_BUILTIN,}, &ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,}) predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689540000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},}
                                            ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                                            └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
			`,
		},
		{
			comment: "math expression",
			query:   `sum by (bar) (count_over_time({app="foo"}[1m]) / 300)`,
			expected: `
AggregateVector operation=AGGREGATE_VECTOR_OP_SUM group_by=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,})
└── Projection all=true expand=(&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_DIV,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:value,Type:COLUMN_TYPE_GENERATED,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_FloatLiteral{FloatLiteral:&FloatLiteral{Value:300,},},},},},},},})
    └── AggregateRange operation=AGGREGATE_RANGE_OP_COUNT start=2025-01-01T00:00:00Z end=2025-01-01T01:00:00Z step=0s range=1m0s partition_by=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,})
        └── Parallelize
            └── ColumnCompat src=COLUMN_TYPE_METADATA dst=COLUMN_TYPE_METADATA collision=COLUMN_TYPE_LABEL
                └── ScanSet num_targets=2 projections=(&ColumnExpression{Name:bar,Type:COLUMN_TYPE_AMBIGUOUS,}, &ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,}) predicate[0]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_GTE,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735689540000000000,},},},},},},},} predicate[1]=&Expression{Kind:&Expression_BinaryExpression{BinaryExpression:&BinaryExpression{Op:BINARY_OP_LT,Left:&Expression{Kind:&Expression_ColumnExpression{ColumnExpression:&ColumnExpression{Name:timestamp,Type:COLUMN_TYPE_BUILTIN,},},},Right:&Expression{Kind:&Expression_LiteralExpression{LiteralExpression:&LiteralExpression{Kind:&LiteralExpression_TimestampLiteral{TimestampLiteral:&TimestampLiteral{Value:1735693200000000000,},},},},},},},}
                        ├── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=1 projections=() direction=SORT_ORDER_INVALID limit=0
                        └── @target type=SCAN_TYPE_DATA_OBJECT location=objects/00/0000000000.dataobj streams=5 section_id=0 projections=() direction=SORT_ORDER_INVALID limit=0
						`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.query, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			t.Cleanup(cancel)

			q := &TestQuery{
				statement: tc.query,
				start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
				end:       time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
				interval:  5 * time.Minute,
				limit:     1000,
				direction: logproto.BACKWARD,
			}

			fmt.Println(tc.query)
			logicalPlan, err := logical.BuildPlan(q)
			require.NoError(t, err)

			catalog := physical.NewMetastoreCatalog(ctx, metastore)
			planner := physical.NewPlanner(physical.NewContext(q.Start(), q.End()), catalog)

			plan, err := planner.Build(logicalPlan)
			//tmp := physical.PrintAsTree(plan)
			//fmt.Println(tmp)

			require.NoError(t, err)
			plan, err = planner.Optimize(plan)
			require.NoError(t, err)

			actual := physical.PrintAsTree(plan)
			if tc.comment == `metric: multiple parse stages` {
				t.Logf("first failing test case")
			}
			require.Equal(t, strings.TrimSpace(tc.expected), strings.TrimSpace(actual))
		})
	}
}
