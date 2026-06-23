package postings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSortLabelEntries_Order(t *testing.T) {
	tests := []struct {
		name     string
		entries  []LabelEntry
		expected []LabelEntry
	}{
		{
			name: "ColumnName dominates",
			entries: []LabelEntry{
				{ColumnName: "z", LabelValue: "x", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "a", LabelValue: "x", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "y", LabelValue: "x", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "c", LabelValue: "x", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "b", LabelValue: "x", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []LabelEntry{
				{ColumnName: "a", LabelValue: "x", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "b", LabelValue: "x", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "c", LabelValue: "x", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "y", LabelValue: "x", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "z", LabelValue: "x", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "LabelValue dominates within equal ColumnName",
			entries: []LabelEntry{
				{ColumnName: "env", LabelValue: "z", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "env", LabelValue: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "y", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "c", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "b", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []LabelEntry{
				{ColumnName: "env", LabelValue: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "b", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "env", LabelValue: "c", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "y", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "z", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "MinTimestamp dominates within equal ColumnName and LabelValue",
			entries: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 500, MaxTimestamp: 1, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 400, MaxTimestamp: 1, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 200, MaxTimestamp: 1, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 300, MaxTimestamp: 1, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 200, MaxTimestamp: 1, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 300, MaxTimestamp: 1, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 400, MaxTimestamp: 1, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 500, MaxTimestamp: 1, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "MaxTimestamp dominates within equal ColumnName, LabelValue, and MinTimestamp",
			entries: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 500, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 400, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 300, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 300, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 400, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 500, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "ObjectPath then SectionIndex break ties",
			entries: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 5},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/b", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 3},
			},
			expected: []LabelEntry{
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 3},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 5},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/b", SectionIndex: 2},
				{ColumnName: "env", LabelValue: "prod", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/z", SectionIndex: 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortLabelEntries(tt.entries)
			require.Equal(t, tt.expected, tt.entries)
		})
	}
}

func TestSortBloomEntries_Order(t *testing.T) {
	tests := []struct {
		name     string
		entries  []BloomEntry
		expected []BloomEntry
	}{
		{
			name: "ColumnName dominates",
			entries: []BloomEntry{
				{ColumnName: "z", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "y", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "c", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "b", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []BloomEntry{
				{ColumnName: "a", MinTimestamp: 1, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "b", MinTimestamp: 200, MaxTimestamp: 200, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "c", MinTimestamp: 300, MaxTimestamp: 300, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "y", MinTimestamp: 900, MaxTimestamp: 900, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "z", MinTimestamp: 1000, MaxTimestamp: 1000, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "MinTimestamp dominates within equal ColumnName",
			entries: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 500, MaxTimestamp: 1, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 400, MaxTimestamp: 1, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "svc", MinTimestamp: 200, MaxTimestamp: 1, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 300, MaxTimestamp: 1, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 1, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 200, MaxTimestamp: 1, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 300, MaxTimestamp: 1, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "svc", MinTimestamp: 400, MaxTimestamp: 1, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "svc", MinTimestamp: 500, MaxTimestamp: 1, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "MaxTimestamp dominates within equal ColumnName and MinTimestamp",
			entries: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 500, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 400, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 300, ObjectPath: "/b", SectionIndex: 1},
			},
			expected: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 200, ObjectPath: "/c", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 300, ObjectPath: "/b", SectionIndex: 1},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 400, ObjectPath: "/y", SectionIndex: 8},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 500, ObjectPath: "/z", SectionIndex: 9},
			},
		},
		{
			name: "ObjectPath then SectionIndex break ties",
			entries: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/z", SectionIndex: 9},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 5},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/b", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 3},
			},
			expected: []BloomEntry{
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 0},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 3},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/a", SectionIndex: 5},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/b", SectionIndex: 2},
				{ColumnName: "svc", MinTimestamp: 100, MaxTimestamp: 150, ObjectPath: "/z", SectionIndex: 9},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortBloomEntries(tt.entries)
			require.Equal(t, tt.expected, tt.entries)
		})
	}
}
