package bench

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"
)

func TestParquetQuerier(t *testing.T) {
	bkt, err := filesystem.NewBucket("data/parquet")
	require.NoError(t, err)

	q := NewParquetQuerier(bkt, log.NewLogfmtLogger(os.Stdout))

	query := `{env="prod"} | level="warn"`
	iterator, err := q.SelectLogs(context.Background(), logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  query,
			Limit:     uint32(2),
			Start:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:       time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			Direction: logproto.BACKWARD,
			Plan: &plan.QueryPlan{
				AST: syntax.MustParseExpr(query),
			},
		},
	})
	require.NoError(t, err)

	for iterator.Next() {
		entry := iterator.At()
		fmt.Println(entry.Timestamp, entry.Line)
	}
	require.NoError(t, iterator.Err())
}

func TestParquetQuerierSamples(t *testing.T) {
	bkt, err := filesystem.NewBucket("data/parquet")
	require.NoError(t, err)

	q := NewParquetQuerier(bkt, log.NewLogfmtLogger(os.Stdout))

	query := `sum by (namespace) (rate({env="prod"} | detected_level="warn" [5m]))`
	iterator, err := q.SelectSamples(context.Background(), logql.SelectSampleParams{
		SampleQueryRequest: &logproto.SampleQueryRequest{
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			Plan: &plan.QueryPlan{
				AST: syntax.MustParseExpr(query),
			},
		},
	})
	require.NoError(t, err)

	for iterator.Next() {
		entry := iterator.At()
		fmt.Println(entry.Timestamp, entry.Value)
	}
	require.NoError(t, iterator.Err())
}
