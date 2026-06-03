package postings_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// TestReadPointers_SchemaParity is the Success Criterion #4 anchor.
// It builds an index dataobj containing postings + streams + (parallel)
// pointers sections, configures pointers.Reader EXACTLY like
// metastore.indexSectionsReader.openStreamPointersReader (FULL 9-column
// default pointer-scan projection — no narrowing), reads one batch from
// each side, and asserts arrow.Schema.Equal byte-for-byte. NEITHER side's
// projection is narrowed.
func TestReadPointers_SchemaParity(t *testing.T) {
	fx := buildJoinedFixture(t, []testStream{
		{streamID: 1, minTs: unixTime(100), maxTs: unixTime(150), rows: 10, uncompressedSize: 1000},
		{streamID: 2, minTs: unixTime(200), maxTs: unixTime(250), rows: 20, uncompressedSize: 2000},
		{streamID: 3, minTs: unixTime(300), maxTs: unixTime(350), rows: 30, uncompressedSize: 3000},
	})

	// Postings side.
	postingsReader := postings.NewReader(postings.ReaderOptions{
		Columns:   fx.postingsSec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, postingsReader.Open(t.Context()))
	t.Cleanup(func() { _ = postingsReader.Close() })

	postingsBatch, err := postingsReader.ReadPointers(t.Context(),
		map[int64]struct{}{1: {}, 2: {}, 3: {}},
		unixTime(0), unixTime(10000))
	require.NoError(t, err)
	require.NotNil(t, postingsBatch)

	// Pointers side: mirror openStreamPointersReader EXACTLY — full 9-column
	// default projection, EqualPredicate(PointerKindStreamIndex),
	// WhereTimeRangeOverlapsWith. NEITHER side narrows.
	pointersCols, err := findPointersColumnsByTypesTestHelper(
		fx.pointersSec.Columns(),
		pointers.ColumnTypePath,
		pointers.ColumnTypeSection,
		pointers.ColumnTypePointerKind,
		pointers.ColumnTypeStreamID,
		pointers.ColumnTypeStreamIDRef,
		pointers.ColumnTypeMinTimestamp,
		pointers.ColumnTypeMaxTimestamp,
		pointers.ColumnTypeRowCount,
		pointers.ColumnTypeUncompressedSize,
	)
	require.NoError(t, err)
	require.Len(t, pointersCols, 9)

	var (
		colPointerKind  *pointers.Column
		colMinTimestamp *pointers.Column
		colMaxTimestamp *pointers.Column
	)
	for _, c := range pointersCols {
		switch c.Type {
		case pointers.ColumnTypePointerKind:
			colPointerKind = c
		case pointers.ColumnTypeMinTimestamp:
			colMinTimestamp = c
		case pointers.ColumnTypeMaxTimestamp:
			colMaxTimestamp = c
		}
	}
	require.NotNil(t, colPointerKind)
	require.NotNil(t, colMinTimestamp)
	require.NotNil(t, colMaxTimestamp)

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(unixTime(0).UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(unixTime(10000).UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	pointersReader := pointers.NewReader(pointers.ReaderOptions{
		Columns: pointersCols,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{Column: colPointerKind, Value: scalar.NewInt64Scalar(int64(pointers.PointerKindStreamIndex))},
			pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
		},
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, pointersReader.Open(t.Context()))
	t.Cleanup(func() { _ = pointersReader.Close() })

	pointersBatch, err := pointersReader.Read(t.Context(), 128)
	require.NoError(t, err)
	require.NotNil(t, pointersBatch)

	require.True(t, postingsBatch.Schema().Equal(pointersBatch.Schema()),
		"postings ReadPointers schema must match pointers.Reader-fed openStreamPointersReader schema byte-for-byte on the FULL 9-column default pointer-scan projection (no narrowing); got\n  postings=%s\n  pointers=%s",
		postingsBatch.Schema(), pointersBatch.Schema())
}

// TestReadPointers_StreamIDFilter asserts that the streamIDs filter limits the
// result to the requested set and that each returned row's min/max_timestamp
// reflects its postings row's [min, max].
func TestReadPointers_StreamIDFilter(t *testing.T) {
	fx := buildJoinedFixture(t, []testStream{
		{streamID: 1, minTs: unixTime(100), maxTs: unixTime(150), rows: 10, uncompressedSize: 1000},
		{streamID: 2, minTs: unixTime(200), maxTs: unixTime(250), rows: 20, uncompressedSize: 2000},
		{streamID: 3, minTs: unixTime(300), maxTs: unixTime(350), rows: 30, uncompressedSize: 3000},
		{streamID: 4, minTs: unixTime(400), maxTs: unixTime(450), rows: 40, uncompressedSize: 4000},
	})

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   fx.postingsSec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	batch, err := r.ReadPointers(t.Context(),
		map[int64]struct{}{1: {}, 3: {}},
		unixTime(0), unixTime(10000))
	require.NoError(t, err)
	require.NotNil(t, batch)

	require.Equal(t, int64(2), batch.NumRows(), "expected exactly 2 rows for streamIDs={1,3}")

	got := materialiseReadPointersRows(t, batch)
	gotIDs := make([]int64, 0, len(got))
	for _, r := range got {
		gotIDs = append(gotIDs, r.streamID)
	}
	require.ElementsMatch(t, []int64{1, 3}, gotIDs)

	for _, r := range got {
		switch r.streamID {
		case 1:
			require.Equal(t, unixTime(100).UnixNano(), r.minTimestamp)
			require.Equal(t, unixTime(150).UnixNano(), r.maxTimestamp)
			// row_count / uncompressed_size are a metadata hint no consumer
			// reads; ReadPointers emits them as zero from the postings path.
			require.Equal(t, int64(0), r.rowCount)
			require.Equal(t, int64(0), r.uncompressedSize)
		case 3:
			require.Equal(t, unixTime(300).UnixNano(), r.minTimestamp)
			require.Equal(t, unixTime(350).UnixNano(), r.maxTimestamp)
			require.Equal(t, int64(0), r.rowCount)
			require.Equal(t, int64(0), r.uncompressedSize)
		}
	}
}

// TestReadPointers_TimeRangeFilter asserts that the time-range filter prunes
// postings rows whose [min,max] does not overlap the requested window.
func TestReadPointers_TimeRangeFilter(t *testing.T) {
	fx := buildJoinedFixture(t, []testStream{
		{streamID: 1, minTs: unixTime(100), maxTs: unixTime(150), rows: 10, uncompressedSize: 1000},
		{streamID: 2, minTs: unixTime(200), maxTs: unixTime(250), rows: 20, uncompressedSize: 2000},
		{streamID: 3, minTs: unixTime(300), maxTs: unixTime(350), rows: 30, uncompressedSize: 3000},
	})

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   fx.postingsSec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	// Only stream 2's [200, 250] overlaps [190, 260].
	batch, err := r.ReadPointers(t.Context(), nil, unixTime(190), unixTime(260))
	require.NoError(t, err)
	require.NotNil(t, batch)

	require.Equal(t, int64(1), batch.NumRows(), "expected exactly 1 row for time range [190,260]")
	got := materialiseReadPointersRows(t, batch)
	require.Equal(t, int64(2), got[0].streamID)
}

// TestReadPointers_EmptyStreamIDs asserts that passing a nil/empty
// streamIDs map applies no stream-ID filter — all streams whose
// [min,max] overlaps the time range are returned.
func TestReadPointers_EmptyStreamIDs(t *testing.T) {
	fx := buildJoinedFixture(t, []testStream{
		{streamID: 1, minTs: unixTime(100), maxTs: unixTime(150), rows: 10, uncompressedSize: 1000},
		{streamID: 2, minTs: unixTime(200), maxTs: unixTime(250), rows: 20, uncompressedSize: 2000},
		{streamID: 3, minTs: unixTime(300), maxTs: unixTime(350), rows: 30, uncompressedSize: 3000},
	})

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   fx.postingsSec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	batch, err := r.ReadPointers(t.Context(), nil, unixTime(0), unixTime(10000))
	require.NoError(t, err)
	require.NotNil(t, batch)

	require.Equal(t, int64(3), batch.NumRows(), "expected all 3 streams when streamIDs is nil")
	got := materialiseReadPointersRows(t, batch)
	gotIDs := make([]int64, 0, len(got))
	for _, r := range got {
		gotIDs = append(gotIDs, r.streamID)
	}
	require.ElementsMatch(t, []int64{1, 2, 3}, gotIDs)
}

// TestReadPointers_NoParentObject pins the decoupling from the streams
// section: ReadPointers works on a postings section opened via the plain
// [Open] (no parent dataobj back-pointer, no sibling streams section),
// because pointers are now sourced entirely from the postings rows.
func TestReadPointers_NoParentObject(t *testing.T) {
	pb := postings.NewBuilder(nil, 0, 0)
	pb.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/test/objB",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "v2",
		StreamID:         2,
		Timestamp:        unixTime(100),
		UncompressedSize: 1,
	})
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(pb))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		opened, openErr := postings.Open(t.Context(), s) // NO parent.
		require.NoError(t, openErr)
		sec = opened
		break
	}
	require.NotNil(t, sec, "postings section missing from fixture")

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	batch, err := r.ReadPointers(t.Context(), nil, unixTime(0), unixTime(10000))
	require.NoError(t, err, "ReadPointers must work without a sibling streams section")
	require.NotNil(t, batch)
	require.Equal(t, int64(1), batch.NumRows())
	got := materialiseReadPointersRows(t, batch)
	require.Equal(t, int64(2), got[0].streamID)
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

type testStream struct {
	streamID         int64
	minTs            time.Time
	maxTs            time.Time
	rows             int64
	uncompressedSize int64
}

type joinedFixture struct {
	obj         *dataobj.Object
	postingsSec *postings.Section
	streamsSec  *streams.Section
	pointersSec *pointers.Section
}

// buildJoinedFixture builds a single dataobj.Object containing three sections
// — postings + streams + (parallel) pointers — with consistent per-stream
// metadata so the schema-parity and behaviour tests can compare both sides.
//
// The postings section contains a single KindLabel posting per test stream
// whose stream_id_bitmap selects that stream (bit at index streamID). The
// streams section records each test stream's (min, max, rows, size). The
// parallel pointers section emits a matching SectionPointer per stream with
// PointerKind=PointerKindStreamIndex for the schema-parity reference reader.
func buildJoinedFixture(t *testing.T, testStreams []testStream) joinedFixture {
	t.Helper()

	const objectPath = "/test/obj"
	const sectionIndex = int64(0)

	pb := postings.NewBuilder(nil, 0, 0)
	sb := streams.NewBuilder(nil, 0, 0)
	ptrb := pointers.NewBuilder(nil, 0, 0)

	for _, ts := range testStreams {
		// Postings: KindLabel observations per stream — set the stream's bit
		// in the bitmap. Use a deterministic (column, value) pair so each test
		// stream gets its own posting row (avoids aggregation collapse).
		// Observe at both minTs and maxTs so the aggregated posting carries the
		// stream's full [min, max] range — ReadPointers reads min/max_timestamp
		// from these postings rows.
		pb.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       objectPath,
			SectionIndex:     sectionIndex,
			ColumnName:       "env",
			LabelValue:       fmt.Sprintf("v%d", ts.streamID),
			StreamID:         ts.streamID,
			Timestamp:        ts.minTs,
			UncompressedSize: ts.uncompressedSize,
		})
		pb.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       objectPath,
			SectionIndex:     sectionIndex,
			ColumnName:       "env",
			LabelValue:       fmt.Sprintf("v%d", ts.streamID),
			StreamID:         ts.streamID,
			Timestamp:        ts.maxTs,
			UncompressedSize: 0,
		})

		// Streams: synthesise per-stream metadata via Record calls. We
		// call Record `rows` times to populate the row count, and use
		// distinct timestamps to set min/max.
		lbls := labels.FromStrings("stream", fmt.Sprintf("s%d", ts.streamID))
		// First record sets min, last sets max.
		_ = sb.Record(lbls, ts.minTs, ts.uncompressedSize)
		for i := int64(1); i < ts.rows-1; i++ {
			_ = sb.Record(lbls, ts.minTs, 0)
		}
		if ts.rows > 1 {
			_ = sb.Record(lbls, ts.maxTs, 0)
		}

		// Pointers: matching SectionPointer with PointerKind=
		// PointerKindStreamIndex. ObserveStream twice (start, end) to
		// set StartTs / EndTs explicitly.
		ptrb.ObserveStream(objectPath, sectionIndex, ts.streamID, ts.streamID, ts.minTs, ts.uncompressedSize)
		ptrb.ObserveStream(objectPath, sectionIndex, ts.streamID, ts.streamID, ts.maxTs, 0)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(pb))
	require.NoError(t, objBuilder.Append(sb))
	require.NoError(t, objBuilder.Append(ptrb))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var fx joinedFixture
	fx.obj = obj

	for _, sec := range obj.Sections() {
		switch {
		case postings.CheckSection(sec):
			s, err := postings.Open(t.Context(), sec)
			require.NoError(t, err)
			fx.postingsSec = s
		case streams.CheckSection(sec):
			s, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			fx.streamsSec = s
		case pointers.CheckSection(sec):
			s, err := pointers.Open(t.Context(), sec)
			require.NoError(t, err)
			fx.pointersSec = s
		}
	}
	require.NotNil(t, fx.postingsSec, "postings section missing from fixture")
	require.NotNil(t, fx.streamsSec, "streams section missing from fixture")
	require.NotNil(t, fx.pointersSec, "pointers section missing from fixture")
	return fx
}

// findPointersColumnsByTypesTestHelper is a verbatim copy of the metastore
// helper of the same name. We duplicate it here to keep the test in
// package postings_test without an internal cross-package dependency on
// pkg/dataobj/metastore (which would create a cycle: metastore already
// depends on pointers + streams + (future) postings).
func findPointersColumnsByTypesTestHelper(allColumns []*pointers.Column, columnTypes ...pointers.ColumnType) ([]*pointers.Column, error) {
	result := make([]*pointers.Column, 0, len(columnTypes))
	for _, c := range allColumns {
		for _, neededType := range columnTypes {
			if neededType != c.Type {
				continue
			}
			result = append(result, c)
		}
	}
	return result, nil
}

type readPointersRow struct {
	objectPath       string
	sectionIndex     int64
	pointerKind      int64
	streamID         int64
	streamIDRef      int64
	minTimestamp     int64
	maxTimestamp     int64
	rowCount         int64
	uncompressedSize int64
}

// materialiseReadPointersRows extracts the 9-column rows from a
// ReadPointers RecordBatch into a Go-side slice for assertions.
// Column ordering matches readPointersOutputSchema().
func materialiseReadPointersRows(t *testing.T, rb arrow.RecordBatch) []readPointersRow {
	t.Helper()

	n := int(rb.NumRows())
	out := make([]readPointersRow, n)

	pathCol := rb.Column(0).(*array.String)
	sectionCol := rb.Column(1).(*array.Int64)
	kindCol := rb.Column(2).(*array.Int64)
	streamIDCol := rb.Column(3).(*array.Int64)
	streamIDRefCol := rb.Column(4).(*array.Int64)
	minTsCol := rb.Column(5).(*array.Timestamp)
	maxTsCol := rb.Column(6).(*array.Timestamp)
	rowCountCol := rb.Column(7).(*array.Int64)
	uncompressedCol := rb.Column(8).(*array.Int64)

	for i := 0; i < n; i++ {
		out[i] = readPointersRow{
			objectPath:       pathCol.Value(i),
			sectionIndex:     sectionCol.Value(i),
			pointerKind:      kindCol.Value(i),
			streamID:         streamIDCol.Value(i),
			streamIDRef:      streamIDRefCol.Value(i),
			minTimestamp:     int64(minTsCol.Value(i)),
			maxTimestamp:     int64(maxTsCol.Value(i)),
			rowCount:         rowCountCol.Value(i),
			uncompressedSize: uncompressedCol.Value(i),
		}
	}
	return out
}

func unixTime(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
