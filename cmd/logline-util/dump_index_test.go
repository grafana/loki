package main

import (
	"bytes"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

// createTestLintFile creates a test .lidx file with known contents.
func createTestLintFile(t *testing.T, path string) {
	baseTime := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC).UnixMilli()
	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: baseTime, MaxTimeUnix: baseTime + 300*1000},
		{ID: 1, MinTimeUnix: baseTime + 300*1000, MaxTimeUnix: baseTime + 600*1000},
		{ID: 2, MinTimeUnix: baseTime + 600*1000, MaxTimeUnix: baseTime + 900*1000},
	}
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	terms := []struct {
		term   [8]byte
		docIDs []uint32
	}{
		{[8]byte{'E', 'R', 'R', 'O', 'R', 'L', 'O', 'G'}, []uint32{0, 2}},
		{[8]byte{'I', 'N', 'F', 'O', 'L', 'O', 'G', ' '}, []uint32{0, 1, 2}},
		{[8]byte{'W', 'A', 'R', 'N', 'I', 'N', 'G', ' '}, []uint32{1}},
		{[8]byte{'D', 'E', 'B', 'U', 'G', 'M', 'S', 'G'}, []uint32{0, 1}},
		{[8]byte{'T', 'R', 'A', 'C', 'E', 'L', 'O', 'G'}, []uint32{2}},
	}

	// StreamingIndexWriter requires terms in ascending lexicographic order.
	sort.Slice(terms, func(i, j int) bool {
		return bytes.Compare(terms[i].term[:], terms[j].term[:]) < 0
	})
	for _, term := range terms {
		bm := roaring.New()
		bm.AddMany(term.docIDs)
		require.NoError(t, writer.WriteTermBitmap(term.term, format.Bitmap{Roaring: bm}))
	}

	require.NoError(t, writer.Close())
}

func TestPrintHeader(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.lidx")
	createTestLintFile(t, testFile)

	reader, version, err := logline.OpenFile(testFile)
	require.NoError(t, err)
	defer reader.Close()

	var buf bytes.Buffer
	printHeaderToWriter(&buf, reader, version, testFile)

	output := buf.String()
	assert.Contains(t, output, "File: "+testFile)
	assert.Contains(t, output, "Version: "+version)
	assert.Contains(t, output, "Term Count: 5")
	assert.Contains(t, output, "Document Count: 3")
	assert.Contains(t, output, "Posting Count: 9") // 2+3+1+2+1 = 9
}

func TestPrintTerms(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.lidx")
	createTestLintFile(t, testFile)

	reader, _, err := logline.OpenFile(testFile)
	require.NoError(t, err)
	defer reader.Close()

	tests := []struct {
		name        string
		showBitmaps bool
		limit       int
		checks      []string
	}{
		{
			name:        "summary only",
			showBitmaps: false,
			limit:       0,
			checks: []string{
				"Terms (5 total)",
				"DEBUGM",
				"ERRORL",
				"docs",
			},
		},
		{
			name:        "with bitmaps",
			showBitmaps: true,
			limit:       0,
			checks: []string{
				"Terms (5 total)",
				"ERRORL",
				"[0, 2]",
			},
		},
		{
			name:        "with limit",
			showBitmaps: false,
			limit:       2,
			checks: []string{
				"showing first 2",
				"and 3 more terms",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			it, err := reader.NewTermIterator()
			require.NoError(t, err)

			printTermsToWriter(&buf, it, tt.showBitmaps, tt.limit)

			output := buf.String()
			for _, check := range tt.checks {
				assert.Contains(t, output, check, "output should contain: %s", check)
			}
		})
	}
}

func TestPrintDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.lidx")
	createTestLintFile(t, testFile)

	reader, _, err := logline.OpenFile(testFile)
	require.NoError(t, err)
	defer reader.Close()

	tests := []struct {
		name   string
		limit  int
		checks []string
	}{
		{
			name:  "all documents",
			limit: 0,
			checks: []string{
				"Documents (3 total)",
				"Doc 0:",
				"Doc 1:",
				"Doc 2:",
				"2024-02-01",
			},
		},
		{
			name:  "with limit",
			limit: 2,
			checks: []string{
				"showing first 2",
				"Doc 0:",
				"Doc 1:",
				"and 1 more document",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printDocumentsToWriter(&buf, reader, tt.limit)

			output := buf.String()
			for _, check := range tt.checks {
				assert.Contains(t, output, check, "output should contain: %s", check)
			}
		})
	}
}

func TestDumpLintErrors(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nonexistent file",
			filePath:    "/tmp/nonexistent.lidx",
			expectError: true,
			errorMsg:    "unsupported format",
		},
		{
			name:        "invalid file",
			filePath:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := logline.OpenFile(tt.filePath)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
