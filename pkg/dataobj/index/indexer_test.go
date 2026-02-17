package index

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
)

func TestSerialIndexer_BuildIndex(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set up test data
	bucket := objstore.NewInMemBucket()
	buildLogObject(t, "loki", "test-path-0", bucket)

	event := metastore.ObjectWrittenEvent{
		ObjectPath: "test-path-0",
		WriteTime:  time.Now().Format(time.RFC3339),
	}

	record := &kgo.Record{
		Value:     nil, // Will be set below
		Partition: int32(0),
	}
	eventBytes, err := event.Marshal()
	require.NoError(t, err)
	record.Value = eventBytes

	bufferedEvt := bufferedEvent{
		event:  event,
		record: record,
	}

	// Create indexer with mock calculator
	mockCalc := &mockCalculator{}
	indexStorageBucket := objstore.NewInMemBucket()

	// Create dedicated registry for this test
	reg := prometheus.NewRegistry()

	builderMetrics := newBuilderMetrics()
	require.NoError(t, builderMetrics.register(reg))

	indexerMetrics := newIndexerMetrics()
	require.NoError(t, indexerMetrics.register(reg))

	indexer := newSerialIndexer(
		mockCalc,
		bucket,
		indexStorageBucket,
		builderMetrics,
		indexerMetrics,
		log.NewLogfmtLogger(os.Stderr),
		indexerConfig{QueueSize: 10},
	)

	// Start indexer service
	require.NoError(t, indexer.StartAsync(ctx))
	require.NoError(t, indexer.AwaitRunning(ctx))
	defer func() {
		indexer.StopAsync()
		require.NoError(t, indexer.AwaitTerminated(context.Background()))
	}()

	// Submit build request
	records, err := indexer.submitBuild(ctx, []bufferedEvent{bufferedEvt}, 0, triggerTypeAppend)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, record, records[0])

	// Verify calculator was used
	require.Equal(t, 1, mockCalc.count)
	require.NotNil(t, mockCalc.object)

	// Verify Prometheus metrics
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalRequests))
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalBuilds))
}

func TestSerialIndexer_MultipleBuilds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Set up test data
	bucket := objstore.NewInMemBucket()
	buildLogObject(t, "loki", "test-path-0", bucket)
	buildLogObject(t, "testing", "test-path-1", bucket)

	events := []bufferedEvent{}
	for i := range 2 {
		event := metastore.ObjectWrittenEvent{
			ObjectPath: fmt.Sprintf("test-path-%d", i),
			WriteTime:  time.Now().Format(time.RFC3339),
		}

		record := &kgo.Record{
			Partition: int32(0),
		}
		eventBytes, err := event.Marshal()
		require.NoError(t, err)
		record.Value = eventBytes

		events = append(events, bufferedEvent{
			event:  event,
			record: record,
		})
	}

	// Create indexer with mock calculator
	mockCalc := &mockCalculator{}
	indexStorageBucket := objstore.NewInMemBucket()

	// Create dedicated registry for this test
	reg := prometheus.NewRegistry()

	builderMetrics := newBuilderMetrics()
	require.NoError(t, builderMetrics.register(reg))

	indexerMetrics := newIndexerMetrics()
	require.NoError(t, indexerMetrics.register(reg))

	indexer := newSerialIndexer(
		mockCalc,
		bucket,
		indexStorageBucket,
		builderMetrics,
		indexerMetrics,
		log.NewLogfmtLogger(os.Stderr),
		indexerConfig{QueueSize: 10},
	)

	// Start indexer service
	require.NoError(t, indexer.StartAsync(ctx))
	require.NoError(t, indexer.AwaitRunning(ctx))
	defer func() {
		indexer.StopAsync()
		require.NoError(t, indexer.AwaitTerminated(context.Background()))
	}()

	// Submit build request with multiple events
	records, err := indexer.submitBuild(ctx, events, 0, triggerTypeAppend)
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Verify calculator processed all events
	require.Equal(t, 2, mockCalc.count)
	require.NotNil(t, mockCalc.object)

	// Verify Prometheus metrics - multiple events in single request/build
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalRequests))
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalBuilds))
}

func TestSerialIndexer_ServiceNotRunning(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create indexer without starting it
	mockCalc := &mockCalculator{}
	bucket := objstore.NewInMemBucket()
	indexStorageBucket := objstore.NewInMemBucket()
	builderMetrics := newBuilderMetrics()
	require.NoError(t, builderMetrics.register(prometheus.NewRegistry()))

	indexerMetrics := newIndexerMetrics()
	require.NoError(t, indexerMetrics.register(prometheus.NewRegistry()))

	indexer := newSerialIndexer(
		mockCalc,
		bucket,
		indexStorageBucket,
		builderMetrics,
		indexerMetrics,
		log.NewNopLogger(),
		indexerConfig{QueueSize: 10},
	)

	// Try to submit build without starting service
	event := metastore.ObjectWrittenEvent{
		ObjectPath: "test-path-0",
		WriteTime:  time.Now().Format(time.RFC3339),
	}
	record := &kgo.Record{Partition: int32(0)}
	bufferedEvt := bufferedEvent{event: event, record: record}

	_, err := indexer.submitBuild(ctx, []bufferedEvent{bufferedEvt}, 0, triggerTypeAppend)
	require.Error(t, err)
	require.Contains(t, err.Error(), "indexer service is not running")
}

func TestSerialIndexer_ConcurrentBuilds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Set up test data
	bucket := objstore.NewInMemBucket()
	numTestObjects := 5
	for i := 0; i < numTestObjects; i++ {
		buildLogObject(t, fmt.Sprintf("app-%d", i), fmt.Sprintf("test-path-%d", i), bucket)
	}

	// Create indexer with mock calculator
	mockCalc := &mockCalculator{}
	indexStorageBucket := objstore.NewInMemBucket()

	// Create dedicated registry for this test
	reg := prometheus.NewRegistry()

	builderMetrics := newBuilderMetrics()
	require.NoError(t, builderMetrics.register(reg))

	indexerMetrics := newIndexerMetrics()
	require.NoError(t, indexerMetrics.register(reg))

	indexer := newSerialIndexer(
		mockCalc,
		bucket,
		indexStorageBucket,
		builderMetrics,
		indexerMetrics,
		log.NewLogfmtLogger(os.Stderr),
		indexerConfig{QueueSize: 10},
	)

	// Start indexer service
	require.NoError(t, indexer.StartAsync(ctx))
	require.NoError(t, indexer.AwaitRunning(ctx))
	defer func() {
		indexer.StopAsync()
		require.NoError(t, indexer.AwaitTerminated(context.Background()))
	}()

	// Submit multiple concurrent build requests
	numRequests := 200
	results := make(chan error, numRequests)

	cancelCtx, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			event := metastore.ObjectWrittenEvent{
				ObjectPath: fmt.Sprintf("test-path-%d", idx%numTestObjects),
				WriteTime:  time.Now().Format(time.RFC3339),
			}

			record := &kgo.Record{Partition: int32(0)}
			eventBytes, err := event.Marshal()
			if err != nil {
				results <- err
				return
			}
			record.Value = eventBytes

			bufferedEvt := bufferedEvent{event: event, record: record}

			buildCtx := ctx
			if i == numRequests/2 {
				buildCtx = cancelCtx
			}
			_, err = indexer.submitBuild(buildCtx, []bufferedEvent{bufferedEvt}, int32(0), triggerTypeAppend)
			results <- err
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		err := <-results
		require.NoError(t, err)
	}

	// Verify all events were processed (serialized)
	require.Equal(t, numRequests, mockCalc.count)

	// Verify Prometheus metrics - multiple concurrent requests
	require.Equal(t, float64(numRequests), testutil.ToFloat64(indexerMetrics.totalRequests))
	require.Equal(t, float64(numRequests), testutil.ToFloat64(indexerMetrics.totalBuilds))
	require.Greater(t, testutil.ToFloat64(indexerMetrics.buildTimeSeconds), float64(0))
	require.Equal(t, float64(0), testutil.ToFloat64(indexerMetrics.queueDepth))
}

// mockCalculator is a calculator that does nothing for use in tests
type mockCalculator struct {
	count           int
	object          *dataobj.Object
	flushCallCount  int
	resetCallCount  int
	errOnCallNumber int  // Which Calculate call should set full flag (0 = never)
	full            bool // Track when builder becomes full
}

func (c *mockCalculator) Calculate(_ context.Context, _ log.Logger, object *dataobj.Object, _ string) error {
	c.count++
	c.object = object

	if c.errOnCallNumber > 0 && c.count == c.errOnCallNumber {
		c.full = true
	}
	return nil
}

func (c *mockCalculator) Flush() (*dataobj.Object, io.Closer, error) {
	c.flushCallCount++
	return c.object, io.NopCloser(bytes.NewReader([]byte("test-data"))), nil
}

func (c *mockCalculator) TimeRanges() []multitenancy.TimeRange {
	return []multitenancy.TimeRange{
		{
			Tenant:  "test",
			MinTime: time.Now(),
			MaxTime: time.Now().Add(time.Hour),
		},
	}
}

func (c *mockCalculator) Reset() {
	c.resetCallCount++
	c.full = false
}

func (c *mockCalculator) IsFull() bool {
	return c.full
}

func TestSerialIndexer_FlushOnBuilderFull(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Set up test data - 3 events to process
	bucket := objstore.NewInMemBucket()
	for i := 0; i < 3; i++ {
		buildLogObject(t, fmt.Sprintf("tenant-%d", i), fmt.Sprintf("test-path-%d", i), bucket)
	}

	events := []bufferedEvent{}
	for i := 0; i < 3; i++ {
		event := metastore.ObjectWrittenEvent{
			ObjectPath: fmt.Sprintf("test-path-%d", i),
			WriteTime:  time.Now().Format(time.RFC3339),
		}

		record := &kgo.Record{Partition: int32(0)}
		eventBytes, err := event.Marshal()
		require.NoError(t, err)
		record.Value = eventBytes

		events = append(events, bufferedEvent{
			event:  event,
			record: record,
		})
	}

	// Create mock calculator that sets full flag on second call
	mockCalc := &mockCalculator{
		errOnCallNumber: 2, // Set full flag on the 2nd Calculate call
	}
	indexStorageBucket := objstore.NewInMemBucket()

	// Create dedicated registry for this test
	reg := prometheus.NewRegistry()

	builderMetrics := newBuilderMetrics()
	require.NoError(t, builderMetrics.register(reg))

	indexerMetrics := newIndexerMetrics()
	require.NoError(t, indexerMetrics.register(reg))

	indexer := newSerialIndexer(
		mockCalc,
		bucket,
		indexStorageBucket,
		builderMetrics,
		indexerMetrics,
		log.NewLogfmtLogger(os.Stderr),
		indexerConfig{QueueSize: 10},
	)

	// Start indexer service
	require.NoError(t, indexer.StartAsync(ctx))
	require.NoError(t, indexer.AwaitRunning(ctx))
	defer func() {
		indexer.StopAsync()
		require.NoError(t, indexer.AwaitTerminated(context.Background()))
	}()

	// Submit build request with multiple events
	records, err := indexer.submitBuild(ctx, events, 0, triggerTypeAppend)
	require.NoError(t, err)
	require.Len(t, records, 2) // All records should be returned

	// Verify calculator behavior
	require.Equal(t, 2, mockCalc.count)          // 2 calls (no retries)
	require.Equal(t, 1, mockCalc.flushCallCount) // 1 flush after full only
	require.Equal(t, 1, mockCalc.resetCallCount) // 1 reset after full

	// Verify metrics - single request/build despite multiple flushes
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalRequests))
	require.Equal(t, float64(1), testutil.ToFloat64(indexerMetrics.totalBuilds))
}

func TestDownloadObject_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bucket := objstore.NewInMemBucket()
	testData := []byte("test data content for download")
	objectPath := "test-object"

	// Upload test object
	require.NoError(t, bucket.Upload(ctx, objectPath, bytes.NewReader(testData)))

	// Download with pre-allocation
	result, err := downloadObject(ctx, bucket, objectPath)
	require.NoError(t, err)
	require.Equal(t, testData, result)
}

func TestDownloadObject_ObjectNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	bucket := objstore.NewInMemBucket()
	objectPath := "non-existent-object"

	// Try to download non-existent object
	_, err := downloadObject(ctx, bucket, objectPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to fetch object from storage")
}
