package tsdb

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestTSDBBuilder_PartitionRevocation(t *testing.T) {
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
	builder, err := NewTSDBBuilder(
		Config{
			TSDBStoragePrefix: "index/tsdb/",
		},
		kafka.Config{},
		log.NewLogfmtLogger(os.Stderr),
		"instance-id",
		bucket,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	builder.client.Close()
	builder.client = &mockKafkaClient{}

	// Start the service so the downloader goroutines are started.
	require.NoError(t, builder.StartAsync(ctx))
	require.NoError(t, builder.AwaitRunning(ctx))

	// Assign some partitions to the builder.
	builder.handlePartitionsAssigned(ctx, nil, map[string][]int32{
		"loki.metastore-events": {0, 1, 2},
	})

	revokedProcessed := make(chan struct{}, 1)
	trigger := make(chan struct{})
	go func() {
		<-trigger
		builder.handlePartitionsRevoked(ctx, nil, map[string][]int32{
			"loki.metastore-events": {1},
		})
		revokedProcessed <- struct{}{}
	}()

	// Trigger the revocation of a partition, but only after we've processed a couple of records.
	for i := range 10 {
		if i == 2 {
			trigger <- struct{}{}
		}
		builder.processRecord(ctx, &kgo.Record{
			Value:     eventBytes,
			Partition: int32(1),
		})
		if i < 2 {
			// After revocation is triggered, we can't guarantee that the revoked partition will be in the partition states map so stop checking it after that.
			require.True(t, builder.ownedPartitions[1])
		}
	}

	// Verify that the partition was revoked.
	<-revokedProcessed
	require.Equal(t, 2, len(builder.ownedPartitions))
	require.False(t, builder.ownedPartitions[1])
}

func TestTSDBBuilder(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Setup test dependencies
	bucket := objstore.NewInMemBucket()

	cluster, configString := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(t, 1, "loki.metastore-events")
	defer cluster.Close()

	client, err := kgo.NewClient(kgo.ConsumerGroup("test-consumer-group"), kgo.ConsumeTopics("loki.metastore-events"), kgo.SeedBrokers(configString))
	require.NoError(t, err)

	prefix := "index/tsdb/"
	p, err := NewTSDBBuilder(
		Config{
			TSDBStoragePrefix: prefix,
		},
		kafka.Config{}, log.NewNopLogger(), "instance-id", bucket, prometheus.NewRegistry(),
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
		// Hack: Change the node name so each object gets a different tsdb file.
		p.tsdbBuilder.(*dataobjTSDBBuilder).nodeName = fmt.Sprintf("instance-id-%d", i)

		event := metastore.ObjectWrittenEvent{
			ObjectPath: fmt.Sprintf("test-path-%d", i),
			WriteTime:  time.Now().Format(time.RFC3339),
		}
		eventBytes, err := event.Marshal()
		require.NoError(t, err)

		p.processRecord(context.Background(), &kgo.Record{
			Value:     eventBytes,
			Partition: int32(0),
		})
	}

	// There should be 2 files per object (a tsdb and section ref table)
	indexes := findAllObjects(t, bucket, prefix)
	require.Equal(t, 6, len(indexes))
}

func findAllObjects(t *testing.T, bucket objstore.Bucket, prefix string) []string {
	var out []string

	err := bucket.Iter(context.Background(), prefix, func(name string) error {
		out = append(out, name)
		return nil
	}, objstore.WithRecursiveIter())
	require.NoError(t, err)
	return out
}

// A mockKafkaClient implements the kafkaClient interface for tests.
type mockKafkaClient struct{}

func (m *mockKafkaClient) CommitRecords(_ context.Context, _ ...*kgo.Record) error {
	return nil
}

func (m *mockKafkaClient) PollRecords(_ context.Context, _ int) kgo.Fetches {
	return nil
}

func (m *mockKafkaClient) Close() {}

func buildLogObject(t *testing.T, app string, path string, bucket objstore.Bucket) {
	candidate, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          128 * 1024,
			TargetObjectSize:        4 * 1024 * 1024,
			TargetSectionSize:       2 * 1024 * 1024,
			BufferSize:              4 * 1024 * 1024,
			SectionStripeMergeLimit: 2,
		},
		DataobjSortOrder: "stream-asc",
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
