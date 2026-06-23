package executor

import (
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_logsPredicateIsUnsatisfiable(t *testing.T) {
	timestampColumn := &logs.Column{Type: logs.ColumnTypeTimestamp}
	columns := []*logs.Column{timestampColumn}

	tests := []struct {
		name string
		p    logs.Predicate
		want bool
	}{
		{name: "false", p: logs.FalsePredicate{}, want: true},
		{name: "true", p: logs.TruePredicate{}, want: false},
		{
			name: "AND with false child",
			p: logs.AndPredicate{
				Left:  logs.TruePredicate{},
				Right: logs.FalsePredicate{},
			},
			want: true,
		},
		{
			name: "OR with one satisfiable child",
			p: logs.OrPredicate{
				Left:  logs.FalsePredicate{},
				Right: logs.TruePredicate{},
			},
			want: false,
		},
		{
			name: "OR with both unsatisfiable",
			p: logs.OrPredicate{
				Left:  logs.FalsePredicate{},
				Right: logs.FalsePredicate{},
			},
			want: true,
		},
		{
			name: "NOT false",
			p:    logs.NotPredicate{Inner: logs.FalsePredicate{}},
			want: false,
		},
		{
			name: "NOT true",
			p:    logs.NotPredicate{Inner: logs.TruePredicate{}},
			want: true,
		},
		{
			name: "equality can match",
			p: logs.EqualPredicate{
				Column: timestampColumn,
				Value:  scalar.NewInt64Scalar(1),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, logsPredicateIsUnsatisfiable(tt.p))
		})
	}

	t.Run("missing column becomes false", func(t *testing.T) {
		expr := &physical.BinaryExpr{
			Op:    types.BinaryOpEq,
			Left:  columnRef(types.ColumnTypeMetadata, "missing"),
			Right: physical.NewLiteral("value"),
		}
		p, err := buildLogsPredicate(expr, columns)
		require.NoError(t, err)
		require.True(t, logsPredicateIsUnsatisfiable(p))
	})
}

func Test_logsPredicatesAreUnsatisfiable(t *testing.T) {
	require.False(t, logsPredicatesAreUnsatisfiable(nil))
	require.False(t, logsPredicatesAreUnsatisfiable([]logs.Predicate{logs.TruePredicate{}}))
	require.True(t, logsPredicatesAreUnsatisfiable([]logs.Predicate{
		logs.TruePredicate{},
		logs.FalsePredicate{},
	}))
}

func Test_executeDataObjScan_unsatisfiablePredicate(t *testing.T) {
	obj := buildDataobj(t, []logproto.Stream{
		{
			Labels: `{service="loki"}`,
			Entries: []logproto.Entry{
				{Timestamp: mustTimeUnix(5), Line: "hello"},
			},
		},
	})

	bucket := objstore.NewInMemBucket()
	const objectPath = "objects/test"
	reader, err := obj.Reader(context.Background())
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(context.Background(), objectPath, reader))
	require.NoError(t, reader.Close())

	ctx := user.InjectOrgID(t.Context(), "tenant")
	c := &Context{
		bucket:    bucket,
		batchSize: 512,
		logger:    log.NewNopLogger(),
	}

	pipeline := c.executeDataObjScan(ctx, &physical.DataObjScan{
		Location:  objectPath,
		Section:   0,
		StreamIDs: []int64{1},
		Predicates: []physical.Expression{
			physical.NewLiteral(false),
		},
	})

	_, err = pipeline.Read(ctx)
	require.ErrorIs(t, err, EOF)
}

func mustTimeUnix(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
