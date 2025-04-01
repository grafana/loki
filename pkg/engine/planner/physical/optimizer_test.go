package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestCanApplyPredicate(t *testing.T) {
	tests := []struct {
		predicate Expression
		want      bool
	}{
		{
			predicate: IntLiteral(123),
			want:      true,
		},
		{
			predicate: &ColumnExpr{
				Name:       "timestamp",
				ColumnType: types.ColumnTypeBuiltin,
			},
			want: true,
		},
		{
			predicate: &ColumnExpr{
				Name:       "foo",
				ColumnType: types.ColumnTypeLabel,
			},
			want: false,
		},
		{
			predicate: &BinaryExpr{
				Left: &ColumnExpr{
					Name:       "timestamp",
					ColumnType: types.ColumnTypeBuiltin,
				},
				Right: TimestampLiteral(1743424636000000000),
				Op:    types.BinaryOpGt,
			},
			want: true,
		},
		{
			predicate: &BinaryExpr{
				Left: &ColumnExpr{
					Name:       "foo",
					ColumnType: types.ColumnTypeLabel,
				},
				Right: StringLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.predicate.String(), func(t *testing.T) {
			got := canApplyPredicate(tt.predicate)
			require.Equal(t, tt.want, got)
		})
	}
}
