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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/scratch"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// intRecord is a simple test record for the merge heap tests.
type intRecord struct {
	Key int
	Val string
}

// testSequence is a simple sequence[intRecord] for testing.
type testSequence struct {
	idx       int
	records   []intRecord
	index     int
	cur       intRecord
	err       error
	exhausted bool
}

func newTestSequence(records ...intRecord) *testSequence {
	return &testSequence{
		idx:     0,
		records: records,
		index:   0,
	}
}

func newTestSequenceWithIndex(idx int, records ...intRecord) *testSequence {
	return &testSequence{
		idx:     idx,
		records: records,
		index:   0,
	}
}

func (p *testSequence) Next() bool {
	if p.exhausted {
		return false
	}
	if p.index >= len(p.records) {
		p.exhausted = true
		return false
	}
	p.cur = p.records[p.index]
	p.index++
	return true
}

func (p *testSequence) At() intRecord {
	return p.cur
}

func (p *testSequence) Err() error {
	return p.err
}

func (p *testSequence) Index() int {
	return p.idx
}

func (p *testSequence) Close() error {
	return nil
}

var _ sequence[intRecord] = (*testSequence)(nil)

// trackingSequence wraps a sequence and tracks whether Close() was called.
type trackingSequence[R any] struct {
	underlying sequence[R]
	closed     bool
}

func newTrackingSequence[R any](underlying sequence[R]) *trackingSequence[R] {
	return &trackingSequence[R]{underlying: underlying, closed: false}
}

func (p *trackingSequence[R]) Next() bool {
	return p.underlying.Next()
}

func (p *trackingSequence[R]) At() R {
	return p.underlying.At()
}

func (p *trackingSequence[R]) Err() error {
	return p.underlying.Err()
}

func (p *trackingSequence[R]) Index() int {
	return p.underlying.Index()
}

func (p *trackingSequence[R]) Close() error {
	p.closed = true
	return p.underlying.Close()
}

func (p *trackingSequence[R]) wasClosed() bool {
	return p.closed
}

var _ sequence[intRecord] = (*trackingSequence[intRecord])(nil)

// drainMerge drives a K-way merge over the given sequences using the same
// loser-tree helpers as the production merge sites (heapAt, heapLess,
// closeSeq). It yields every record in sorted order to fn; iteration stops
// early when fn returns false. It mirrors the inlined loop in
// mergePostingsIntoBuilder / mergeStatsIntoBuilder so those helpers stay tested
// without a dedicated merge function.
func drainMerge[R any](ctx context.Context, groups []sequence[R], cmp func(a, b R) int, fn func(R) bool) error {
	tree := loser.New(groups, heapVal[R]{isMax: true}, heapAt[R], heapLess(cmp), closeSeq[R])
	defer tree.Close()

	for tree.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !fn(tree.Winner().At()) {
			return nil
		}
	}

	for _, g := range groups {
		if err := g.Err(); err != nil {
			return err
		}
	}
	return nil
}

// TestMerge_DistinctKeys tests merging two sequences with distinct keys.
func TestMerge_DistinctKeys(t *testing.T) {
	ctx := context.Background()

	// Two sequences: {1,3,5} and {2,4,6}
	seq1 := newTestSequence(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
		intRecord{Key: 5, Val: "e"},
	)
	seq2 := newTestSequence(
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

	expected := []intRecord{
		{Key: 1, Val: "a"},
		{Key: 2, Val: "b"},
		{Key: 3, Val: "c"},
		{Key: 4, Val: "d"},
		{Key: 5, Val: "e"},
		{Key: 6, Val: "f"},
	}

	var actual []intRecord
	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// TestMerge_EqualKeysStableOrder verifies that equal-key records are emitted
// in stable group-index order (the merge no longer reduces; collision handling
// lives in the consumers).
func TestMerge_EqualKeysStableOrder(t *testing.T) {
	ctx := context.Background()

	// Two sequences, each with {1, 3}
	seq1 := newTestSequenceWithIndex(0,
		intRecord{Key: 1, Val: "a1"},
		intRecord{Key: 3, Val: "a3"},
	)
	seq2 := newTestSequenceWithIndex(1,
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

	// Every record is yielded; equal keys come out lower-group-index first.
	expected := []intRecord{
		{Key: 1, Val: "a1"},
		{Key: 1, Val: "b1"},
		{Key: 3, Val: "a3"},
		{Key: 3, Val: "b3"},
	}

	var actual []intRecord
	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

// TestMerge_EmptySequences tests merging empty sequences.
func TestMerge_EmptySequences(t *testing.T) {
	ctx := context.Background()

	seq1 := newTestSequence()
	seq2 := newTestSequence()

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	var actual []intRecord
	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(rec intRecord) bool {
		actual = append(actual, rec)
		return true
	})
	require.NoError(t, err)
	require.Len(t, actual, 0)
}

// TestMerge_ContextCancelled tests that context cancellation is honored.
func TestMerge_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	seq1 := newTestSequence(intRecord{Key: 1, Val: "a"})
	seq2 := newTestSequence(intRecord{Key: 2, Val: "b"})

	cmp := func(a, b intRecord) int {
		if a.Key < b.Key {
			return -1
		} else if a.Key > b.Key {
			return 1
		}
		return 0
	}

	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(_ intRecord) bool {
		return true
	})
	require.Equal(t, context.Canceled, err)
}

// TestMerge_ClosesAllSequencesOnEarlyStop tests that merge closes all sequences when the caller stops iteration early.
func TestMerge_ClosesAllSequencesOnEarlyStop(t *testing.T) {
	ctx := context.Background()

	// Create tracking sequences
	seq1 := newTrackingSequence(newTestSequence(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
		intRecord{Key: 5, Val: "e"},
	))
	seq2 := newTrackingSequence(newTestSequence(
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

	// Iterate and stop after the 2nd record
	var count int
	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(_ intRecord) bool {
		count++
		return count < 2 // Stop after 2 records
	})
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Both sequences should be closed
	require.True(t, seq1.wasClosed(), "seq1 should be closed")
	require.True(t, seq2.wasClosed(), "seq2 should be closed")
}

// TestMerge_ClosesAllSequencesOnReadError tests that merge closes all sequences when a read error occurs.
func TestMerge_ClosesAllSequencesOnReadError(t *testing.T) {
	ctx := context.Background()

	// Create a sequence that returns an error
	errorSeq := &errorSequence[intRecord]{}
	trackingErrorSeq := newTrackingSequence(errorSeq)

	// Create a normal tracking sequence
	trackingNormalSeq := newTrackingSequence(newTestSequence(
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

	// Try to iterate; should get an error
	err := drainMerge(ctx, []sequence[intRecord]{trackingErrorSeq, trackingNormalSeq}, cmp, func(_ intRecord) bool {
		return true
	})
	require.Error(t, err)

	// Both sequences should be closed
	require.True(t, trackingErrorSeq.wasClosed(), "error sequence should be closed")
	require.True(t, trackingNormalSeq.wasClosed(), "normal sequence should be closed")
}

// TestMerge_ClosesAllSequencesOnContextCancel tests that merge closes all sequences when context is cancelled.
func TestMerge_ClosesAllSequencesOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create tracking sequences
	seq1 := newTrackingSequence(newTestSequence(
		intRecord{Key: 1, Val: "a"},
		intRecord{Key: 3, Val: "c"},
	))
	seq2 := newTrackingSequence(newTestSequence(
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

	// Start iteration and cancel after first record
	var count int
	err := drainMerge(ctx, []sequence[intRecord]{seq1, seq2}, cmp, func(_ intRecord) bool {
		count++
		if count == 1 {
			cancel()
		}
		return true
	})
	require.Equal(t, context.Canceled, err)

	// Both sequences should be closed
	require.True(t, seq1.wasClosed(), "seq1 should be closed")
	require.True(t, seq2.wasClosed(), "seq2 should be closed")
}

// errorSequence is a test sequence that always returns an error.
type errorSequence[R any] struct {
	exhausted bool
}

func (p *errorSequence[R]) Next() bool {
	if !p.exhausted {
		p.exhausted = true
	}
	return false
}

func (p *errorSequence[R]) At() R {
	var zero R
	return zero
}

func (p *errorSequence[R]) Err() error {
	return io.ErrUnexpectedEOF
}

func (p *errorSequence[R]) Index() int {
	return 0
}

func (p *errorSequence[R]) Close() error {
	return nil
}

func (p *errorSequence[R]) Exhausted() bool {
	return p.exhausted
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

	// 2. Construct an IndexMerge node with a single RunRef referencing the source object.
	//    The SectionIndex is a placeholder; the executor scans all sections.
	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant-1",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{
				{ObjectPath: srcPath, SectionIndex: 0}, // SectionIndex is a placeholder; executor scans all
			}},
		},
	}

	// 3. Create executor context and run the merger.
	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	// 4. Verify the output exists and contains both section kinds.
	exists, err := bucket.Exists(ctx, outputPath)
	require.NoError(t, err)
	require.True(t, exists, "output object must exist")

	outObj := openObjectFromBucket(ctx, t, bucket, outputPath)
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

// TestExecuteIndexMerge_SkipsLegacySections tests that the executor correctly
// skips legacy section types (streams, pointers) while processing a source object
// that contains all four section types: streams, pointers, stats, and postings.
func TestExecuteIndexMerge_SkipsLegacySections(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build a source object with all four section types: streams, pointers, stats, postings.
	srcPath := "source/index-legacy.dat"
	buildSourceWithLegacySections(t, bucket, "tenant-1", srcPath)

	// Construct an IndexMerge node with a single RunRef.
	outputPath := "output/merged.dat"
	node := &physical.IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          "tenant-1",
		OutputIndexPath: outputPath,
		TaskTTL:         time.Minute,
		Runs: []*compactionv2pb.RunRef{
			{Sections: []*compactionv2pb.SectionRef{
				{ObjectPath: srcPath, SectionIndex: 0}, // SectionIndex is a placeholder
			}},
		},
	}

	// Run the executor.
	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err, "merge should succeed despite legacy sections")

	// Verify the output exists.
	exists, err := bucket.Exists(ctx, outputPath)
	require.NoError(t, err)
	require.True(t, exists, "output object must exist")

	outObj := openObjectFromBucket(ctx, t, bucket, outputPath)

	// Check what sections are present in the output.
	var sawPostings, sawStats bool
	for _, sec := range outObj.Sections() {
		if postings.CheckSection(sec) {
			sawPostings = true
		}
		if stats.CheckSection(sec) {
			sawStats = true
		}
	}

	// Legacy sections (streams, pointers) are not passed through the merger.

	require.True(t, sawPostings, "output must contain a postings section")
	require.True(t, sawStats, "output must contain a stats section")

	// Verify content exists in both sections.
	postingsRows := readPostingsRowsFromBucket(ctx, t, bucket, outputPath)
	statsRows := readStatsRowsFromBucket(ctx, t, bucket, outputPath)

	require.NotEmpty(t, postingsRows, "output postings section should have rows")
	require.NotEmpty(t, statsRows, "output stats section should have rows")
}

// buildSourceWithLegacySections builds a dataobj containing streams, pointers,
// stats, and postings sections (all four section types), then uploads it.
// This simulates a first-generation index object from indexobj.Builder.
func buildSourceWithLegacySections(t *testing.T, bucket objstore.Bucket, tenant, path string) {
	t.Helper()
	ctx := context.Background()

	// Use the indexobj.Builder from the observation API to create a full object.
	// This produces objects with the full set of section types: streams, pointers,
	// pointers (index pointers), stats, postings.
	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22, // 4 MiB
		TargetSectionSize:       1 << 21, // 2 MiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}

	builder, err := indexobj.NewBuilder(cfg, nil)
	require.NoError(t, err, "failed to create indexobj.Builder")

	// Append a stream to get a streams section.
	ts := time.Unix(0, 1_000_000)
	_, err = builder.AppendStream(tenant, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "service", Value: "api"}),
		MinTimestamp:     ts,
		MaxTimestamp:     ts.Add(time.Second),
		Rows:             10,
		UncompressedSize: 1000,
	})
	require.NoError(t, err, "failed to append stream")

	// Observe a log line to get a pointers section.
	err = builder.ObserveLogLine(tenant, "log-A", 0, 1, 1, ts, 100)
	require.NoError(t, err, "failed to observe log line")

	// Append a stat to get a stats section.
	err = builder.AppendStat(tenant, "log-A", 0, "service",
		map[string]string{"service": "api"},
		ts, ts.Add(time.Second), 10, 1000)
	require.NoError(t, err, "failed to append stat")

	// Observe a label posting to get a postings section.
	builder.ObserveLabelPosting(tenant, postings.LabelObservation{
		ObjectPath:       "log-A",
		SectionIndex:     0,
		ColumnName:       "service",
		LabelValue:       "api",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

	// Flush the builder to create the object.
	obj, closer, err := builder.Flush()
	require.NoError(t, err, "failed to flush builder")
	defer closer.Close()

	// Upload to bucket.
	require.NoError(t, uploadObjectToBucket(ctx, bucket, path, obj))
}

// buildSourceIndexWithBothKinds builds a dataobj containing one postings section
// (with 1-2 LabelObservations) and one stats section (with 1-2 Stat rows),
// then uploads it to the bucket at the given path.
func buildSourceIndexWithBothKinds(t *testing.T, bucket objstore.Bucket, _, path string) {
	t.Helper()
	ctx := context.Background()

	// Build postings section with one label entry
	postingsBuilder := postings.NewBuilder(nil, 0, 0)
	ts := time.Unix(0, 1_000_000)

	postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
		ObjectPath:       "log-A",
		SectionIndex:     0,
		ColumnName:       "service",
		LabelValue:       "api",
		StreamID:         1,
		Timestamp:        ts,
		UncompressedSize: 100,
	})

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

// openObjectFromBucket downloads and opens a dataobj.Object from the bucket.
func openObjectFromBucket(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) *dataobj.Object {
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
			TargetPageSize:          2048,
			MaxPageRows:             10000,
			TargetObjectSize:        1 << 22, // 4 MiB
			TargetSectionSize:       1 << 21, // 2 MiB
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
		logger: log.NewNopLogger(),
	}
}

// buildSourcePostingsObject builds a dataobj containing a single postings section
// with the given label observations, then uploads it to the bucket.
func buildSourcePostingsObject(t *testing.T, bucket objstore.Bucket, _, path string, observations []postings.LabelObservation) {
	t.Helper()
	ctx := context.Background()

	postingsBuilder := postings.NewBuilder(nil, 0, 0)
	for _, obs := range observations {
		postingsBuilder.ObserveLabelPosting(obs)
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
func buildSourceStatsObject(t *testing.T, bucket objstore.Bucket, _, path string, statsRows []stats.Stat) {
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
func readPostingsRowsFromBucket(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []postings.Row {
	t.Helper()

	obj := openObjectFromBucket(ctx, t, bucket, path)

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

	reader := postings.NewRowReader(ctx, sec)
	defer reader.Close()

	var rows []postings.Row
	for reader.Next() {
		rows = append(rows, reader.At())
	}
	if reader.Err() != nil {
		require.NoError(t, reader.Err())
	}

	return rows
}

// readStatsRowsFromBucket downloads an object from the bucket, finds its
// stats section, and returns all decoded stats rows.
func readStatsRowsFromBucket(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []stats.Stat {
	t.Helper()

	obj := openObjectFromBucket(ctx, t, bucket, path)

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

	reader := stats.NewRowReader(ctx, sec)
	defer reader.Close()

	var rows []stats.Stat
	for reader.Next() {
		rows = append(rows, reader.At())
	}
	if reader.Err() != nil {
		require.NoError(t, reader.Err())
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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
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
	rows := readPostingsRowsFromBucket(ctx, t, bucket, outputPath)

	// Should have 2 unique full keys (overlap collapsed).
	require.Len(t, rows, 2)

	// Rows must be in sort order (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue).
	for i := 0; i < len(rows)-1; i++ {
		assert := comparePostingsRow(rows[i], rows[i+1]) < 0
		require.True(t, assert, "rows not in sort order at index %d", i)
	}

	// The overlapping key (log-A, 0, service, api) should have the winner from Source A
	// (first-wins).
	var apiRow postings.Row
	for _, r := range rows {
		if r.ObjectPath == "log-A" && r.SectionIndex == 0 &&
			r.ColumnName == "service" && r.LabelValue == "api" {
			apiRow = r
			break
		}
	}

	require.NotZero(t, apiRow, "expected to find overlapping key in output")
	// The kept row carries Source A's bitmap.
	// This is a simplified check; a full check would decode the bitmap.
	require.NotEmpty(t, apiRow.StreamIDBitmap)

	// Source A's UncompressedSize is 100; Source B's is 300. The heap's stable
	// tiebreak pops the lower sequence index (Source A) first, and first-wins
	// keeps that row, dropping the later duplicate from Source B.
	// Therefore the merged row's UncompressedSize must be 100.
	require.Equal(t, int64(100), apiRow.UncompressedSize,
		"first-wins merger should keep Source A's UncompressedSize")
}

// TestExecuteIndexMerge_StatsDuplicateFirstWins verifies that stats rows
// which collide on the full merge key — (Labels, MinTimestamp,
// MaxTimestamp, ObjectPath, SectionIndex) — collapse to a single row,
// keeping the first row (first-wins).
func TestExecuteIndexMerge_StatsDuplicateFirstWins(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build two source objects whose stats rows agree on every component of
	// the new merge key, including (ObjectPath, SectionIndex). This is the
	// only scenario in which equal-key collisions are now possible.
	ts1, ts2 := int64(100), int64(200)

	sourceAPath := "source/index-a.dat"
	buildSourceStatsObject(t, bucket, "tenant", sourceAPath, []stats.Stat{
		{
			ObjectPath:       "log-X",
			SectionIndex:     0,
			SortSchema:       "service",
			Labels:           map[string]string{"service": "api"},
			MinTimestamp:     ts1, // identical to source B
			MaxTimestamp:     ts2, // identical to source B
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
			MinTimestamp:     ts1, // identical to source A
			MaxTimestamp:     ts2, // identical to source A
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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: sourceAPath, SectionIndex: 0},
					{ObjectPath: sourceBPath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	rows := readStatsRowsFromBucket(ctx, t, bucket, outputPath)

	// Should have exactly one stats row (duplicate collapsed by first-wins).
	require.Len(t, rows, 1)

	row := rows[0]
	require.Equal(t, "log-X", row.ObjectPath)
	require.Equal(t, int64(0), row.SectionIndex)
	require.Equal(t, "service", row.SortSchema)
	require.Equal(t, map[string]string{"service": "api"}, row.Labels)

	// Timestamps unchanged (both inputs match).
	require.Equal(t, ts1, row.MinTimestamp)
	require.Equal(t, ts2, row.MaxTimestamp)

	require.Equal(t, int64(10), row.RowCount,
		"first-wins must keep Source A's RowCount, not aggregate")
	require.Equal(t, int64(1000), row.UncompressedSize,
		"first-wins must keep Source A's UncompressedSize, not aggregate")
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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: postingsPath, SectionIndex: 0},
				},
			},
			{
				Sections: []*compactionv2pb.SectionRef{
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

	outObj := openObjectFromBucket(ctx, t, bucket, outputPath)
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
	postingsRows := readPostingsRowsFromBucket(ctx, t, bucket, outputPath)
	statRows := readStatsRowsFromBucket(ctx, t, bucket, outputPath)

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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
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

// TestExecuteIndexMerge_StatsDuplicateFirstWinsMultiSource verifies
// first-wins behavior across more than two duplicate sources. With four
// sequences colliding on the same (Labels, MinTimestamp, MaxTimestamp,
// ObjectPath, SectionIndex), the merge emits a single row carrying the
// values from the lowest sequence index (sequence 0 here).
func TestExecuteIndexMerge_StatsDuplicateFirstWinsMultiSource(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build 4 source objects whose stats rows collide on every component of
	// the new merge key. They differ only in their counts, so we can prove
	// which sequence row survives.
	const sourceCount = 4
	rowCounts := []int64{10, 20, 30, 40}
	uncompressedSizes := []int64{1000, 2000, 3000, 4000}
	ts1, ts2 := int64(100), int64(200) // identical for all sources

	for i := range sourceCount {
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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{},
			},
		},
	}

	for i := range sourceCount {
		path := fmt.Sprintf("source/index-%d.dat", i)
		node.Runs[0].Sections = append(node.Runs[0].Sections,
			&compactionv2pb.SectionRef{ObjectPath: path, SectionIndex: 0})
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.NoError(t, err)

	rows := readStatsRowsFromBucket(ctx, t, bucket, outputPath)

	// Should have exactly one row (all duplicates collapsed by first-wins).
	require.Len(t, rows, 1)

	row := rows[0]
	require.Equal(t, int64(10), row.RowCount,
		"first-wins must keep the lowest sequence index's RowCount")
	require.Equal(t, int64(1000), row.UncompressedSize,
		"first-wins must keep the lowest sequence index's UncompressedSize")
	// Timestamps unchanged (all inputs identical).
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
		Runs: []*compactionv2pb.RunRef{
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

// TestStatsRowReader_DottedLabelNames tests that label names containing dots
// are correctly decoded (not truncated by split).
func TestStatsRowReader_DottedLabelNames(t *testing.T) {
	ctx := context.Background()

	// Build a stats section with a label name containing a dot, e.g., "my.svc"
	b := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(1024*1024, 10000))

	b.Append(stats.Stat{
		ObjectPath:       "/obj1",
		SectionIndex:     0,
		SortSchema:       "my.svc",
		Labels:           map[string]string{"my.svc": "api"},
		MinTimestamp:     100,
		MaxTimestamp:     200,
		RowCount:         5,
		UncompressedSize: 50,
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

	// Create a reader and read the row
	reader := stats.NewRowReader(ctx, sec)
	defer reader.Close()

	if !reader.Next() {
		require.Fail(t, "expected to read a row")
	}
	row := reader.At()
	if reader.Err() != nil {
		require.NoError(t, reader.Err())
	}

	// Verify the label name is NOT truncated: should be "my.svc", not "my"
	require.Equal(t, "my.svc", row.SortSchema)
	require.Equal(t, map[string]string{"my.svc": "api"}, row.Labels)
	require.Equal(t, "api", row.Labels["my.svc"])

	// Verify we don't have a truncated label
	_, hasTruncated := row.Labels["my"]
	require.False(t, hasTruncated, "label should not be truncated to 'my'")
}

// TestExecuteIndexMerge_StatsSortSchemaMismatch_FailsLoudly tests that merging
// stats sections with different SortSchema values fails with a clear error
// before any data is written.
func TestExecuteIndexMerge_StatsSortSchemaMismatch_FailsLoudly(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()

	// Build two source objects with DIFFERENT SortSchema values
	sourceAPath := "source/index-a.dat"
	buildSourceStatsObject(t, bucket, "tenant", sourceAPath, []stats.Stat{
		{
			ObjectPath:       "log-X",
			SectionIndex:     0,
			SortSchema:       "service,job",
			Labels:           map[string]string{"service": "api", "job": "j1"},
			MinTimestamp:     100,
			MaxTimestamp:     200,
			RowCount:         10,
			UncompressedSize: 1000,
		},
	})

	sourceBPath := "source/index-b.dat"
	buildSourceStatsObject(t, bucket, "tenant", sourceBPath, []stats.Stat{
		{
			ObjectPath:       "log-X",
			SectionIndex:     0,
			SortSchema:       "job,service", // Different SortSchema
			Labels:           map[string]string{"job": "j1", "service": "api"},
			MinTimestamp:     100,
			MaxTimestamp:     200,
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
		Runs: []*compactionv2pb.RunRef{
			{
				Sections: []*compactionv2pb.SectionRef{
					{ObjectPath: sourceAPath, SectionIndex: 0},
					{ObjectPath: sourceBPath, SectionIndex: 0},
				},
			},
		},
	}

	execCtx := newTestExecutorContext(t, bucket)
	err := execCtx.doIndexMerge(ctx, node)
	require.Error(t, err, "merge should fail with SortSchema mismatch")
	require.Contains(t, err.Error(), "SortSchema", "error should mention SortSchema mismatch")
}
