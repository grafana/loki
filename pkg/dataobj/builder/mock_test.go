package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// countingCloser counts how many times it was closed. When inner is set it is
// closed too, so a closer substituted by a mock still releases the real
// resource behind it. It is safe for concurrent use.
type countingCloser struct {
	mtx    sync.Mutex
	closed int
	err    error
	inner  io.Closer
}

func (c *countingCloser) Close() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.closed++
	if c.inner != nil {
		_ = c.inner.Close()
	}
	return c.err
}

// mockBuilder mocks a [logsobj.Builder].
type mockBuilder struct {
	builder *logsobj.Builder
	nextErr error
	// full, when true, forces IsFull to report the builder as full regardless
	// of the underlying builder's estimated size.
	full bool
	// flushCloser, when set, is returned by Flush in place of the underlying
	// builder's closer, letting tests observe and fail the release of the
	// unsorted object.
	flushCloser *countingCloser
}

func (m *mockBuilder) Append(tenant string, stream logproto.Stream, recTime time.Time) error {
	if err := m.nextErr; err != nil {
		m.nextErr = nil
		return err
	}
	return m.builder.Append(tenant, stream, recTime)
}

func (m *mockBuilder) GetEarliestRecordTime() time.Time {
	return m.builder.GetEarliestRecordTime()
}

func (m *mockBuilder) GetEstimatedSize() int {
	return m.builder.GetEstimatedSize()
}

func (m *mockBuilder) IsFull() bool {
	return m.full || m.builder.IsFull()
}

func (m *mockBuilder) CopyAndSort(ctx context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error) {
	return m.builder.CopyAndSort(ctx, obj)
}

func (m *mockBuilder) Flush() (*dataobj.Object, io.Closer, error) {
	if err := m.nextErr; err != nil {
		m.nextErr = nil
		return nil, nil, err
	}
	obj, closer, err := m.builder.Flush()
	if err != nil || m.flushCloser == nil {
		return obj, closer, err
	}
	m.flushCloser.inner = closer
	return obj, m.flushCloser, nil
}

func (m *mockBuilder) TimeRanges() []dataobj.TimeRange {
	return m.builder.TimeRanges()
}

// A mockCommitter implements the committer interface for tests.
type mockCommitter struct {
	offsets []int64
	// onCommit, if set, runs before the offset is recorded, letting tests check
	// the state that the commit sees.
	onCommit func()
}

func (m *mockCommitter) Commit(_ context.Context, _ int32, offset int64) error {
	if m.onCommit != nil {
		m.onCommit()
	}
	m.offsets = append(m.offsets, offset)
	return nil
}

// mockFlusher implements the flusher interface, handing out a distinct object
// path per flush. It is safe for concurrent use.
type mockFlusher struct {
	mtx     sync.Mutex
	flushes int
	// obj is returned by every flush, letting tests assert that the flushed
	// object reaches the caller.
	obj *dataobj.Object
	// closer is closed each time the caller closes a flushed object, letting
	// tests assert that the caller releases every flushed object.
	closer countingCloser
	// open counts the flushed objects that the caller has not closed yet.
	// maxOpen is the largest value that open reached.
	open, maxOpen int
	// onFlush, if set, runs before each flush returns. It runs without the
	// lock, so it must be safe for concurrent use.
	onFlush func()
}

func (m *mockFlusher) Flush(_ context.Context, _ builder, _ string) (*dataobj.Object, io.Closer, string, error) {
	m.mtx.Lock()
	m.flushes++
	path := fmt.Sprintf("object_%03d", m.flushes)
	m.open++
	m.maxOpen = max(m.maxOpen, m.open)
	m.mtx.Unlock()

	if m.onFlush != nil {
		m.onFlush()
	}
	return m.obj, mockFlushedObject{flusher: m}, path, nil
}

// mockFlushedObject is the closer of an object that a [mockFlusher] flushed.
type mockFlushedObject struct {
	flusher *mockFlusher
}

func (o mockFlushedObject) Close() error {
	o.flusher.mtx.Lock()
	o.flusher.open--
	o.flusher.mtx.Unlock()
	return o.flusher.closer.Close()
}

// mockIndexer implements the indexer interface, recording what it was asked to
// index. Each index is reported at "index/" followed by the object path. It is
// safe for concurrent use.
type mockIndexer struct {
	mtx   sync.Mutex
	objs  []*dataobj.Object
	paths []string
	// errs is consumed one entry per call so tests can drive retries. Once it
	// is exhausted, indexing succeeds.
	errs []error
	// delay is how long each call sleeps before it indexes. In a synctest
	// bubble, the sleep advances the fake clock.
	delay time.Duration
}

func (m *mockIndexer) Index(_ context.Context, obj *dataobj.Object, objPath string) (index.Result, error) {
	time.Sleep(m.delay)
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.objs = append(m.objs, obj)
	m.paths = append(m.paths, objPath)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		return index.Result{}, err
	}
	return index.Result{
		Path:      "index/" + objPath,
		TimeRange: dataobj.TimeRange{Tenant: "test"},
	}, nil
}

// mockTOCWriter implements the tocWriter interface, recording the entries it
// was asked to write. It is safe for concurrent use.
type mockTOCWriter struct {
	mtx     sync.Mutex
	paths   []string
	tenants []string
	err     error
	// afterWrite, if set, runs after an entry is recorded, letting tests act
	// between updating the ToC and committing the offset.
	afterWrite func()
}

func (m *mockTOCWriter) WriteEntry(_ context.Context, tenant string, entry metastore.TableOfContentsEntry) error {
	if m.afterWrite != nil {
		defer m.afterWrite()
	}
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.err != nil {
		return m.err
	}
	m.paths = append(m.paths, entry.Path)
	m.tenants = append(m.tenants, tenant)
	return nil
}

type mockFlushCommitter struct {
	flushes int
	// lastBuilderCount records the number of builders passed to the most
	// recent Flush call, letting tests assert how a partition was split
	// across windows.
	lastBuilderCount int
	lastReason       string
	lastOffset       int64
}

func (m *mockFlushCommitter) Flush(_ context.Context, builders []builder, reason string, offset int64) error {
	m.flushes++
	m.lastBuilderCount = len(builders)
	m.lastReason = reason
	m.lastOffset = offset
	return nil
}

// gatedFlushCommitter blocks each Flush until the test sends its result on
// results, or until the context is canceled. It records the offset, reason
// and builders of each Flush when the Flush starts, and the largest number of
// flushes that ran at the same time. It does not use the builders. It is safe
// for concurrent use.
type gatedFlushCommitter struct {
	results chan error

	mtx        sync.Mutex
	offsets    []int64
	reasons    []string
	builders   [][]builder
	running    int
	maxRunning int
}

func newGatedFlushCommitter() *gatedFlushCommitter {
	return &gatedFlushCommitter{results: make(chan error)}
}

func (m *gatedFlushCommitter) Flush(ctx context.Context, builders []builder, reason string, offset int64) error {
	m.mtx.Lock()
	m.offsets = append(m.offsets, offset)
	m.reasons = append(m.reasons, reason)
	m.builders = append(m.builders, builders)
	m.running++
	m.maxRunning = max(m.maxRunning, m.running)
	m.mtx.Unlock()

	defer func() {
		m.mtx.Lock()
		m.running--
		m.mtx.Unlock()
	}()

	select {
	case err := <-m.results:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startedOffsets returns the offsets of the flushes that started, in order.
func (m *gatedFlushCommitter) startedOffsets() []int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return slices.Clone(m.offsets)
}

// startedReasons returns the reasons of the flushes that started, in order.
func (m *gatedFlushCommitter) startedReasons() []string {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return slices.Clone(m.reasons)
}

// startedBuilders returns the builders of the flushes that started, in order.
func (m *gatedFlushCommitter) startedBuilders() [][]builder {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return slices.Clone(m.builders)
}

// maxConcurrentFlushes returns the largest number of flushes that ran at the
// same time.
func (m *gatedFlushCommitter) maxConcurrentFlushes() int {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.maxRunning
}

// testBuilderFactory creates real [logsobj.Builder] instances backed by an
// in-memory scratch store. All builders share a single, unregistered
// [logsobj.BuilderMetrics] instance, mirroring how the production factory
// shares metrics across the builders it creates.
type testBuilderFactory struct {
	metrics *logsobj.BuilderMetrics
	// created counts how many builders have been handed out. Tests use it to
	// assert that builders are reused per scope rather than recreated.
	created int
	// failAt, when non-negative, makes NewBuilder fail once created reaches
	// this value. A value of -1 (the default) never fails.
	failAt int
}

func newTestBuilderFactory() *testBuilderFactory {
	return &testBuilderFactory{metrics: logsobj.NewBuilderMetrics(), failAt: -1}
}

func (f *testBuilderFactory) NewBuilder() (*logsobj.Builder, error) {
	if f.failAt >= 0 && f.created >= f.failAt {
		return nil, errors.New("boom")
	}
	f.created++
	return logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), f.metrics, log.NewNopLogger(), nil)
}

// mockMultiBuilder wraps the production [MultiObjectBuilder] so processor
// tests can drive real builder behaviour while still being able to force the
// group to report itself as full.
type mockMultiBuilder struct {
	*MultiObjectBuilder
	forceFull bool
}

var _ multiBuilder = (*mockMultiBuilder)(nil)

func (m *mockMultiBuilder) IsFull() bool {
	return m.forceFull || m.MultiObjectBuilder.IsFull()
}

// newTestMultiBuilder returns a multiBuilder backed by real per-scope
// builders, suitable for driving the processor in tests.
func newTestMultiBuilder() *mockMultiBuilder {
	return &mockMultiBuilder{
		MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize)),
	}
}

// mockSorter returns the object it is given, so the flusher can be driven
// without performing a real sort.
type mockSorter struct {
	// closer is handed to the flusher on every sort, letting tests assert
	// whether ownership of the sorted object was transferred to the caller or
	// released by the flusher.
	closer countingCloser
}

func (m *mockSorter) Sort(_ context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error) {
	return obj, &m.closer, nil
}

type mockUploader struct {
	uploaded []*dataobj.Object
	mtx      sync.Mutex
}

func (m *mockUploader) Upload(_ context.Context, obj *dataobj.Object) (string, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.uploaded = append(m.uploaded, obj)
	return fmt.Sprintf("object_%03d", len(m.uploaded)), nil
}

// failureUploader is an uploader that always fails.
type failureUploader struct{}

func (f *failureUploader) Upload(_ context.Context, _ *dataobj.Object) (string, error) {
	return "", errors.New("mock error")
}
