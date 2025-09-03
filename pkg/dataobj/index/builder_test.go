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
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = indexobj.BuilderConfig{
	TargetPageSize:    128 * 1024,
	TargetObjectSize:  4 * 1024 * 1024,
	TargetSectionSize: 2 * 1024 * 1024,

	BufferSize: 4 * 1024 * 1024,

	SectionStripeMergeLimit: 2,
}

func buildLogObject(t *testing.T, app string, path string, bucket objstore.Bucket) {
	candidate, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:    128 * 1024,
		TargetObjectSize:  4 * 1024 * 1024,
		TargetSectionSize: 2 * 1024 * 1024,

		BufferSize:              4 * 1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		stream := logproto.Stream{
			Labels:  fmt.Sprintf("{app=\"%s\",stream=\"%d\"}", app, i),
			Entries: []logproto.Entry{{Timestamp: time.Now(), Line: fmt.Sprintf("line %d", i)}},
		}
		err = candidate.Append("tenant", stream)
		require.NoError(t, err)
	}

	obj, closer, err := candidate.Flush()
	require.NoError(t, err)
	defer closer.Close()

	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer reader.Close()

	err = bucket.Upload(t.Context(), path, reader)
	require.NoError(t, err)
}

func TestIndexBuilder_PartitionRevocation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Set up some test data
	bucket := objstore.NewInMemBucket()
	buildLogObject(t, "loki", "test-path-0", bucket)
	event := metastore.ObjectWrittenEvent{
		ObjectPath: "test-path-0",
		WriteTime:  time.Now().Format(time.RFC3339),
	}
	eventBytes, err := event.Marshal()
	require.NoError(t, err)

	// Create a builder with mocks for dependencies
	builder, err := NewIndexBuilder(
		Config{
			BuilderConfig:  testBuilderConfig,
			EventsPerIndex: 1,
		},
		metastore.Config{},
		kafka.Config{},
		log.NewLogfmtLogger(os.Stderr),
		"instance-id",
		bucket,
		nil,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	builder.calculator = &mockCalculator{}
	builder.ctx = ctx
	builder.client.Close()
	builder.client = &mockKafkaClient{}

	// Start the service so the downloader goroutines are started.
	require.NoError(t, builder.StartAsync(ctx))
	require.NoError(t, builder.AwaitRunning(ctx))

	// Assign some partitions to the builder.
	builder.handlePartitionsAssigned(ctx, nil, map[string][]int32{
		"loki.metastore-events": {0, 1, 2},
	})

	trigger := make(chan struct{})
	go func() {
		<-trigger
		builder.handlePartitionsRevoked(ctx, nil, map[string][]int32{
			"loki.metastore-events": {1},
		})
	}()

	// Trigger the revocation of a partition, but only after we've processed a couple of records.
	for i := range 10 {
		if i == 2 {
			trigger <- struct{}{}
		}
		builder.processRecord(&kgo.Record{
			Value:     eventBytes,
			Partition: int32(1),
		})
		if i < 2 {
			// After revocation is triggered, we can't guarantee that the partition will be in the buffered events map.
			require.NotNil(t, builder.bufferedEvents[1])
			require.Len(t, builder.bufferedEvents[1], 0)
		}
	}
	// Verify that the first records were processed successfully.
	require.GreaterOrEqual(t, builder.calculator.(*mockCalculator).count, 2)
	require.NotNil(t, builder.calculator.(*mockCalculator).object)

	// Verify that the partition was revoked.
	require.Equal(t, 2, len(builder.bufferedEvents))
	require.Nil(t, builder.bufferedEvents[1])
}

func TestIndexBuilder(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Setup test dependencies
	bucket := objstore.NewInMemBucket()

	cluster, configString := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(t, 1, "loki.metastore-events")
	defer cluster.Close()

	client, err := kgo.NewClient(kgo.ConsumerGroup("test-consumer-group"), kgo.ConsumeTopics("loki.metastore-events"), kgo.SeedBrokers(configString))
	require.NoError(t, err)

	p, err := NewIndexBuilder(
		Config{
			BuilderConfig:  testBuilderConfig,
			EventsPerIndex: 3,
		},
		metastore.Config{},
		kafka.Config{},
		log.NewNopLogger(),
		"instance-id",
		bucket,
		nil,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	p.client.Close()
	p.client = client
	require.NoError(t, p.StartAsync(ctx))
	require.NoError(t, p.AwaitRunning(ctx))

	// Assign some partitions to the builder.
	p.handlePartitionsAssigned(ctx, nil, map[string][]int32{
		"loki.metastore-events": {0},
	})

	buildLogObject(t, "loki", "test-path-0", bucket)
	buildLogObject(t, "testing", "test-path-1", bucket)
	buildLogObject(t, "three", "test-path-2", bucket)

	for i := 0; i < 3; i++ {
		event := metastore.ObjectWrittenEvent{
			ObjectPath: fmt.Sprintf("test-path-%d", i),
			WriteTime:  time.Now().Format(time.RFC3339),
		}
		eventBytes, err := event.Marshal()
		require.NoError(t, err)

		p.processRecord(&kgo.Record{
			Value:     eventBytes,
			Partition: int32(0),
		})
	}

	indexes := readAllSectionPointers(t, bucket)
	require.Equal(t, 30, len(indexes))
}

func readAllSectionPointers(t *testing.T, bucket objstore.Bucket) []pointers.SectionPointer {
	var out []pointers.SectionPointer

	err := bucket.Iter(context.Background(), "indexes/", func(name string) error {
		objReader, err := bucket.Get(context.Background(), name)
		require.NoError(t, err)
		defer objReader.Close()

		objectBytes, err := io.ReadAll(objReader)
		require.NoError(t, err)

		object, err := dataobj.FromReaderAt(bytes.NewReader(objectBytes), int64(len(objectBytes)))
		require.NoError(t, err)

		var reader pointers.RowReader
		defer reader.Close()

		buf := make([]pointers.SectionPointer, 64)

		for _, section := range object.Sections() {
			if !pointers.CheckSection(section) {
				continue
			}

			sec, err := pointers.Open(context.Background(), section)
			if err != nil {
				return fmt.Errorf("opening section: %w", err)
			}

			reader.Reset(sec)
			for {
				num, err := reader.Read(context.Background(), buf)
				if err != nil && err != io.EOF {
					return fmt.Errorf("reading section: %w", err)
				}
				if num == 0 && err == io.EOF {
					break
				}
				out = append(out, buf[:num]...)
			}
		}
		return nil
	}, objstore.WithRecursiveIter())
	require.NoError(t, err)

	return out
}

// mockCalculator is a calculator that does nothing for use in tests
type mockCalculator struct {
	count  int
	object *dataobj.Object
}

func (c *mockCalculator) Calculate(_ context.Context, _ log.Logger, object *dataobj.Object, _ string) error {
	c.count++
	c.object = object
	return nil
}

func (c *mockCalculator) Flush() (*dataobj.Object, io.Closer, error) {
	return c.object, io.NopCloser(bytes.NewReader([]byte{})), nil
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

func (c *mockCalculator) Reset() {}

// A mockKafkaClient implements the kafkaClient interface for tests.
type mockKafkaClient struct{}

func (m *mockKafkaClient) CommitRecords(_ context.Context, _ ...*kgo.Record) error {
	return nil
}

func (m *mockKafkaClient) PollRecords(_ context.Context, _ int) kgo.Fetches {
	return nil
}

func (m *mockKafkaClient) Close() {}
