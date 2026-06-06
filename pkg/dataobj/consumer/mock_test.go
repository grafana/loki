package consumer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// mockBuilder mocks a [logsobj.Builder].
type mockBuilder struct {
	builder *logsobj.Builder
	nextErr error
	// full, when true, forces IsFull to report the builder as full regardless
	// of the underlying builder's estimated size.
	full bool
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
	return m.builder.Flush()
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

type mockFlusher struct {
	flushes int
}

func (m *mockFlusher) Flush(_ context.Context, _ builder, _ string) (string, error) {
	m.flushes++
	return "", nil
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
	return logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), f.metrics)
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

// mockKafka mocks a [kgo.Client]. The zero value is usable.
type mockKafka struct {
	fetches  []kgo.Fetches
	produced []*kgo.Record

	// produceFailer is an (optional) callback executed in [Produce] that
	// can be used to fail producing certain records. If it is non-nil and
	// returns a non-nil error, the record will be failed, and the error
	// be passed to the promise.
	produceFailer func(r *kgo.Record) error

	// Internal, should not be accessed from tests.
	fetchesIdx int
	mtx        sync.Mutex
}

// PollFetches implements [kgo.Client.PollFetches].
func (m *mockKafka) PollFetches(_ context.Context) kgo.Fetches {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.fetchesIdx >= len(m.fetches) {
		return kgo.Fetches{}
	}
	fetches := m.fetches[m.fetchesIdx]
	m.fetchesIdx++
	return fetches
}

// Produce implements [kgo.Client.Produce].
func (m *mockKafka) Produce(
	_ context.Context,
	r *kgo.Record,
	promise func(*kgo.Record, error),
) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	var err error
	// Check if producing the record should fail.
	if m.produceFailer != nil {
		err = m.produceFailer(r)
	}
	if err != nil {
		promise(nil, err)
		return
	}
	m.produced = append(m.produced, r)
	promise(r, nil)
}

// ProduceSync implements [kgo.Client.ProduceSync].
func (m *mockKafka) ProduceSync(_ context.Context, rs ...*kgo.Record) kgo.ProduceResults {
	m.produced = append(m.produced, rs...)
	return kgo.ProduceResults{{Err: nil}}
}

type mockSorter struct{}

func (m *mockSorter) Sort(_ context.Context, obj *dataobj.Object) (*dataobj.Object, io.Closer, error) {
	return obj, io.NopCloser(nil), nil
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
