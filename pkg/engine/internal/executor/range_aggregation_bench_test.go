package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// BenchmarkRangeAggregationPipeline measures pipeline.Read for each window strategy.
// The pipeline and input batches are built once per subbenchmark; each iteration only
// resets cursors and aggregator state so Read can run again.
func BenchmarkRangeAggregationPipeline(b *testing.B) {
	old := assertions.Enabled
	assertions.Enabled = false
	b.Cleanup(func() { assertions.Enabled = old })

	groupBy := buildRangeAggregationGrouping()
	schema, rows := buildRangeAggregationInput()
	inputRecords := buildInputRecords(b, schema, rows)
	b.Cleanup(func() {
		for _, rec := range inputRecords {
			rec.Release()
		}
	})

	cases := []struct {
		name string
		opts rangeAggregationOptions
	}{
		{
			name: "case=instant",
			opts: rangeAggregationOptions{
				grouping:      groupBy,
				startTs:       time.Unix(1000, 0),
				endTs:         time.Unix(1000, 0),
				rangeInterval: 1000 * time.Second,
				step:          0,
				operation:     types.RangeAggregationTypeCount,
			},
		},
		{
			name: "case=aligned",
			opts: rangeAggregationOptions{
				grouping:      groupBy,
				startTs:       time.Unix(10, 0),
				endTs:         time.Unix(40, 0),
				rangeInterval: 10 * time.Second,
				step:          10 * time.Second,
				operation:     types.RangeAggregationTypeCount,
			},
		},
		{
			name: "case=gapped",
			opts: rangeAggregationOptions{
				grouping:      groupBy,
				startTs:       time.Unix(10, 0),
				endTs:         time.Unix(40, 0),
				rangeInterval: 5 * time.Second,
				step:          10 * time.Second,
				operation:     types.RangeAggregationTypeCount,
			},
		},
		{
			name: "case=overlapping",
			opts: rangeAggregationOptions{
				grouping:      groupBy,
				startTs:       time.Unix(10, 0),
				endTs:         time.Unix(40, 0),
				rangeInterval: 5 * time.Minute,
				step:          10 * time.Second,
				operation:     types.RangeAggregationTypeCount,
			},
		},
	}

	ctx := context.Background()
	evaluator := newExpressionEvaluator()

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			input := NewBufferedPipeline(inputRecords...)
			pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, evaluator, tc.opts)
			if err != nil {
				b.Fatal(err)
			}
			if err := pipeline.Open(ctx); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Read all the records each iteration
				for {
					rec, err := pipeline.Read(ctx)
					if err != nil {
						if errors.Is(err, EOF) {
							resetRangeAggregationPipeline(pipeline, input)
							break
						}
						b.Fatal(err)
					}
					if rec != nil {
						rec.Release()
					}
				}
			}
		})
	}
}

// resetRangeAggregationPipeline rewinds a range aggregation pipeline so Read can be
// invoked again with the same inputs. rangeAggregationPipeline is single-shot by
// default (inputsExhausted); this is intended for benchmarks and tests only.
func resetRangeAggregationPipeline(p *rangeAggregationPipeline, input *BufferedPipeline) {
	p.inputsExhausted = false
	p.aggregator.Reset()
	input.Reset()
}

func buildInputRecords(b *testing.B, schema *arrow.Schema, rows []arrowtest.Rows) []arrow.RecordBatch {
	b.Helper()

	records := make([]arrow.RecordBatch, len(rows))
	for i, r := range rows {
		records[i] = r.Record(memory.DefaultAllocator, schema)
	}
	return records
}

func buildRangeAggregationGrouping() physical.Grouping {
	return physical.Grouping{
		Columns: []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "env",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "service",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
		},
		Without: false,
	}
}

// benchmarkRangeAggregationWindows builds query windows the same way as rangeAggregationPipeline.init.
func benchmarkRangeAggregationWindows(opts rangeAggregationOptions) []window {
	windows := []window{}
	cur := opts.startTs
	endUnix := opts.endTs.UnixNano()
	for cur.UnixNano() <= endUnix {
		windows = append(windows, window{start: cur.Add(-opts.rangeInterval), end: cur})
		if opts.step == 0 {
			break
		}
		cur = cur.Add(opts.step)
	}
	return windows
}

func buildRangeAggregationInput() (*arrow.Schema, []arrowtest.Rows) {
	fields := []arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colEnv, false),
		semconv.FieldFromFQN(colSvc, false),
	}
	schema := arrow.NewSchema(fields, nil)

	const (
		rowsPerBatch = 1024
		batches      = 8
	)

	rows := make([]arrowtest.Rows, batches)
	base := time.Unix(12, 0).UTC()
	for batch := range batches {
		batchRows := make(arrowtest.Rows, rowsPerBatch)
		for i := range rowsPerBatch {
			offset := batch*rowsPerBatch + i
			batchRows[i] = arrowtest.Row{
				colTs:  base.Add(time.Duration(offset%28) * time.Second),
				colEnv: []string{"prod", "dev", "staging"}[offset%3],
				colSvc: []string{"app1", "app2", "app3", "app4"}[offset%4],
			}
		}
		rows[batch] = batchRows
	}

	return schema, rows
}
