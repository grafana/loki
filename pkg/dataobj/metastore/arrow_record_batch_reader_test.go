package metastore

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// stubBatchReader is a controllable ArrowRecordBatchReader that records the xcap
// region visible to each call, so tests can assert how the decorator scopes spans.
type stubBatchReader struct {
	openErr error
	readErr error
	readRec arrow.RecordBatch

	openCalls  int
	readCalls  int
	closeCalls int

	openRegion  *xcap.Region
	readRegions []*xcap.Region
}

func (s *stubBatchReader) Open(ctx context.Context) error {
	s.openCalls++
	s.openRegion = xcap.RegionFromContext(ctx)
	return s.openErr
}

func (s *stubBatchReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	s.readCalls++
	region := xcap.RegionFromContext(ctx)
	s.readRegions = append(s.readRegions, region)
	// Recording here proves the decorator scoped a region the inner reader can
	// record into; the value rolls up under the "<prefix>.Read" region.
	region.Record(xcap.StatMetastoreSectionPointersRead.Observe(1))
	return s.readRec, s.readErr
}

func (s *stubBatchReader) Close() { s.closeCalls++ }

var _ ArrowRecordBatchReader = (*stubBatchReader)(nil)

// TestXcapArrowRecordBatchReader_DelegatesAndScopesSpans proves the decorator
// delegates every call to the wrapped reader and runs Open/Read inside spans
// named from the prefix, with a single read span created once and reused across
// Read calls so the inner reader's observations aggregate in one place.
func TestXcapArrowRecordBatchReader_DelegatesAndScopesSpans(t *testing.T) {
	t.Parallel()

	ctx, capture := xcap.NewCapture(context.Background(), nil)
	defer capture.End()

	stub := &stubBatchReader{readErr: io.EOF}
	r := newXcapArrowRecordBatchReader(stub, "metastore.test")

	// Open is delegated and runs inside a "<prefix>.Open" region.
	require.NoError(t, r.Open(ctx))
	require.Equal(t, 1, stub.openCalls)
	require.NotNil(t, stub.openRegion, "Open must run inside a span-linked region")
	require.Equal(t, "metastore.test.Open", stub.openRegion.Name())

	// Two reads are delegated and return the inner reader's results.
	_, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	_, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 2, stub.readCalls)

	// Both reads share a single "<prefix>.Read" span/region: created on the first
	// Read, reused on the next.
	require.Len(t, stub.readRegions, 2)
	require.NotNil(t, stub.readRegions[0])
	require.Same(t, stub.readRegions[0], stub.readRegions[1], "read span is created once and reused")
	require.Equal(t, "metastore.test.Read", stub.readRegions[0].Name())

	r.Close()
	require.Equal(t, 1, stub.closeCalls)

	// Observations recorded by the inner reader during Read roll up under the read
	// region (sum across the two reads).
	require.Equal(t, int64(2),
		xcap.ValueFromRegion[int64](capture, "metastore.test.Read", xcap.StatMetastoreSectionPointersRead))
}

func TestXcapArrowRecordBatchReader_OpenErrorPropagates(t *testing.T) {
	t.Parallel()

	ctx, capture := xcap.NewCapture(context.Background(), nil)
	defer capture.End()

	wantErr := errors.New("open failed")
	stub := &stubBatchReader{openErr: wantErr}
	r := newXcapArrowRecordBatchReader(stub, "metastore.test")

	require.ErrorIs(t, r.Open(ctx), wantErr)
	require.Equal(t, 1, stub.openCalls)
}

func TestXcapArrowRecordBatchReader_CloseWithoutReadIsSafe(t *testing.T) {
	t.Parallel()

	stub := &stubBatchReader{}
	r := newXcapArrowRecordBatchReader(stub, "metastore.test")

	// Close before the first Read must not touch the never-created read span.
	require.NotPanics(t, r.Close)
	require.Equal(t, 1, stub.closeCalls)
}

// unwrapReader returns the concrete reader behind the span-wrapping decorator,
// or r itself when it is not wrapped.
func unwrapReader(r ArrowRecordBatchReader) ArrowRecordBatchReader {
	if w, ok := r.(*xcapArrowRecordBatchReader); ok {
		return w.r
	}
	return r
}
