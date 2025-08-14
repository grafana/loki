package physical

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestCatalog_ConvertLiteral(t *testing.T) {
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
			expr:    NewLiteral(datatype.Timestamp(time.Now().UnixNano())),
			wantErr: true,
		},
		{
			expr:    NewLiteral(datatype.Duration(time.Hour.Nanoseconds())),
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
			got, err := convertLiteralToString(tt.expr)
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

func TestCatalog_ConvertColumnRef(t *testing.T) {
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
			got, err := convertColumnRef(tt.expr, false)
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

func TestCatalog_ExpressionToMatchers(t *testing.T) {
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
			got, err := expressionToMatchers(tt.expr, false)
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

func TestCatalog_TimeRangeOverlaps(t *testing.T) {
	tests := []struct {
		name        string
		firstRange  TimeRange
		secondRange TimeRange
		want        bool
	}{
		{name: "Second contained in first",
			firstRange:  TimeRange{Start: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)},
			secondRange: TimeRange{Start: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 2, 0, 0, 0, time.UTC)},
			want:        true,
		},
		{name: "Second completely after first",
			firstRange:  TimeRange{Start: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)},
			secondRange: TimeRange{Start: time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 14, 0, 0, 0, time.UTC)},
			want:        false,
		},
		{name: "Second starts in first",
			firstRange:  TimeRange{Start: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)},
			secondRange: TimeRange{Start: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC)},
			want:        true,
		},
		{name: "Second ends in first",
			firstRange:  TimeRange{Start: time.Date(2025, time.January, 1, 6, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC)},
			secondRange: TimeRange{Start: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC)},
			want:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got1 := tt.firstRange.Overlaps(tt.secondRange)
			got2 := tt.secondRange.Overlaps(tt.firstRange)
			require.Equal(t, tt.want, got1, got2)
		})
	}
}
