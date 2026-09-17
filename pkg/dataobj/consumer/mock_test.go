package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// countingCloser counts how many times it was closed. When inner is set it is
// closed too, so a closer substituted by a mock still releases the real
// resource behind it.
type countingCloser struct {
	closed int
	err    error
	inner  io.Closer
}

func (c *countingCloser) Close() error {
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

func (m *mockBuilder) TimeRanges() []multitenancy.TimeRange {
	return m.builder.TimeRanges()
}

// A mockCommitter implements the committer interface for tests.
type mockCommitter struct {
	offsets []int64
}

func (m *mockCommitter) Commit(_ context.Context, _ int32, offset int64) error {
	m.offsets = append(m.offsets, offset)
	return nil
}

// mockFlusher implements the flusher interface, handing out a distinct object
// path per flush.
type mockFlusher struct {
	flushes int
	// obj is returned by every flush, letting tests assert that the flushed
	// object reaches the caller.
	obj *dataobj.Object
	// closer is returned by every flush, letting tests assert that the caller
	// releases the flushed object once it is done with it.
	closer countingCloser
}

func (m *mockFlusher) Flush(_ context.Context, _ builder, _ string) (*dataobj.Object, io.Closer, string, error) {
	m.flushes++
	return m.obj, &m.closer, fmt.Sprintf("object_%03d", m.flushes), nil
}

// mockIndexer implements the indexer interface, recording what it was asked to
// index.
type mockIndexer struct {
	objs  []*dataobj.Object
	paths []string
	// errs is consumed one entry per call so tests can drive retries. Once it
	// is exhausted, indexing succeeds.
	errs []error
}

func (m *mockIndexer) Index(_ context.Context, obj *dataobj.Object, objPath string) error {
	m.objs = append(m.objs, obj)
	m.paths = append(m.paths, objPath)
	if len(m.errs) == 0 {
		return nil
	}
	err := m.errs[0]
	m.errs = m.errs[1:]
	return err
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

// testBuilderFactory creates real [logsobj.Builder] instances backed by an
// in-memory scratch store. All builders share a single, unregistered
// [logsobj.BuilderMetrics] instance, mirroring how the production factory
// shares metrics across the builders it creates.
type testBuilderFactory struct {
	metrics *logsobj.BuilderMetrics
	// created counts how many builders have been handed out. Tests use it to
	// assert that builders are reused per window rather than recreated.
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

// mockMultiBuilder wraps the production [TOCAlignedMultiBuilder] so processor
// tests can drive real builder behaviour while still being able to force the
// group to report itself as full.
type mockMultiBuilder struct {
	*TOCAlignedMultiBuilder
	forceFull bool
}

var _ multiBuilder = (*mockMultiBuilder)(nil)

func (m *mockMultiBuilder) IsFull() bool {
	return m.forceFull || m.TOCAlignedMultiBuilder.IsFull()
}

// newTestMultiBuilder returns a multiBuilder backed by real per-window
// builders, suitable for driving the processor in tests.
func newTestMultiBuilder() *mockMultiBuilder {
	return &mockMultiBuilder{
		TOCAlignedMultiBuilder: NewTOCAlignedMultiBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize)),
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
