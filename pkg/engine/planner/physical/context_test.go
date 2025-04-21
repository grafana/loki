package physical

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestContext_ConvertLiteral(t *testing.T) {
	tests := []struct {
		expr    Expression
		want    string
		wantErr bool
	}{
		{
			expr: NewLiteral("foo"),
			want: "foo",
		},
		{
			expr:    NewLiteral(false),
			wantErr: true,
		},
		{
			expr:    NewLiteral(int64(123)),
			wantErr: true,
		},
		{
			expr:    NewLiteral(uint64(123456789)),
			wantErr: true,
		},
		{
			expr:    newColumnExpr("foo", types.ColumnTypeLabel),
			wantErr: true,
		},
		{
			expr: &BinaryExpr{
				Left:  newColumnExpr("foo", types.ColumnTypeLabel),
				Right: NewLiteral("foo"),
				Op:    types.BinaryOpEq,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.expr.String(), func(t *testing.T) {
			got, err := convertLiteral(tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				t.Log(err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestContext_ConvertColumnRef(t *testing.T) {
	tests := []struct {
		expr    Expression
		want    string
		wantErr bool
	}{
		{
			expr: newColumnExpr("foo", types.ColumnTypeLabel),
			want: "foo",
		},
		{
			expr:    newColumnExpr("foo", types.ColumnTypeAmbiguous),
			wantErr: true,
		},
		{
			expr:    newColumnExpr("foo", types.ColumnTypeBuiltin),
			wantErr: true,
		},
		{
			expr:    NewLiteral(false),
			wantErr: true,
		},
		{
			expr: &BinaryExpr{
				Left:  newColumnExpr("foo", types.ColumnTypeLabel),
				Right: NewLiteral("foo"),
				Op:    types.BinaryOpEq,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.expr.String(), func(t *testing.T) {
			got, err := convertColumnRef(tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				t.Log(err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestContext_ExpressionToMatchers(t *testing.T) {
	tests := []struct {
		expr    Expression
		want    []*labels.Matcher
		wantErr bool
	}{
		{
			expr:    newColumnExpr("foo", types.ColumnTypeLabel),
			wantErr: true,
		},
		{
			expr:    NewLiteral("foo"),
			wantErr: true,
		},
		{
			expr: &BinaryExpr{
				Left:  newColumnExpr("foo", types.ColumnTypeLabel),
				Right: NewLiteral("bar"),
				Op:    types.BinaryOpEq,
			},
			want: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
			},
		},
		{
			expr: &BinaryExpr{
				Left: &BinaryExpr{
					Left:  newColumnExpr("foo", types.ColumnTypeLabel),
					Right: NewLiteral("bar"),
					Op:    types.BinaryOpEq,
				},
				Right: &BinaryExpr{
					Left:  newColumnExpr("bar", types.ColumnTypeLabel),
					Right: NewLiteral("baz"),
					Op:    types.BinaryOpNeq,
				},
				Op: types.BinaryOpAnd,
			},
			want: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				labels.MustNewMatcher(labels.MatchNotEqual, "bar", "baz"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.expr.String(), func(t *testing.T) {
			got, err := expressionToMatchers(tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				t.Log(err)
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, tt.want, got)
			}
		})
	}
}
