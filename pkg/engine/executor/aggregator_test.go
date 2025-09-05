package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestAggregator(t *testing.T) {
	t.Run("basic aggregation with record building", func(t *testing.T) {
		alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer alloc.AssertSize(t, 0)

		groupBy := []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "env",
					Type:   types.ColumnTypeLabel,
				},
			},
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "service",
					Type:   types.ColumnTypeLabel,
				},
			},
		}

		agg := newAggregator(groupBy, 10)

		ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)

		// Add test data
		// ts1: prod/app1 = 10, prod/app2 = 20, dev/app1 = 30
		agg.Add(ts1, 10, []string{"prod", "app1"})
		agg.Add(ts1, 20, []string{"prod", "app2"})
		agg.Add(ts1, 30, []string{"dev", "app1"})

		// ts2: prod/app1 = 15, prod/app2 = 25, dev/app2 = 35
		agg.Add(ts2, 15, []string{"prod", "app1"})
		agg.Add(ts2, 25, []string{"prod", "app2"})
		agg.Add(ts2, 35, []string{"dev", "app2"})

		// Add more data to same groups to test aggregation
		agg.Add(ts1, 5, []string{"prod", "app1"})  // prod/app1 at ts1 should now be 15
		agg.Add(ts2, 10, []string{"prod", "app1"}) // prod/app1 at ts2 should now be 25

		record, err := agg.BuildRecord()
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			// ts1: prod/app1=15, prod/app2=20, dev/app1=30
			{"timestamp": ts1, "value": int64(15), "env": "prod", "service": "app1"},
			{"timestamp": ts1, "value": int64(20), "env": "prod", "service": "app2"},
			{"timestamp": ts1, "value": int64(30), "env": "dev", "service": "app1"},
			// ts2: prod/app1=25, prod/app2=25, dev/app2=35
			{"timestamp": ts2, "value": int64(25), "env": "prod", "service": "app1"},
			{"timestamp": ts2, "value": int64(25), "env": "prod", "service": "app2"},
			{"timestamp": ts2, "value": int64(35), "env": "dev", "service": "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})
}
