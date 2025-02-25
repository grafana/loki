package consumer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// mockBucket implements objstore.Bucket interface for testing
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
func (m *mockBucket) GetAndReplace(_ context.Context, name string, _ func(io.Reader) (io.Reader, error)) error {
	return m.Upload(context.Background(), name, bytes.NewReader([]byte{}))
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

var testBuilderConfig = dataobj.BuilderConfig{
	TargetPageSize:    2048,
	TargetObjectSize:  4096,
	TargetSectionSize: 4096,

	BufferSize: 2048 * 8,
}

// TestIdleFlush tests the idle flush behavior of the partition processor
// under different timeout and initialization conditions.
func TestIdleFlush(t *testing.T) {
	tests := []struct {
		name          string
		idleTimeout   time.Duration
		sleepDuration time.Duration
		expectFlush   bool
		initBuilder   bool
	}{
		{
			name:          "should not flush before idle timeout",
			idleTimeout:   1 * time.Second,
			sleepDuration: 100 * time.Millisecond,
			expectFlush:   false,
			initBuilder:   true,
		},
		{
			name:          "should flush after idle timeout",
			idleTimeout:   100 * time.Millisecond,
			sleepDuration: 1 * time.Second,
			expectFlush:   true,
			initBuilder:   true,
		},
		{
			name:          "should not flush if builder is nil",
			idleTimeout:   100 * time.Millisecond,
			sleepDuration: 100 * time.Millisecond,
			expectFlush:   false,
			initBuilder:   false,
		},
		{
			name:          "should not flush if last modified is less than idle timeout",
			idleTimeout:   1 * time.Second,
			sleepDuration: 100 * time.Millisecond,
			expectFlush:   false,
			initBuilder:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Setup test dependencies
			bucket := newMockBucket()
			bufPool := &sync.Pool{
				New: func() interface{} {
					return bytes.NewBuffer(make([]byte, 0, 1024))
				},
			}

			// Create processor with test configuration
			p := newPartitionProcessor(
				context.Background(),
				&kgo.Client{},
				testBuilderConfig,
				uploader.Config{},
				bucket,
				"test-tenant",
				0,
				"test-topic",
				0,
				log.NewNopLogger(),
				prometheus.NewRegistry(),
				bufPool,
				tc.idleTimeout,
			)

			if tc.initBuilder {
				require.NoError(t, p.initBuilder())
				p.start()
				defer p.stop()
			}

			// Record initial flush time
			initialFlushTime := p.lastFlush

			stream := logproto.Stream{
				Labels: `{cluster="test",app="foo"}`,
				Entries: []push.Entry{{
					Timestamp: time.Now().UTC(),
					Line:      strings.Repeat("a", 1024),
				}},
			}

			streamBytes, err := stream.Marshal()
			require.NoError(t, err)

			// Send a record to the processor
			p.records <- &kgo.Record{
				Value: streamBytes,
				Key:   []byte("test-tenant"),
			}

			// Wait for specified duration
			time.Sleep(tc.sleepDuration)

			// Trigger idle flush check
			p.idleFlush()

			if tc.expectFlush {
				require.True(t, p.lastFlush.After(initialFlushTime), "expected flush to occur")
			} else {
				require.Equal(t, initialFlushTime, p.lastFlush, "expected no flush to occur")
			}
		})
	}
}

// TestIdleFlushWithActiveProcessing tests the idle flush behavior
// while the processor is actively processing records.
func TestIdleFlushWithActiveProcessing(t *testing.T) {
	t.Parallel()
	// Setup test dependencies
	bucket := newMockBucket()
	bufPool := &sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}

	p := newPartitionProcessor(
		context.Background(),
		&kgo.Client{},
		testBuilderConfig,
		uploader.Config{},
		bucket,
		"test-tenant",
		0,
		"test-topic",
		0,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
		bufPool,
		200*time.Millisecond,
	)

	require.NoError(t, p.initBuilder())

	// Start the processor
	p.start()
	defer p.stop()

	stream := logproto.Stream{
		Labels: `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{
			Timestamp: time.Now().UTC(),
			Line:      strings.Repeat("a", 1024),
		}},
	}

	streamBytes, err := stream.Marshal()
	require.NoError(t, err)

	// Send a record to the processor
	p.records <- &kgo.Record{
		Value: streamBytes,
		Key:   []byte("test-tenant"),
	}

	// Record initial flush time
	initialFlushTime := p.lastFlush

	// Wait longer than idle timeout
	time.Sleep(300 * time.Millisecond)

	// Verify that idle flush occurred
	require.True(t, p.lastFlush.After(initialFlushTime), "expected idle flush to occur while processor is running")
}

// TestIdleFlushWithEmptyData tests the idle flush behavior
// when no data has been processed.
func TestIdleFlushWithEmptyData(t *testing.T) {
	t.Parallel()
	// Setup test dependencies
	bucket := newMockBucket()
	bufPool := &sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1024))
		},
	}

	p := newPartitionProcessor(
		context.Background(),
		&kgo.Client{},
		testBuilderConfig,
		uploader.Config{},
		bucket,
		"test-tenant",
		0,
		"test-topic",
		0,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
		bufPool,
		200*time.Millisecond,
	)

	require.NoError(t, p.initBuilder())

	// Start the processor
	p.start()
	defer p.stop()

	// Record initial flush time
	initialFlushTime := p.lastFlush

	// Wait longer than idle timeout
	time.Sleep(300 * time.Millisecond)

	// Verify that idle flush occurred
	require.True(t, p.lastFlush.Equal(initialFlushTime), "expected no idle flush with empty data")
}
