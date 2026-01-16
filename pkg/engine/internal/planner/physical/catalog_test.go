package physical

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
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
			expr:    NewLiteral(types.Timestamp(time.Now().UnixNano())),
			wantErr: true,
		},
		{
			expr:    NewLiteral(types.Duration(time.Hour.Nanoseconds())),
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

func TestCatalog_TimeRangeValidate(t *testing.T) {
	tests := []struct {
		name      string
		start     time.Time
		end       time.Time
		expectErr bool
	}{
		{name: "Normal time range",
			start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			expectErr: false,
		},
		{name: "Zero-width time range",
			start:     time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			expectErr: false,
		},
		{name: "Invalid time range",
			start:     time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			end:       time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			expectErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTimeRange(tt.start, tt.end)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCatalog_TimeRangeOverlaps(t *testing.T) {
	tests := []struct {
		name        string
		firstStart  time.Time
		firstEnd    time.Time
		secondStart time.Time
		secondEnd   time.Time
		want        bool
	}{
		{name: "Second contained in first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 2, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "Second completely after first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 14, 0, 0, 0, time.UTC),
			want:        false,
		},
		{name: "Second starts in first",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 1, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 13, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "Second ends in first",
			firstStart:  time.Date(2025, time.January, 1, 6, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 9, 0, 0, 0, time.UTC),
			want:        true,
		},
		{name: "First end = second start",
			firstStart:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			firstEnd:    time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondStart: time.Date(2025, time.January, 1, 12, 0, 0, 0, time.UTC),
			secondEnd:   time.Date(2025, time.January, 1, 20, 0, 0, 0, time.UTC),
			want:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			firstRange, err := newTimeRange(tt.firstStart, tt.firstEnd)
			require.NoError(t, err)
			secondRange, err := newTimeRange(tt.secondStart, tt.secondEnd)
			require.NoError(t, err)
			got1 := firstRange.Overlaps(secondRange)
			got2 := secondRange.Overlaps(firstRange)
			require.Equal(t, tt.want, got1, got2)
		})
	}
}

func TestCatalog_FilterDescriptorsForShard(t *testing.T) {
	t.Run("", func(t *testing.T) {
		now := time.Now()
		start1 := now.Add(time.Second * -10)
		end1 := now.Add(time.Second * -5)
		start2 := now.Add(time.Second * -30)
		end2 := now.Add(time.Second * -20)
		start3 := now.Add(time.Second * -20)
		end3 := now.Add(time.Second * -10)
		shard := ShardInfo{Shard: 1, Of: 2}
		desc1 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{1, 2}, RowCount: 10, Size: 10, Start: start1, End: end1}
		desc1.ObjectPath = "foo"
		desc1.SectionIdx = 1
		desc2 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{3, 4}, RowCount: 10, Size: 10, Start: start2, End: end2}
		desc2.ObjectPath = "bar"
		desc2.SectionIdx = 2
		desc3 := metastore.DataobjSectionDescriptor{StreamIDs: []int64{1, 5}, RowCount: 10, Size: 10, Start: start3, End: end3}
		desc3.ObjectPath = "baz"
		desc3.SectionIdx = 3
		sectionDescriptors := []*metastore.DataobjSectionDescriptor{&desc1, &desc2, &desc3}
		res, err := filterForShard(shard, sectionDescriptors)
		require.NoError(t, err)
		tr1, err := newTimeRange(start1, end1)
		require.NoError(t, err)
		tr3, err := newTimeRange(start3, end3)
		require.NoError(t, err)
		expected := []DataObjSections{
			{Location: "foo", Streams: []int64{1, 2}, Sections: []int{1}, TimeRange: tr1},
			{Location: "baz", Streams: []int64{1, 5}, Sections: []int{3}, TimeRange: tr3},
		}
		require.ElementsMatch(t, res, expected)
	})

}
