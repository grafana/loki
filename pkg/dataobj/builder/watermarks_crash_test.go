package builder

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"

	"github.com/grafana/loki/pkg/push"
)

// crashHooks inject a crash into one run of the builder. A nil hook does
// nothing.
type crashHooks struct {
	// beforeTOCWrite runs before the n-th ToC write of the run, counted from 1.
	// An error fails the write, so the flush fails and the run ends.
	beforeTOCWrite func(n int) error
	// onCommit runs before the n-th commit of the run, counted from 1. It
	// returns true if the commit must not happen.
	onCommit func(n int) bool
	// afterCommit runs after the n-th commit of the run. It returns true if the
	// builder must stop now.
	afterCommit func(n int) bool
	// afterIndex runs after the n-th index of the run, before the flush uses
	// it. An error fails the flush, so the run ends.
	afterIndex func(n int) error
}

// crashEnv is the state that survives a restart of the builder: the bucket and
// the committed offset.
type crashEnv struct {
	bucket    *objstore.InMemBucket
	idxBucket objstore.Bucket

	// crashed is true after a hook crashed the builder.
	crashed atomic.Bool

	// lateLogs is the late log threshold of the builder. Zero turns the
	// separation of late logs off.
	lateLogs time.Duration

	committed atomic.Int64

	// logObjectOf maps the path of an index object to the path of the log
	// object it indexes. Flushes with concurrency above 1 write it in parallel.
	logObjectOf sync.Map
}

func newCrashEnv() *crashEnv {
	bucket := objstore.NewInMemBucket()
	e := &crashEnv{
		bucket:    bucket,
		idxBucket: metastore.NewIndexBucket(bucket),
	}
	e.committed.Store(-1)
	return e
}

func (e *crashEnv) resumeOffset() int64 {
	return e.committed.Load() + 1
}

type recordingIndexer struct {
	inner indexer
	env   *crashEnv
	hooks crashHooks

	indexes atomic.Int64
}

func (r *recordingIndexer) Index(ctx context.Context, obj *dataobj.Object, objPath string) (index.Result, error) {
	res, err := r.inner.Index(ctx, obj, objPath)
	if err != nil {
		return res, err
	}
	r.env.logObjectOf.Store(res.Path, objPath)
	if n := int(r.indexes.Add(1)); r.hooks.afterIndex != nil {
		if err := r.hooks.afterIndex(n); err != nil {
			r.env.crashed.Store(true)
			return index.Result{}, fmt.Errorf("%w: %w", index.ErrUnprocessableObject, err)
		}
	}
	return res, nil
}

type hookedTOCWriter struct {
	inner *metastore.TableOfContentsWriter
	hooks crashHooks
	env   *crashEnv

	writes atomic.Int64
}

func (h *hookedTOCWriter) before() error {
	n := int(h.writes.Inc())
	if h.hooks.beforeTOCWrite != nil {
		if err := h.hooks.beforeTOCWrite(n); err != nil {
			h.env.crashed.Store(true)
			return err
		}
	}
	return nil
}

func (h *hookedTOCWriter) WriteEntriesWithWatermark(ctx context.Context, tenant string, entries []metastore.TableOfContentsEntry, update metastore.WatermarkUpdate) error {
	if err := h.before(); err != nil {
		return err
	}
	return h.inner.WriteEntriesWithWatermark(ctx, tenant, entries, update)
}

type hookedCommitter struct {
	env    *crashEnv
	hooks  crashHooks
	cancel context.CancelFunc

	commits atomic.Int64
}

func (h *hookedCommitter) Commit(_ context.Context, _ int32, offset int64) error {
	n := int(h.commits.Inc())
	if h.hooks.onCommit != nil && h.hooks.onCommit(n) {
		h.env.crashed.Store(true)
		h.cancel()
		return context.Canceled
	}
	h.env.committed.Store(offset)
	if h.hooks.afterCommit != nil && h.hooks.afterCommit(n) {
		h.env.crashed.Store(true)
		h.cancel()
	}
	return nil
}

// run starts a builder that resumes at the committed offset, feeds it the
// records from that offset, and waits until it stops. It cuts a log object
// after every flushEvery records, counting dropped records. The number of
// records to read must be a multiple of flushEvery, or the last records stay
// in the builder. The builder stops by itself after the last
// record, or because a hook crashes it.
func (e *crashEnv) run(t *testing.T, records []*kgo.Record, flushEvery, concurrency int, hooks crashHooks) (crashed bool) {
	t.Helper()

	var cfg Config
	cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
	cfg.LogsobjBuilder.DropDuplicates = true

	var (
		logger       = log.NewNopLogger()
		reg          = prometheus.NewRegistry()
		scratchStore = scratch.NewMemory()
	)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	factory, err := logsobj.NewBuilderFactory(cfg.LogsobjBuilder, scratchStore, logsobj.NewBuilderMetrics(), logger, logsobj.NoTenantOverrides{})
	require.NoError(t, err)
	indexobjMetrics := indexobj.NewBuilderMetrics(reg)
	idx, err := index.NewSimpleIndexer(cfg.IndexobjBuilder, scratchStore, logger, e.idxBucket, index.NewIndexerMetrics(reg), indexobjMetrics, index.NewCalculatorMetrics(reg), fakeTenantOverrides{})
	require.NoError(t, err)

	uploader := dataobj_uploader.New(dataobj_uploader.Config{SHAPrefixSize: 2}, e.bucket, logger)
	tocWriter := &hookedTOCWriter{
		inner: metastore.NewTableOfContentsWriter(e.idxBucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, logger, metastore.NewTocWriterMetrics(reg), indexobjMetrics),
		hooks: hooks,
		env:   e,
	}
	committer := &hookedCommitter{env: e, hooks: hooks, cancel: cancel}
	w := newWatermarkCache("test-topic", 0, e.idxBucket, NewWatermarkMetrics(reg))
	resume := e.resumeOffset()
	flushCommitter := newFlushCommitter(newFlusher(uploader, logger, reg), committer, &recordingIndexer{inner: idx, env: e, hooks: hooks}, tocWriter, "test-topic", 0, concurrency, logger, reg)

	input := make(chan *kgo.Record)
	overrides := TenantOverrides(testOverrides{})
	if e.lateLogs > 0 {
		overrides = constThreshold(e.lateLogs)
	}
	m := &fixedCountMultiBuilder{
		MultiObjectBuilder: NewMultiObjectBuilder(factory, int(cfg.LogsobjBuilder.TargetObjectSize), w, overrides),
		fullAfter:          flushEvery,
	}
	proc := newProcessor(m, input, flushCommitter, time.Hour, 0, logger, reg)

	// Run returns an error for a crash that fails a flush, and nil for a crash
	// that cancels the context. Both end the run.
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		_ = proc.Run(ctx)
	}()

	// The trailer makes the processor flush the last records. The processor
	// never flushes the trailer itself.
	var pending []*kgo.Record
	for _, rec := range records {
		if rec.Offset >= resume {
			pending = append(pending, rec)
		}
	}
	last := records[len(records)-1]
	trailer := newTestRecord(t, "a", last.Timestamp)
	trailer.Offset = last.Offset + 1
	pending = append(pending, trailer)

	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		for _, rec := range pending {
			select {
			case input <- rec:
			case <-ctx.Done():
				return
			case <-ran:
				return
			}
		}
		close(input)
		<-ctx.Done()
	}()

	select {
	case <-ran:
	case <-time.After(time.Minute):
		t.Fatal("the builder did not stop")
	}
	cancel()
	<-feedDone
	return e.crashed.Swap(false)
}

// lineCounts returns how many times each log line is reachable through the
// ToCs of the tenants. A log object is reachable if an index pointer of a ToC
// points to an index of it.
func (e *crashEnv) lineCounts(t *testing.T, tenants []string, windows []time.Time) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, tenant := range tenants {
		for _, window := range windows {
			e.addLineCounts(t, counts, tenant, window)
		}
	}
	return counts
}

func (e *crashEnv) addLineCounts(t *testing.T, counts map[string]int, tenant string, window time.Time) {
	t.Helper()
	ctx := t.Context()
	{
		toc, err := dataobj.FromBucket(ctx, e.idxBucket, metastore.TableOfContentsPath(tenant, window), 0)
		if err != nil {
			require.True(t, e.idxBucket.IsObjNotFoundErr(err), "ToC of %s: %v", tenant, err)
			return
		}
		for res := range indexpointers.Iter(ctx, toc) {
			pointer, err := res.Value()
			require.NoError(t, err)

			logPath, ok := e.logObjectOf.Load(pointer.Path)
			require.True(t, ok, "unknown index %s", pointer.Path)

			obj, err := dataobj.FromBucket(ctx, e.bucket, logPath.(string), 0)
			require.NoError(t, err)
			for rec := range logs.Iter(ctx, obj) {
				r, err := rec.Value()
				require.NoError(t, err)
				counts[string(r.Line)]++
			}
		}
	}
}

// newCrashRecords returns one record per offset from 0, alternating between
// the tenants "a" and "b". Every third record lies in the second ToC window.
// The line of a record is "line-<offset>". Every fourth record also has a line
// "line-<offset>-b" in the other window, so the record spans both windows.
func newCrashRecords(t *testing.T, count int) []*kgo.Record {
	t.Helper()
	records := make([]*kgo.Record, 0, count)
	for offset := range count {
		window := testWindow1
		if offset%3 == 2 {
			window = testWindow2
		}
		ts := window.Add(time.Duration(offset+1) * time.Minute)
		entries := []push.Entry{{Timestamp: ts, Line: fmt.Sprintf("line-%d", offset)}}
		if offset%4 == 3 {
			other := testWindow1
			if window.Equal(testWindow1) {
				other = testWindow2
			}
			entries = append(entries, push.Entry{Timestamp: other.Add(time.Duration(offset+1) * time.Minute), Line: fmt.Sprintf("line-%d-b", offset)})
		}
		stream := logproto.Stream{Labels: `{foo="bar"}`, Entries: entries}
		value, err := stream.Marshal()
		require.NoError(t, err)
		records = append(records, &kgo.Record{
			Key:       []byte([]string{"a", "b"}[offset%2]),
			Value:     value,
			Timestamp: ts,
			Offset:    int64(offset),
		})
	}
	return records
}

// TestWatermarks_CrashRecovery stops the builder at each point of the flush
// timeline, restarts it with different log object boundaries, and counts the lines that
// the ToCs reach.
func TestWatermarks_CrashRecovery(t *testing.T) {
	const recordCount = 12
	var (
		records = newCrashRecords(t, recordCount)
		tenants = []string{"a", "b"}
		windows = []time.Time{testWindow1, testWindow2}
	)
	allLines := make(map[string]int, recordCount)
	for offset := range recordCount {
		allLines[fmt.Sprintf("line-%d", offset)] = 1
		if offset%4 == 3 {
			allLines[fmt.Sprintf("line-%d-b", offset)] = 1
		}
	}

	// The first run cuts a log object after every 4 records. The first flush
	// (offsets 0 to 3) completes with 4 ToC writes. The crash hits the second
	// flush (offsets 4 to 7), which has 3 ToC writes of its own. The last of
	// them is the second window of the record at offset 7.
	crashAtTOCWrite := func(crashAt int) crashHooks {
		return crashHooks{beforeTOCWrite: func(n int) error {
			if n == crashAt {
				return errors.New("crash at a ToC write")
			}
			return nil
		}}
	}
	for _, tc := range []struct {
		name  string
		hooks crashHooks
	}{
		{name: "reaches every line once after a stop before the ToC writes", hooks: crashAtTOCWrite(5)},
		{name: "reaches every line once after a stop during the ToC writes", hooks: crashAtTOCWrite(6)},
		{name: "reaches every line once after a stop between the ToCs of two windows of one record", hooks: crashAtTOCWrite(7)},
		{name: "reaches every line once after a stop after the ToC writes and before the commit", hooks: crashHooks{onCommit: func(n int) bool { return n == 2 }}},
		{name: "reaches every line once after a stop after the commit", hooks: crashHooks{afterCommit: func(n int) bool { return n == 2 }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, concurrency := range []int{1, 2} {
				env := newCrashEnv()
				require.True(t, env.run(t, records, 4, concurrency, tc.hooks), "the hook did not crash the first run")
				require.False(t, env.run(t, records, 2, concurrency, crashHooks{}))

				require.Equal(t, allLines, env.lineCounts(t, tenants, windows), "flush concurrency %d", concurrency)
			}
		})
	}
}

// logPathsOf returns the paths of the log objects that the index pointers of
// toc reach.
func (e *crashEnv) logPathsOf(t *testing.T, toc *dataobj.Object) []string {
	t.Helper()
	var paths []string
	for res := range indexpointers.Iter(t.Context(), toc) {
		pointer, err := res.Value()
		require.NoError(t, err)
		logPath, ok := e.logObjectOf.Load(pointer.Path)
		require.True(t, ok, "unknown index %s", pointer.Path)
		paths = append(paths, logPath.(string))
	}
	return paths
}

// TestWatermarks_CrashRecoveryLateLogs stops the builder at each point of the
// flush timeline with late log separation on, restarts it with other log object
// cuts, and counts the lines that the ToCs reach. It also checks that no log
// object mixes late and in-time lines.
func TestWatermarks_CrashRecoveryLateLogs(t *testing.T) {
	const recordCount = 12
	var (
		records = newLateCrashRecords(t, recordCount)
		tenants = []string{"a", "b"}
		windows = []time.Time{testWindow1, testWindow2}
	)
	allLines := make(map[string]int, 2*recordCount)
	for offset := range recordCount {
		allLines[fmt.Sprintf("fresh-%d", offset)] = 1
		if offset%2 == 1 {
			allLines[fmt.Sprintf("late-%d", offset)] = 1
		}
	}

	// The probe run shows how many ToC writes the first run makes, so the
	// crash points cover its first, middle and last write.
	var tocWrites int
	probe := newCrashEnv()
	probe.lateLogs = testLateThreshold
	require.False(t, probe.run(t, records, 4, 1, crashHooks{beforeTOCWrite: func(n int) error {
		tocWrites = n
		return nil
	}}))
	require.Greater(t, tocWrites, 3)
	require.Equal(t, allLines, probe.lineCounts(t, tenants, windows))

	crashAtTOCWrite := func(crashAt int) crashHooks {
		return crashHooks{beforeTOCWrite: func(n int) error {
			if n == crashAt {
				return errors.New("crash at a ToC write")
			}
			return nil
		}}
	}
	crashAfterIndex := func(crashAt int) crashHooks {
		return crashHooks{afterIndex: func(n int) error {
			if n == crashAt {
				return errors.New("crash after an index")
			}
			return nil
		}}
	}
	for _, tc := range []struct {
		name  string
		hooks crashHooks
	}{
		{name: "reaches every line once after a stop before the first ToC write", hooks: crashAtTOCWrite(1)},
		{name: "reaches every line once after a stop before a ToC write in the middle", hooks: crashAtTOCWrite(tocWrites / 2)},
		{name: "reaches every line once after a stop before the last ToC write", hooks: crashAtTOCWrite(tocWrites)},
		{name: "reaches every line once after a stop between the objects of the first ToC", hooks: crashAfterIndex(1)},
		{name: "reaches every line once after a stop between the objects of a ToC in the middle", hooks: crashAfterIndex(7)},
		{name: "reaches every line once after a stop after the ToC writes and before the commit", hooks: crashHooks{onCommit: func(n int) bool { return n == 2 }}},
		{name: "reaches every line once after a stop after the commit", hooks: crashHooks{afterCommit: func(n int) bool { return n == 2 }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, concurrency := range []int{1, 2} {
				env := newCrashEnv()
				env.lateLogs = testLateThreshold
				require.True(t, env.run(t, records, 4, concurrency, tc.hooks), "the hook did not crash the first run")
				require.False(t, env.run(t, records, 2, concurrency, crashHooks{}))

				require.Equal(t, allLines, env.lineCounts(t, tenants, windows), "flush concurrency %d", concurrency)
				seen := make(map[string]struct{})
				for _, kinds := range env.lineKinds(t, tenants, windows) {
					require.Len(t, uniqueStrings(kinds), 1, "a log object mixes late and in-time lines: %v", kinds)
					seen[kinds[0]] = struct{}{}
				}
				require.Len(t, seen, 2, "the objects must hold both late and in-time lines")
			}
		})
	}
}

func uniqueStrings(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

// lineKinds returns the kinds of the lines of every data object that the ToCs
// of the tenants and windows reach. A kind is the part of a line before the
// first dash: "late" or "fresh".
func (e *crashEnv) lineKinds(t *testing.T, tenants []string, windows []time.Time) [][]string {
	t.Helper()
	ctx := t.Context()
	var objects [][]string
	for _, tenant := range tenants {
		for _, window := range windows {
			toc, err := dataobj.FromBucket(ctx, e.idxBucket, metastore.TableOfContentsPath(tenant, window), 0)
			if err != nil {
				require.True(t, e.idxBucket.IsObjNotFoundErr(err), "ToC of %s: %v", tenant, err)
				continue
			}
			for _, logPath := range e.logPathsOf(t, toc) {
				obj, err := dataobj.FromBucket(ctx, e.bucket, logPath, 0)
				require.NoError(t, err)
				var kinds []string
				for rec := range logs.Iter(ctx, obj) {
					r, err := rec.Value()
					require.NoError(t, err)
					kinds = append(kinds, strings.SplitN(string(r.Line), "-", 2)[0])
				}
				objects = append(objects, kinds)
			}
		}
	}
	return objects
}

// newLateCrashRecords returns one record per offset from 0, alternating
// between the tenants "a" and "b". The records at odd offsets arrive 3 hours
// after the line "late-<offset>" and carry the line "fresh-<offset>" too, so
// they feed both builders. The other records carry only "fresh-<offset>".
// Every third record lies in the second ToC window.
func newLateCrashRecords(t *testing.T, count int) []*kgo.Record {
	t.Helper()
	records := make([]*kgo.Record, 0, count)
	for offset := range count {
		window := testWindow1
		if offset%3 == 2 {
			window = testWindow2
		}
		ts := window.Add(time.Duration(offset+1) * time.Minute)
		recTime := ts
		entries := []push.Entry{{Timestamp: ts, Line: fmt.Sprintf("fresh-%d", offset)}}
		if offset%2 == 1 {
			recTime = ts.Add(3 * time.Hour)
			entries = []push.Entry{
				{Timestamp: ts, Line: fmt.Sprintf("late-%d", offset)},
				{Timestamp: recTime.Add(-time.Minute), Line: fmt.Sprintf("fresh-%d", offset)},
			}
		}
		stream := logproto.Stream{Labels: `{foo="bar"}`, Entries: entries}
		value, err := stream.Marshal()
		require.NoError(t, err)
		records = append(records, &kgo.Record{
			Key:       []byte([]string{"a", "b"}[offset%2]),
			Value:     value,
			Timestamp: recTime,
			Offset:    int64(offset),
		})
	}
	return records
}
