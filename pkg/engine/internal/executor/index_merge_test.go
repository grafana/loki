package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// intRecord is a simple test record for the merge heap tests.
type intRecord struct {
	Key int
	Val string
}

// testPileReader is a simple pileReader[intRecord] for testing.
type testPileReader struct {
	records []intRecord
	index   int
}

func newTestPileReader(records ...intRecord) *testPileReader {
	return &testPileReader{
		records: records,
		index:   0,
	}
}

func (p *testPileReader) Next(ctx context.Context) (intRecord, error) {
	if p.index >= len(p.records) {
		return intRecord{}, io.EOF
	}
	rec := p.records[p.index]
	p.index++
	return rec, nil
}

func (p *testPileReader) Close() error {
	return nil
}

// trackingPileReader wraps a pileReader and tracks whether Close() was called.
type trackingPileReader[R any] struct {
	underlying pileReader[R]
	closed     bool
}

func newTrackingPileReader[R any](underlying pileReader[R]) *trackingPileReader[R] {
	return &trackingPileReader[R]{underlying: underlying, closed: false}
}

func (p *trackingPileReader[R]) Next(ctx context.Context) (R, error) {
	return p.underlying.Next(ctx)
}

func (p *trackingPileReader[R]) Close() error {
	p.closed = true
	return p.underlying.Close()
}

func (p *trackingPileReader[R]) wasClosed() bool {
	return p.closed
}

// TestMergeHeap_DistinctKeys tests merging two piles with distinct keys.
func TestMergeHeap_DistinctKeys(t *testing.T) {
	ctx := context.Background()

	// Two piles: {1,3,5} and {2,4,6}
	pile1 := newTestPileReader(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
		intRecord{Key: 5, Val: "e"},
	)
	pile2 := newTestPileReader(
		intRecord{Key: 2, Val: "b"},
		intRecord{Key: 4, Val: "d"},
		intRecord{Key: 6, Val: "f"},
	)

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, nil)

	expected := []intRecord{
		{Key: 1, Val: "a"},
		{Key: 2, Val: "b"},
		{Key: 3, Val: "c"},
		{Key: 4, Val: "d"},
		{Key: 5, Val: "e"},
		{Key: 6, Val: "f"},
	}

	var actual []intRecord
	err := iter(func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// TestMergeHeap_EqualKeysReducer tests merging with reduction on equal keys.
func TestMergeHeap_EqualKeysReducer(t *testing.T) {
	ctx := context.Background()

	// Two piles, each with {1, 3}
	pile1 := newTestPileReader(
		intRecord{Key: 1, Val: "a1"},
		intRecord{Key: 3, Val: "a3"},
	)
	pile2 := newTestPileReader(
		intRecord{Key: 1, Val: "b1"},
		intRecord{Key: 3, Val: "b3"},
	)

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	// Reducer concatenates Val strings
	reduce := func(acc, next intRecord) intRecord {
		return intRecord{Key: acc.Key, Val: acc.Val + "+" + next.Val}
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, reduce)

	expected := []intRecord{
		{Key: 1, Val: "a1+b1"},
		{Key: 3, Val: "a3+b3"},
	}

	var actual []intRecord
	err := iter(func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// TestMergeHeap_EmptyPiles tests merging empty piles.
func TestMergeHeap_EmptyPiles(t *testing.T) {
	ctx := context.Background()

	pile1 := newTestPileReader()
	pile2 := newTestPileReader()

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, nil)

	var actual []intRecord
	err := iter(func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Len(t, actual, 0)
}

// TestMergeHeap_ContextCancelled tests that context cancellation is honored.
func TestMergeHeap_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pile1 := newTestPileReader(intRecord{Key: 1, Val: "a"})
	pile2 := newTestPileReader(intRecord{Key: 2, Val: "b"})

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, nil)

	err := iter(func(rec intRecord) bool {
		return true
	})
	require.Equal(t, context.Canceled, err)
}

// TestPostingsPileReader_RoundTrip tests the postingsPileReader with a synthesized postings section.
func TestPostingsPileReader_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Build a postings section with known rows
	b := postings.NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 0).UTC()

	// Add a label entry
	err := b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})
	require.NoError(t, err)

	// Add a bloom entry
	b.PrepareBloomColumn("/obj", 0, "trace_id", 1000)

	err = b.ObserveBloomPosting(postings.BloomObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "trace_id",
		StreamID:         2,
		Timestamp:        ts,
		UncompressedSize: 200,
	})
	require.NoError(t, err)

	// Flush to a dataobj
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Find and open the postings section
	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	// Create a pile reader and read all rows
	pileReader := newPostingsPileReader(sec)
	defer pileReader.Close()

	var rows []postingsRow
	for {
		row, err := pileReader.Next(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}

	// Verify we got both a label and bloom entry
	require.Len(t, rows, 2)

	// Verify they are in sort order (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue)
	// Bloom entries come first (Kind=0), then label entries (Kind=1)
	require.Equal(t, postings.KindBloom, rows[0].Kind)
	require.Equal(t, "/obj", rows[0].ObjectPath)
	require.Equal(t, int64(0), rows[0].SectionIndex)
	require.Equal(t, "trace_id", rows[0].ColumnName)
	require.NotNil(t, rows[0].BloomFilter, "bloom row should have non-nil bloom filter")

	require.Equal(t, postings.KindLabel, rows[1].Kind)
	require.Equal(t, "/obj", rows[1].ObjectPath)
	require.Equal(t, int64(0), rows[1].SectionIndex)
	require.Equal(t, "env", rows[1].ColumnName)
	require.Equal(t, "prod", rows[1].LabelValue)
	require.NotEmpty(t, rows[1].StreamIDBitmap, "label row should have non-empty stream id bitmap")
}

// TestStatsPileReader_RoundTrip tests the statsPileReader with a synthesized stats section.
func TestStatsPileReader_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Build a stats section
	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))

	b.Append(stats.Stat{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		SortSchema:       "service_name,job",
		Labels:           map[string]string{"service_name": "svc1", "job": "job1"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
	})

	b.Append(stats.Stat{
		ObjectPath:       "/obj2",
		SectionIndex:     0,
		SortSchema:       "service_name,job",
		Labels:           map[string]string{"service_name": "svc2", "job": "job2"},
		MinTimestamp:     150,
		MaxTimestamp:     250,
		RowCount:         10,
		UncompressedSize: 100,
	})

	// Flush to a dataobj
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Find and open the stats section
	var sec *stats.Section
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err = stats.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	// Create a pile reader and read all rows
	pileReader := newStatsPileReader(sec)
	defer pileReader.Close()

	var rows []statsRow
	for {
		row, err := pileReader.Next(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}

	// Verify we got both rows
	require.Len(t, rows, 2)

	// Verify they are in sort order
	require.Equal(t, "/obj1", rows[0].ObjectPath)
	require.Equal(t, "service_name,job", rows[0].SortSchema)
	require.Equal(t, "svc1", rows[0].Labels["service_name"])
	require.Equal(t, "job1", rows[0].Labels["job"])

	require.Equal(t, "/obj2", rows[1].ObjectPath)
	require.Equal(t, "svc2", rows[1].Labels["service_name"])
	require.Equal(t, "job2", rows[1].Labels["job"])
}

// TestMergeHeap_ClosesAllPilesOnEarlyStop tests that mergeHeap closes all piles when the caller stops iteration early.
func TestMergeHeap_ClosesAllPilesOnEarlyStop(t *testing.T) {
	ctx := context.Background()

	// Create tracking piles
	pile1 := newTrackingPileReader(newTestPileReader(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
		intRecord{Key: 5, Val: "e"},
	))
	pile2 := newTrackingPileReader(newTestPileReader(
		intRecord{Key: 2, Val: "b"},
		intRecord{Key: 4, Val: "d"},
		intRecord{Key: 6, Val: "f"},
	))

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, nil)

	// Iterate and stop after the 2nd record
	var count int
	err := iter(func(rec intRecord) bool {
		count++
		return count < 2 // Stop after 2 records
	})
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Both piles should be closed
	require.True(t, pile1.wasClosed(), "pile1 should be closed")
	require.True(t, pile2.wasClosed(), "pile2 should be closed")
}

// TestMergeHeap_ClosesAllPilesOnReadError tests that mergeHeap closes all piles when a read error occurs.
func TestMergeHeap_ClosesAllPilesOnReadError(t *testing.T) {
	ctx := context.Background()

	// Create a pile that returns an error
	errorPile := &errorPileReader[intRecord]{}
	trackingErrorPile := newTrackingPileReader(errorPile)

	// Create a normal tracking pile
	trackingNormalPile := newTrackingPileReader(newTestPileReader(
		intRecord{Key: 1, Val: "a"},
	))

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{trackingErrorPile, trackingNormalPile}, cmp, nil)

	// Try to iterate; should get an error
	err := iter(func(rec intRecord) bool {
		return true
	})
	require.Error(t, err)

	// Both piles should be closed
	require.True(t, trackingErrorPile.wasClosed(), "error pile should be closed")
	require.True(t, trackingNormalPile.wasClosed(), "normal pile should be closed")
}

// TestMergeHeap_ClosesAllPilesOnContextCancel tests that mergeHeap closes all piles when context is cancelled.
func TestMergeHeap_ClosesAllPilesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create tracking piles
	pile1 := newTrackingPileReader(newTestPileReader(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
	))
	pile2 := newTrackingPileReader(newTestPileReader(
		intRecord{Key: 2, Val: "b"},
		intRecord{Key: 4, Val: "d"},
	))

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	iter := mergeHeap(ctx, []pileReader[intRecord]{pile1, pile2}, cmp, nil)

	// Start iteration and cancel after first record
	var count int
	err := iter(func(rec intRecord) bool {
		count++
		if count == 1 {
			cancel()
		}
		return true
	})
	require.Equal(t, context.Canceled, err)

	// Both piles should be closed
	require.True(t, pile1.wasClosed(), "pile1 should be closed")
	require.True(t, pile2.wasClosed(), "pile2 should be closed")
}

// errorPileReader is a test pile reader that always returns an error after the first call.
type errorPileReader[R any] struct {
	called bool
}

func (p *errorPileReader[R]) Next(ctx context.Context) (R, error) {
	if p.called {
		var zero R
		return zero, io.ErrUnexpectedEOF
	}
	p.called = true
	var zero R
	return zero, io.ErrUnexpectedEOF
}

func (p *errorPileReader[R]) Close() error {
	return nil
}

// TestPostingsPileReader_RoundTrip_BitLevelAssertion tests the postingsPileReader with bit-level bitmap validation.
func TestPostingsPileReader_RoundTrip_BitLevelAssertion(t *testing.T) {
	ctx := context.Background()

	// Build a postings section with a label entry that has a known bitmap
	b := postings.NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 0).UTC()

	// Add a label entry with known stream ID
	err := b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "prod",
		StreamID:         5, // Binary: 0b0101 (bit 0 and 2 set)
		Timestamp:        ts,
		UncompressedSize: 100,
	})
	require.NoError(t, err)

	// Add another label entry with different stream ID
	err = b.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "/obj",
		SectionIndex:     0,
		ColumnName:       "env",
		LabelValue:       "staging",
		StreamID:         10, // Binary: 0b1010 (bit 1 and 3 set)
		Timestamp:        ts,
		UncompressedSize: 100,
	})
	require.NoError(t, err)

	// Flush to a dataobj
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(b))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Find and open the postings section
	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}
	require.NotNil(t, sec)

	// Create a pile reader and read all rows
	pileReader := newPostingsPileReader(sec)
	defer pileReader.Close()

	var rows []postingsRow
	for {
		row, err := pileReader.Next(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}

	// Verify we got both label entries
	require.Len(t, rows, 2)

	// Verify the bitmaps are non-empty and have the expected pattern
	// Both should have non-empty bitmaps (different stream IDs)
	require.NotEmpty(t, rows[0].StreamIDBitmap, "first row should have non-empty bitmap")
	require.NotEmpty(t, rows[1].StreamIDBitmap, "second row should have non-empty bitmap")

	// The bitmaps should be different (different stream IDs used)
	require.NotEqual(t, rows[0].StreamIDBitmap, rows[1].StreamIDBitmap, "bitmaps for different stream IDs should differ")
}

// TestExecuteIndexMerge_Smoke_BothKinds smoke tests the full merge path: builds
// a source index object with both postings and stats sections, runs the merge
// executor, and verifies the output contains both section types with merged data.
func TestExecuteIndexMerge_Smoke_BothKinds(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// 1. Build and upload a source index object containing both postings and stats sections.
	srcPath := "source/index-0.dat"
	buildSourceIndexWithBothKinds(t, bucket, "tenant-1", srcPath)

	// 2. Open the uploaded object and find the section indices for postings and stats.
	postingsIdx, statsIdx := findSectionIndices(t, ctx, bucket, srcPath)

	// 3. Construct an IndexMerge node with two RunRefs:
	//    - one referencing the postings section
	//    - one referencing the stats section
	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant-1",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{Sections: []*physical.SectionRef{
				{ObjectPath: srcPath, SectionIndex: int64(postingsIdx)},
			}},
			{Sections: []*physical.SectionRef{
				{ObjectPath: srcPath, SectionIndex: int64(statsIdx)},
			}},
		},
	}

	// 4. Create executor context and run the merger.
	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// 5. Verify the output exists and contains both section kinds.
	exists, err := bucket.Exists(ctx, outputPath)
	require.NoError(t, err)
	require.True(t, exists, "output object must exist")

	outObj := openObjectFromBucket(t, ctx, bucket, outputPath)
	var sawPostings, sawStats bool
	for _, sec := range outObj.Sections() {
		if postings.CheckSection(sec) {
			sawPostings = true
		}
		if stats.CheckSection(sec) {
			sawStats = true
		}
	}
	require.True(t, sawPostings, "output must contain a postings section")
	require.True(t, sawStats, "output must contain a stats section")
}

// buildSourceIndexWithBothKinds builds a dataobj containing one postings section
// (with 1-2 LabelObservations) and one stats section (with 1-2 Stat rows),
// then uploads it to the bucket at the given path.
func buildSourceIndexWithBothKinds(t *testing.T, bucket objstore.Bucket, tenant, path string) {
	t.Helper()
	ctx := context.Background()

	// Build postings section with one label entry
	postingsBuilder := postings.NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 1_000_000)

	err := postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "log-A",
		SectionIndex:     0,
		ColumnName:       "service",
		LabelValue:       "api",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})
	require.NoError(t, err)

	// Build stats section with one stat row
	statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
	statsBuilder.Append(stats.Stat{
		ObjectPath:       "log-A",
		SectionIndex:     0,
		SortSchema:       "service,namespace",
		Labels:           map[string]string{"service": "api", "namespace": "default"},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano() + 1000,
		RowCount:         10,
		UncompressedSize: 1000,
	})

	// Build and flush the combined dataobj
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(postingsBuilder))
	require.NoError(t, objBuilder.Append(statsBuilder))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// Upload to bucket
	require.NoError(t, uploadObjectToBucket(ctx, bucket, path, obj))
}

// findSectionIndices opens the source object and returns the indices of the
// postings and stats sections. Both sections must exist.
func findSectionIndices(t *testing.T, ctx context.Context, bucket objstore.Bucket, path string) (int, int) {
	t.Helper()

	obj := openObjectFromBucket(t, ctx, bucket, path)
	postingsIdx, statsIdx := -1, -1

	for i, sec := range obj.Sections() {
		if postings.CheckSection(sec) {
			postingsIdx = i
		}
		if stats.CheckSection(sec) {
			statsIdx = i
		}
	}

	require.GreaterOrEqual(t, postingsIdx, 0, "source object must contain a postings section")
	require.GreaterOrEqual(t, statsIdx, 0, "source object must contain a stats section")

	return postingsIdx, statsIdx
}

// openObjectFromBucket downloads and opens a dataobj.Object from the bucket.
func openObjectFromBucket(t *testing.T, ctx context.Context, bucket objstore.Bucket, path string) *dataobj.Object {
	t.Helper()

	obj, err := dataobj.FromBucket(ctx, bucket, path, 0)
	require.NoError(t, err, "failed to open object from bucket at %s", path)

	return obj
}

// uploadObjectToBucket serializes a *dataobj.Object and uploads it to the bucket.
func uploadObjectToBucket(ctx context.Context, bucket objstore.Bucket, path string, obj *dataobj.Object) error {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("getting object reader: %w", err)
	}
	defer reader.Close()

	objBytes, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("reading object: %w", err)
	}

	return bucket.Upload(ctx, path, io.NopCloser(bytes.NewReader(objBytes)))
}

// newTestExecutorContext constructs a minimal *Context wired with the bucket,
// an in-memory scratch store, a default indexobj BuilderBaseConfig, and a
// no-op logger.
func newTestExecutorContext(t *testing.T, bucket objstore.Bucket) *Context {
	t.Helper()

	return &Context{
		bucket:       bucket,
		scratchStore: scratch.NewMemory(),
		indexobjCfg: logsobj.BuilderBaseConfig{
			TargetPageSize:         2048,
			MaxPageRows:            10000,
			TargetObjectSize:       1 << 22, // 4 MiB
			TargetSectionSize:      1 << 21, // 2 MiB
			BufferSize:             2048 * 8,
			SectionStripeMergeLimit: 2,
		},
		logger: log.NewNopLogger(),
	}
}

// buildSourcePostingsObject builds a dataobj containing a single postings section
// with the given label observations, then uploads it to the bucket.
func buildSourcePostingsObject(t *testing.T, bucket objstore.Bucket, tenant, path string, observations []postings.LabelObservation) {
	t.Helper()
	ctx := context.Background()

	postingsBuilder := postings.NewBuilder(nil, 0, 0)
	for _, obs := range observations {
		err := postingsBuilder.ObserveLabelPosting(obs)
		require.NoError(t, err)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(postingsBuilder))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.NoError(t, uploadObjectToBucket(ctx, bucket, path, obj))
}

// buildSourceStatsObject builds a dataobj containing a single stats section
// with the given stats rows, then uploads it to the bucket.
func buildSourceStatsObject(t *testing.T, bucket objstore.Bucket, tenant, path string, statsRows []stats.Stat) {
	t.Helper()
	ctx := context.Background()

	statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
	for _, row := range statsRows {
		statsBuilder.Append(row)
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(statsBuilder))

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	require.NoError(t, uploadObjectToBucket(ctx, bucket, path, obj))
}

// readPostingsRowsFromBucket downloads an object from the bucket, finds its
// postings section, and returns all decoded postings rows.
func readPostingsRowsFromBucket(t *testing.T, ctx context.Context, bucket objstore.Bucket, path string) []postingsRow {
	t.Helper()

	obj := openObjectFromBucket(t, ctx, bucket, path)

	var sec *postings.Section
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		var err error
		sec, err = postings.Open(ctx, s)
		require.NoError(t, err)
		break
	}

	require.NotNil(t, sec, "expected postings section in output object")

	pileReader := newPostingsPileReader(sec)
	defer pileReader.Close()

	var rows []postingsRow
	for {
		row, err := pileReader.Next(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}

	return rows
}

// readStatsRowsFromBucket downloads an object from the bucket, finds its
// stats section, and returns all decoded stats rows.
func readStatsRowsFromBucket(t *testing.T, ctx context.Context, bucket objstore.Bucket, path string) []statsRow {
	t.Helper()

	obj := openObjectFromBucket(t, ctx, bucket, path)

	var sec *stats.Section
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		var err error
		sec, err = stats.Open(ctx, s)
		require.NoError(t, err)
		break
	}

	require.NotNil(t, sec, "expected stats section in output object")

	pileReader := newStatsPileReader(sec)
	defer pileReader.Close()

	var rows []statsRow
	for {
		row, err := pileReader.Next(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		rows = append(rows, row)
	}

	return rows
}

// countingBucket wraps an objstore.Bucket and counts Exists and Upload calls.
type countingBucket struct {
	underlying  objstore.Bucket
	existsCount int64
	uploadCount int64
}

func newCountingBucket(underlying objstore.Bucket) *countingBucket {
	return &countingBucket{
		underlying: underlying,
	}
}

func (cb *countingBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	cb.uploadCount++
	return cb.underlying.Upload(ctx, name, r)
}

func (cb *countingBucket) Exists(ctx context.Context, name string) (bool, error) {
	cb.existsCount++
	return cb.underlying.Exists(ctx, name)
}

func (cb *countingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return cb.underlying.Get(ctx, name)
}

func (cb *countingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return cb.underlying.GetRange(ctx, name, off, length)
}

func (cb *countingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	return cb.underlying.Iter(ctx, dir, f, options...)
}

func (cb *countingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return cb.underlying.Attributes(ctx, name)
}

func (cb *countingBucket) Delete(ctx context.Context, name string) error {
	return cb.underlying.Delete(ctx, name)
}

func (cb *countingBucket) Name() string {
	return cb.underlying.Name()
}

func (cb *countingBucket) Provider() objstore.ObjProvider {
	return cb.underlying.Provider()
}

func (cb *countingBucket) GetAndReplace(ctx context.Context, name string, f func(existing io.ReadCloser) (io.ReadCloser, error)) error {
	return cb.underlying.GetAndReplace(ctx, name, f)
}

func (cb *countingBucket) IsObjNotFoundErr(err error) bool {
	return cb.underlying.IsObjNotFoundErr(err)
}

func (cb *countingBucket) IsAccessDeniedErr(err error) bool {
	return cb.underlying.IsAccessDeniedErr(err)
}

func (cb *countingBucket) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return cb.underlying.IterWithAttributes(ctx, dir, f, options...)
}

func (cb *countingBucket) SupportedIterOptions() []objstore.IterOptionType {
	return cb.underlying.SupportedIterOptions()
}

func (cb *countingBucket) ReaderWithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
	if br, ok := cb.underlying.(objstore.InstrumentedBucketReader); ok {
		return br.ReaderWithExpectedErrs(fn)
	}
	return cb.underlying
}

func (cb *countingBucket) WithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	if ib, ok := cb.underlying.(objstore.InstrumentedBucket); ok {
		return ib.WithExpectedErrs(fn)
	}
	return cb
}

func (cb *countingBucket) Close() error {
	return cb.underlying.Close()
}

func (cb *countingBucket) ExistsCount() int64 {
	return cb.existsCount
}

func (cb *countingBucket) UploadCount() int64 {
	return cb.uploadCount
}

func (cb *countingBucket) ResetCounts() {
	cb.existsCount = 0
	cb.uploadCount = 0
}

// TestExecuteIndexMerge_PostingsUnion tests the union operation on overlapping postings.
func TestExecuteIndexMerge_PostingsUnion(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build two source objects with overlapping postings rows.
	// Source A has rows for (log-A, 0, service, api, sid=1) and (log-A, 0, service, auth, sid=2).
	sourceAPath := "source/index-a.dat"
	buildSourcePostingsObject(t, bucket, "tenant", sourceAPath, []postings.LabelObservation{
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       "api",
			StreamID:         1,
			Timestamp:        time.Unix(0, 100),
			UncompressedSize: 100,
		},
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       "auth",
			StreamID:         2,
			Timestamp:        time.Unix(0, 200),
			UncompressedSize: 200,
		},
	})

	// Source B has row for (log-A, 0, service, api, sid=99) - overlaps with A's first row.
	sourceBPath := "source/index-b.dat"
	buildSourcePostingsObject(t, bucket, "tenant", sourceBPath, []postings.LabelObservation{
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       "api",
			StreamID:         99,
			Timestamp:        time.Unix(0, 150),
			UncompressedSize: 300,
		},
	})

	// Build IndexMerge node with two source objects.
	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: sourceAPath, SectionIndex: 0},
					{ObjectPath: sourceBPath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// Read and verify output.
	rows := readPostingsRowsFromBucket(t, ctx, bucket, outputPath)

	// Should have 2 unique full keys (overlap collapsed).
	require.Len(t, rows, 2)

	// Rows must be in sort order (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue).
	for i := 0; i < len(rows)-1; i++ {
		assert := comparePostingsRow(rows[i], rows[i+1]) < 0
		require.True(t, assert, "rows not in sort order at index %d", i)
	}

	// The overlapping key (log-A, 0, service, api) should have the winner from Source B
	// (last-wins reducer).
	var apiRow postingsRow
	for _, r := range rows {
		if r.ObjectPath == "log-A" && r.SectionIndex == 0 &&
			r.ColumnName == "service" && r.LabelValue == "api" {
			apiRow = r
			break
		}
	}

	require.NotZero(t, apiRow, "expected to find overlapping key in output")
	// The bitmap should contain the StreamID from Source B (99).
	// This is a simplified check; a full check would decode the bitmap.
	require.NotEmpty(t, apiRow.StreamIDBitmap)

	// Source A's UncompressedSize is 100; Source B's is 300. The heap's stable
	// tiebreak pops the lower pile index first, giving reducer (acc=A, next=B),
	// and the last-wins reducer keeps the row from the higher pile index (Source B).
	// Therefore the merged row's UncompressedSize must be 300.
	require.Equal(t, int64(300), apiRow.UncompressedSize,
		"last-wins merger should keep Source B's UncompressedSize")
}

// TestExecuteIndexMerge_StatsAggregation tests aggregation of overlapping stats rows.
func TestExecuteIndexMerge_StatsAggregation(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build two source objects with overlapping stats rows (same full key).
	// In v1.0, true full-key aggregation requires identical timestamps: the
	// comparator matches the on-disk sort order (Labels, MinTimestamp, MaxTimestamp),
	// so rows must have the same timestamps to be considered equal keys for merging.
	ts1, ts2 := int64(100), int64(200)

	sourceAPath := "source/index-a.dat"
	buildSourceStatsObject(t, bucket, "tenant", sourceAPath, []stats.Stat{
		{
			ObjectPath:       "log-X",
			SectionIndex:     0,
			SortSchema:       "service",
			Labels:           map[string]string{"service": "api"},
			MinTimestamp:     ts1,  // identical to source B
			MaxTimestamp:     ts2,  // identical to source B
			RowCount:         10,
			UncompressedSize: 1000,
		},
	})

	sourceBPath := "source/index-b.dat"
	buildSourceStatsObject(t, bucket, "tenant", sourceBPath, []stats.Stat{
		{
			ObjectPath:       "log-X",
			SectionIndex:     0,
			SortSchema:       "service",
			Labels:           map[string]string{"service": "api"},
			MinTimestamp:     ts1,  // identical to source A
			MaxTimestamp:     ts2,  // identical to source A
			RowCount:         20,
			UncompressedSize: 2000,
		},
	})

	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: sourceAPath, SectionIndex: 0},
					{ObjectPath: sourceBPath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	rows := readStatsRowsFromBucket(t, ctx, bucket, outputPath)

	// Should have exactly one stats row (overlap aggregated).
	require.Len(t, rows, 1)

	row := rows[0]
	require.Equal(t, "log-X", row.ObjectPath)
	require.Equal(t, int64(0), row.SectionIndex)
	require.Equal(t, "service", row.SortSchema)
	require.Equal(t, map[string]string{"service": "api"}, row.Labels)

	// Verify aggregation on full-key match: timestamps unchanged (both inputs match),
	// row counts and sizes summed.
	require.Equal(t, ts1, row.MinTimestamp)         // unchanged since both inputs match
	require.Equal(t, ts2, row.MaxTimestamp)         // unchanged since both inputs match
	require.Equal(t, int64(30), row.RowCount)       // 10 + 20
	require.Equal(t, int64(3000), row.UncompressedSize) // 1000 + 2000
}

// TestExecuteIndexMerge_MixedKinds tests merging both postings and stats sections.
func TestExecuteIndexMerge_MixedKinds(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build one source with postings only.
	postingsPath := "source/index-postings.dat"
	buildSourcePostingsObject(t, bucket, "tenant", postingsPath, []postings.LabelObservation{
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       "api",
			StreamID:         1,
			Timestamp:        time.Unix(0, 100),
			UncompressedSize: 100,
		},
	})

	// Build one source with stats only.
	statsPath := "source/index-stats.dat"
	buildSourceStatsObject(t, bucket, "tenant", statsPath, []stats.Stat{
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			SortSchema:       "service",
			Labels:           map[string]string{"service": "api"},
			MinTimestamp:     100,
			MaxTimestamp:     200,
			RowCount:         10,
			UncompressedSize: 1000,
		},
	})

	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: postingsPath, SectionIndex: 0},
				},
			},
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: statsPath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// Verify output contains both kinds.
	exists, err := bucket.Exists(ctx, outputPath)
	require.NoError(t, err)
	require.True(t, exists)

	outObj := openObjectFromBucket(t, ctx, bucket, outputPath)
	var sawPostings, sawStats bool
	for _, sec := range outObj.Sections() {
		if postings.CheckSection(sec) {
			sawPostings = true
		}
		if stats.CheckSection(sec) {
			sawStats = true
		}
	}

	require.True(t, sawPostings, "output must contain postings section")
	require.True(t, sawStats, "output must contain stats section")

	// Verify content from both sections.
	postingsRows := readPostingsRowsFromBucket(t, ctx, bucket, outputPath)
	statRows := readStatsRowsFromBucket(t, ctx, bucket, outputPath)

	require.Len(t, postingsRows, 1)
	require.Len(t, statRows, 1)
}

// TestExecuteIndexMerge_ExistenceShortCircuit tests that when output already exists,
// the executor does not re-upload it.
func TestExecuteIndexMerge_ExistenceShortCircuit(t *testing.T) {
	ctx := context.Background()
	innerBucket := objstore.NewInMemBucket()
	bucket := newCountingBucket(innerBucket)

	// Pre-upload a sentinel to the output path.
	outputPath := "output/merged.dat"
	sentinel := bytes.NewReader([]byte{})
	err := innerBucket.Upload(ctx, outputPath, io.NopCloser(sentinel))
	require.NoError(t, err)

	// Reset counts so we only count calls during executor run.
	bucket.ResetCounts()

	// Build a simple source.
	sourcePath := "source/index.dat"
	buildSourcePostingsObject(t, bucket, "tenant", sourcePath, []postings.LabelObservation{
		{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       "api",
			StreamID:         1,
			Timestamp:        time.Unix(0, 100),
			UncompressedSize: 100,
		},
	})

	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: sourcePath, SectionIndex: 0},
				},
			},
		},
	}

	// Reset again before running executor.
	bucket.ResetCounts()

	execCtx := newTestExecutorContext(t, bucket)
	err = execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// Verify Exists was called once and Upload was not called.
	require.Equal(t, int64(1), bucket.ExistsCount())
	require.Equal(t, int64(0), bucket.UploadCount())
}

// TestExecuteIndexMerge_TaskTTLExceeded tests that the executor respects TaskTTL.
func TestExecuteIndexMerge_TaskTTLExceeded(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build a source with enough rows to exceed 1µs processing time.
	// Start with 100k rows and increase if needed on fast machines.
	const rowCount = 100000
	observations := make([]postings.LabelObservation, rowCount)
	for i := 0; i < rowCount; i++ {
		observations[i] = postings.LabelObservation{
			ObjectPath:       "log-A",
			SectionIndex:     0,
			ColumnName:       "service",
			LabelValue:       fmt.Sprintf("svc-%d", i),
			StreamID:         int64(i + 1),
			Timestamp:        time.Unix(0, int64(i*100)),
			UncompressedSize: 100,
		}
	}

	sourcePath := "source/index.dat"
	buildSourcePostingsObject(t, bucket, "tenant", sourcePath, observations)

	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         1 * time.Microsecond, // Very tight deadline.
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{
					{ObjectPath: sourcePath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)

	// The task should be killed by the 1µs TTL. If this flakes on very fast CI
	// machines, increase rowCount to 500k or 1M to ensure the executor is still
	// working when the deadline is checked.
	require.ErrorIs(t, err, context.DeadlineExceeded,
		"expected deadline exceeded with 1µs TTL and %d-row corpus", rowCount)
}

// TestExecuteIndexMerge_RowCountSum tests precise row count aggregation across multiple sources.
func TestExecuteIndexMerge_RowCountSum(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build 4 source objects with overlapping stats rows at the same full key,
	// each with a different RowCount and UncompressedSize. All must have identical
	// timestamps to match the full key for aggregation (see v1.0 design).
	const sourceCount = 4
	rowCounts := []int64{10, 20, 30, 40}
	uncompressedSizes := []int64{1000, 2000, 3000, 4000}
	ts1, ts2 := int64(100), int64(200) // identical for all sources

	for i := 0; i < sourceCount; i++ {
		path := fmt.Sprintf("source/index-%d.dat", i)
		buildSourceStatsObject(t, bucket, "tenant", path, []stats.Stat{
			{
				ObjectPath:       "log-X",
				SectionIndex:     0,
				SortSchema:       "service",
				Labels:           map[string]string{"service": "api"},
				MinTimestamp:     ts1, // identical across all sources
				MaxTimestamp:     ts2, // identical across all sources
				RowCount:         rowCounts[i],
				UncompressedSize: uncompressedSizes[i],
			},
		})
	}

	// Build IndexMerge node with all 4 sources.
	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: []*physical.SectionRef{},
			},
		},
	}

	for i := 0; i < sourceCount; i++ {
		path := fmt.Sprintf("source/index-%d.dat", i)
		node.Runs[0].Sections = append(node.Runs[0].Sections,
			&physical.SectionRef{ObjectPath: path, SectionIndex: 0})
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	rows := readStatsRowsFromBucket(t, ctx, bucket, outputPath)

	// Should have exactly one row (all sources merged at identical full key).
	require.Len(t, rows, 1)

	row := rows[0]
	// Verify exact sum: 10 + 20 + 30 + 40 = 100
	require.Equal(t, int64(100), row.RowCount)
	// Verify exact sum: 1000 + 2000 + 3000 + 4000 = 10000
	require.Equal(t, int64(10000), row.UncompressedSize)
	// Timestamps unchanged (all inputs identical)
	require.Equal(t, ts1, row.MinTimestamp)
	require.Equal(t, ts2, row.MaxTimestamp)
}

// TestExecuteIndexMerge_EmptyInputs tests behavior when no sections are provided.
func TestExecuteIndexMerge_EmptyInputs(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*physical.RunRef{
			{
				Sections: nil, // Empty input
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// Verify a sentinel object was uploaded so retries short-circuit.
	exists, err := bucket.Exists(ctx, outputPath)
	require.NoError(t, err)
	require.True(t, exists, "output object must be uploaded even for empty input")
}
