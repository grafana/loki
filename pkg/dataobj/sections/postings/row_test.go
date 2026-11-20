package postings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompareRows(t *testing.T) {
	tests := []struct {
		name string
		a, b Row
		want int
	}{
		{
			name: "Kind dominates: bloom before label",
			a:    Row{Kind: KindBloom, ColumnName: "z", LabelValue: "z", MinTimestamp: 9, MaxTimestamp: 9, ObjectPath: "/z", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "a", LabelValue: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
			want: -1,
		},
		{
			name: "ColumnName dominates within equal Kind",
			a:    Row{Kind: KindLabel, ColumnName: "a", LabelValue: "z", MinTimestamp: 9, MaxTimestamp: 9, ObjectPath: "/z", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "b", LabelValue: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
			want: -1,
		},
		{
			name: "LabelValue dominates within equal Kind and ColumnName",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "a", MinTimestamp: 9, MaxTimestamp: 9, ObjectPath: "/z", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "b", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
			want: -1,
		},
		{
			name: "MinTimestamp dominates within equal Kind, ColumnName, LabelValue",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 9, ObjectPath: "/z", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 200, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
			want: -1,
		},
		{
			name: "MaxTimestamp dominates within equal Kind, ColumnName, LabelValue, MinTimestamp",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/z", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 400, ObjectPath: "/a", SectionIndex: 0},
			want: -1,
		},
		{
			name: "ObjectPath dominates SectionIndex",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 9},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/b", SectionIndex: 0},
			want: -1,
		},
		{
			name: "SectionIndex breaks final ties",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 3},
			want: -1,
		},
		{
			name: "fully equal rows compare equal",
			a:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
			b:    Row{Kind: KindLabel, ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, CompareRows(tt.a, tt.b))
			require.Equal(t, -tt.want, CompareRows(tt.b, tt.a), "comparison must be antisymmetric")
		})
	}
}

func TestRowSize(t *testing.T) {
	t.Run("counts the strings, bitmap and bloom filter of a bloom row", func(t *testing.T) {
		row := Row{
			Kind:             KindBloom,
			ObjectPath:       "obj",
			ColumnName:       "col",
			StreamIDBitmap:   []byte{1, 2},
			BloomFilter:      []byte{1, 2, 3, 4},
			UncompressedSize: 1000,
		}
		require.Equal(t, int64(3+3+2+4), row.Size())
	})

	t.Run("counts the label value of a label row", func(t *testing.T) {
		row := Row{Kind: KindLabel, ObjectPath: "obj", ColumnName: "service", LabelValue: "api", StreamIDBitmap: []byte{1}}
		require.Equal(t, int64(3+7+3+1), row.Size())
	})
}
