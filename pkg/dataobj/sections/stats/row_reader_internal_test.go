package stats

import (
	"context"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// scriptedReader is a batchReader that returns a fixed sequence of batches, then
// io.EOF. It lets us exercise batch-boundary handling that the real reader can't
// currently produce (empty, non-EOF batches only arise from predicate filtering,
// which the stats reader does not use today).
type scriptedReader struct {
	batches []arrow.RecordBatch
	i       int
}

func (s *scriptedReader) Open(context.Context) error { return nil }
func (s *scriptedReader) Close() error               { return nil }

func (s *scriptedReader) Read(context.Context, int) (arrow.RecordBatch, error) {
	if s.i >= len(s.batches) {
		return nil, io.EOF
	}
	b := s.batches[s.i]
	s.i++
	return b, nil
}

// pathRows builds stats rows carrying one object_path per argument (zero args =>
// an empty, 0-row batch once rendered via [arrowtest.Rows.Record]).
func pathRows(paths ...string) arrowtest.Rows {
	rows := make(arrowtest.Rows, 0, len(paths))
	for _, p := range paths {
		rows = append(rows, arrowtest.Row{"object_path.utf8": p})
	}
	return rows
}

// TestRowReader_SkipsEmptyNonEOFBatches guards against the reader treating an
// empty (0-row) batch without io.EOF as end-of-section. The underlying reader
// may return such a batch when a read window is fully filtered out; the rows in
// later batches must still be returned rather than silently dropped.
func TestRowReader_SkipsEmptyNonEOFBatches(t *testing.T) {
	alloc := memory.DefaultAllocator
	schema := pathRows("/a", "/b").Schema()
	fake := &scriptedReader{batches: []arrow.RecordBatch{
		pathRows().Record(alloc, schema),           // empty, non-EOF
		pathRows().Record(alloc, schema),           // empty, non-EOF
		pathRows("/a", "/b").Record(alloc, schema), // real rows after the empty batches
	}}

	rr := &RowReader{ctx: context.Background(), reader: fake}
	defer func() { require.NoError(t, rr.Close()) }()

	var paths []string
	for rr.Next() {
		paths = append(paths, rr.At().ObjectPath)
	}
	require.NoError(t, rr.Err())
	require.Equal(t, []string{"/a", "/b"}, paths,
		"rows after leading empty (non-EOF) batches must not be dropped")
}

// TestRowReader_NonEmptyBatchWithEOF ensures a batch that arrives together with
// io.EOF is still fully consumed before iteration ends.
func TestRowReader_NonEmptyBatchWithEOF(t *testing.T) {
	rows := pathRows("/a", "/b")
	fake := &scriptedReader{batches: []arrow.RecordBatch{
		rows.Record(memory.DefaultAllocator, rows.Schema()),
	}}

	rr := &RowReader{ctx: context.Background(), reader: fake}
	defer func() { require.NoError(t, rr.Close()) }()

	var paths []string
	for rr.Next() {
		paths = append(paths, rr.At().ObjectPath)
	}
	require.NoError(t, rr.Err())
	require.Equal(t, []string{"/a", "/b"}, paths)
}
