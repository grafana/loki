package consumer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// A mockBucket implements objstore.Bucket interface for tests.
type mockBucket struct {
	uploads map[string][]byte
	mu      sync.Mutex
}

func newMockBucket() *mockBucket {
	return &mockBucket{
		uploads: make(map[string][]byte),
	}
}

func (m *mockBucket) Close() error                             { return nil }
func (m *mockBucket) Delete(_ context.Context, _ string) error { return nil }
func (m *mockBucket) Exists(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.uploads[name]
	return exists, nil
}

func (m *mockBucket) Get(_ context.Context, name string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, exists := m.uploads[name]
	if !exists {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockBucket) GetRange(_ context.Context, _ string, _, _ int64) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockBucket) Upload(_ context.Context, name string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploads[name] = data
	return nil
}

func (m *mockBucket) Iter(_ context.Context, _ string, _ func(string) error, _ ...objstore.IterOption) error {
	return nil
}
func (m *mockBucket) Name() string { return "mock" }
func (m *mockBucket) Attributes(_ context.Context, _ string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, nil
}

func (m *mockBucket) GetAndReplace(_ context.Context, name string, _ func(io.ReadCloser) (io.ReadCloser, error)) error {
	return m.Upload(context.Background(), name, io.NopCloser(bytes.NewReader([]byte{})))
}

func (m *mockBucket) IsAccessDeniedErr(_ error) bool {
	return false
}

func (m *mockBucket) IsObjNotFoundErr(err error) bool {
	return err != nil && err.Error() == "object not found"
}

func (m *mockBucket) IterWithAttributes(_ context.Context, _ string, _ func(objstore.IterObjectAttributes) error, _ ...objstore.IterOption) error {
	return nil
}

func (m *mockBucket) Provider() objstore.ObjProvider {
	return objstore.ObjProvider("MOCK")
}

func (m *mockBucket) SupportedIterOptions() []objstore.IterOptionType {
	return nil
}

type mockBuilder struct {
	builder *logsobj.Builder
	nextErr error
}

func (m *mockBuilder) Append(stream logproto.Stream) error {
	if err := m.nextErr; err != nil {
		m.nextErr = nil
		return err
	}
	return m.builder.Append(stream)
}

func (m *mockBuilder) GetEstimatedSize() int {
	return m.builder.GetEstimatedSize()
}

func (m *mockBuilder) Flush() (*dataobj.Object, io.Closer, error) {
	if err := m.nextErr; err != nil {
		m.nextErr = nil
		return nil, nil, err
	}
	return m.builder.Flush()
}

func (m *mockBuilder) TimeRange() (time.Time, time.Time) {
	return m.builder.TimeRange()
}

func (m *mockBuilder) UnregisterMetrics(r prometheus.Registerer) {
	m.builder.UnregisterMetrics(r)
}

// A mockCommitter implements the committer interface for tests.
type mockCommitter struct {
	// We will need to change this when we add support for other methods like
	// CommitOffsets and CommitOffsetsSync.
	records []*kgo.Record
}

func (m *mockCommitter) CommitRecords(_ context.Context, records ...*kgo.Record) error {
	m.records = append(m.records, records...)
	return nil
}

// A mockProducer implements the producer interface for tests.
type mockProducer struct {
	results []*kgo.Record
}

func (m *mockProducer) ProduceSync(_ context.Context, records ...*kgo.Record) kgo.ProduceResults {
	m.results = append(m.results, records...)
	return kgo.ProduceResults{
		{
			Err: nil,
		},
	}
}

type recordedTocEntry struct {
	DataObjectPath string
	MinTimestamp   time.Time
	MaxTimestamp   time.Time
}

// A recordingTocWriter wraps a [metastore.TableOfContentsWriter] and records
// all entries written to it.
type recordingTocWriter struct {
	entries []recordedTocEntry
	*metastore.TableOfContentsWriter
}

func (m *recordingTocWriter) WriteEntry(ctx context.Context, dataobjPath string, timeRanges multitenancy.TimeRangeSet) error {
	for _, timeRange := range timeRanges {
		m.entries = append(m.entries, recordedTocEntry{
			DataObjectPath: dataobjPath,
			MinTimestamp:   timeRange.MinTime,
			MaxTimestamp:   timeRange.MaxTime,
		})
	}
	return m.TableOfContentsWriter.WriteEntry(ctx, dataobjPath, timeRanges)
}
