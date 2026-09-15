package main

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 500, "500 B"},
		{"kilobytes", 1024, "1.0 KB"},
		{"kilobytes with decimal", 1536, "1.5 KB"},
		{"megabytes", 1048576, "1.0 MB"},
		{"megabytes with decimal", 8388608, "8.0 MB"},
		{"gigabytes", 1073741824, "1.0 GB"},
		{"large value", 10737418240, "10.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTerm(t *testing.T) {
	tests := []struct {
		name     string
		term     [8]byte
		expected string
	}{
		{
			name:     "printable ascii",
			term:     [8]byte{'E', 'R', 'R', 'O', 'R', 'L', 'O', 'G'},
			expected: "ERRORLOG",
		},
		{
			name:     "with null bytes",
			term:     [8]byte{'E', 'R', 'R', 0, 0, 0, 0, 0},
			expected: "ERR",
		},
		{
			name:     "with non-printable",
			term:     [8]byte{'A', 'B', 0x01, 0x02, 0x03, 'C', 'D', 'E'},
			expected: "AB\\x01\\x02\\x03CDE",
		},
		{
			name:     "all non-printable",
			term:     [8]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			expected: "\\x00\\x01\\x02\\x03\\x04\\x05\\x06\\x07",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTerm(tt.term)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name     string
		unix     int64 // milliseconds since Unix epoch
		expected string
	}{
		{"epoch", 0, "1970-01-01T00:00:00.000Z"},
		{"recent", 1707318000000, "2024-02-07T15:00:00.000Z"}, // 2024-02-07 15:00:00 in milliseconds
		{"negative", -3600000, "1969-12-31T23:00:00.000Z"},    // -1 hour in milliseconds
		{"with milliseconds", 1707318000123, "2024-02-07T15:00:00.123Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTime(tt.unix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatBitmap(t *testing.T) {
	tests := []struct {
		name     string
		docIDs   []uint32
		showAll  bool
		expected string
	}{
		{
			name:     "empty bitmap summary",
			docIDs:   []uint32{},
			showAll:  false,
			expected: "0 docs",
		},
		{
			name:     "empty bitmap full",
			docIDs:   []uint32{},
			showAll:  true,
			expected: "[]",
		},
		{
			name:     "single doc summary",
			docIDs:   []uint32{5},
			showAll:  false,
			expected: "1 doc",
		},
		{
			name:     "single doc full",
			docIDs:   []uint32{5},
			showAll:  true,
			expected: "[5]",
		},
		{
			name:     "multiple docs summary",
			docIDs:   []uint32{1, 3, 5, 7, 9},
			showAll:  false,
			expected: "5 docs",
		},
		{
			name:     "multiple docs full",
			docIDs:   []uint32{1, 3, 5, 7, 9},
			showAll:  true,
			expected: "[1, 3, 5, 7, 9]",
		},
		{
			name:     "many docs summary",
			docIDs:   []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			showAll:  false,
			expected: "10 docs",
		},
		{
			name:     "many docs full",
			docIDs:   []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			showAll:  true,
			expected: "[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := roaring.New()
			bm.AddMany(tt.docIDs)
			result := formatBitmap(bm, tt.showAll)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		name     string
		number   uint64
		expected string
	}{
		{"zero", 0, "0"},
		{"single digit", 5, "5"},
		{"two digits", 42, "42"},
		{"three digits", 999, "999"},
		{"thousands", 1000, "1,000"},
		{"thousands with comma", 1234, "1,234"},
		{"millions", 1000000, "1,000,000"},
		{"large number", 125432, "125,432"},
		{"very large", 3421890, "3,421,890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNumber(tt.number)
			assert.Equal(t, tt.expected, result)
		})
	}
}
