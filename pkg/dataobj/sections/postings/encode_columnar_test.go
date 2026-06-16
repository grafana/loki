package postings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test encodeInSections with label entries.
func TestEncodeInSectionsLabelEntries(t *testing.T) {
	tests := []struct {
		name     string
		entries  []LabelEntry
		target   int
		expected int // number of sections
	}{
		{
			name:     "empty entries",
			entries:  []LabelEntry{},
			target:   1000,
			expected: 0,
		},
		{
			name: "single entry under target",
			entries: []LabelEntry{
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "path1", StreamIDBitmap: []byte("x")},
			},
			target:   1000,
			expected: 1,
		},
		{
			name: "all entries under target",
			entries: []LabelEntry{
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "path1", StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "path2", StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "path3", StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   1000,
			expected: 1,
		},
		{
			name: "single large entry becomes its own section",
			entries: []LabelEntry{
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: longString(1000), StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   50, // smaller than one entry
			expected: 1,  // can't split below one entry
		},
		{
			name: "multiple entries split on size",
			entries: []LabelEntry{
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "x" + longString(100), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "y" + longString(100), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "z" + longString(100), StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   150, // enough for ~1 entry
			expected: 3,   // each entry becomes its own section
		},
		{
			name: "splits when size reached regardless of label value",
			entries: []LabelEntry{
				// First entry: col1/val1 (small, ~40 bytes)
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "path1", StreamIDBitmap: []byte("x")},
				// Second entry: col1/val1 same label (huge, exceeds target)
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "p" + longString(300), StreamIDBitmap: []byte("xxxxxxxx")},
				// Third entry: col1/val1 same label again
				{ColumnName: "col1", LabelValue: "val1", ObjectPath: "q" + longString(300), StreamIDBitmap: []byte("xxxxxxxx")},
				// Fourth entry: different label
				{ColumnName: "col2", LabelValue: "val2", ObjectPath: "r1", StreamIDBitmap: []byte("x")},
			},
			target:   200,
			expected: 3, // first entry alone, second alone, third+fourth together
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := 0
			totalEntries := 0
			err := encodeInSections(tt.entries, tt.target, labelEntrySize, func(sec []LabelEntry) error {
				require.Greater(t, len(sec), 0, "section must not be empty")
				sections++
				totalEntries += len(sec)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, tt.expected, sections, "expected %d sections but got %d", tt.expected, sections)
			require.Equal(t, len(tt.entries), totalEntries, "all entries must be preserved")
		})
	}
}

// Test encodeInSections with bloom entries.
func TestEncodeInSectionsBloomEntries(t *testing.T) {
	tests := []struct {
		name     string
		entries  []BloomEntry
		target   int
		expected int // number of sections
	}{
		{
			name:     "empty entries",
			entries:  []BloomEntry{},
			target:   1000,
			expected: 0,
		},
		{
			name: "single entry under target",
			entries: []BloomEntry{
				{ColumnName: "col1", ObjectPath: "path1", BloomFilter: []byte("x"), StreamIDBitmap: []byte("x")},
			},
			target:   1000,
			expected: 1,
		},
		{
			name: "all entries under target",
			entries: []BloomEntry{
				{ColumnName: "col1", ObjectPath: "path1", BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", ObjectPath: "path2", BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", ObjectPath: "path3", BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   1000,
			expected: 1,
		},
		{
			name: "single large entry becomes its own section",
			entries: []BloomEntry{
				{ColumnName: "col1", ObjectPath: longString(1000), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   50, // smaller than one entry
			expected: 1,  // can't split below one entry
		},
		{
			name: "multiple entries split on size",
			entries: []BloomEntry{
				{ColumnName: "col1", ObjectPath: "x" + longString(100), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", ObjectPath: "y" + longString(100), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				{ColumnName: "col1", ObjectPath: "z" + longString(100), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
			},
			target:   150, // enough for ~1 entry
			expected: 3,   // each entry becomes its own section
		},
		{
			name: "splits when size reached regardless of column",
			entries: []BloomEntry{
				// First entry: col1 (small)
				{ColumnName: "col1", ObjectPath: "path1", BloomFilter: []byte("x"), StreamIDBitmap: []byte("x")},
				// Second entry: col1 same column (huge)
				{ColumnName: "col1", ObjectPath: "p" + longString(300), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				// Third entry: col1 same column again
				{ColumnName: "col1", ObjectPath: "q" + longString(300), BloomFilter: []byte("xxxxxxxx"), StreamIDBitmap: []byte("xxxxxxxx")},
				// Fourth entry: different column
				{ColumnName: "col2", ObjectPath: "r1", BloomFilter: []byte("x"), StreamIDBitmap: []byte("x")},
			},
			target:   200,
			expected: 3, // first entry alone, second alone, third+fourth together
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections := 0
			totalEntries := 0
			err := encodeInSections(tt.entries, tt.target, bloomEntrySize, func(sec []BloomEntry) error {
				require.Greater(t, len(sec), 0, "section must not be empty")
				sections++
				totalEntries += len(sec)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, tt.expected, sections, "expected %d sections but got %d", tt.expected, sections)
			require.Equal(t, len(tt.entries), totalEntries, "all entries must be preserved")
		})
	}
}

// longString creates a string of length n.
func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
