package index

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		stream := logproto.Stream{
			Labels:  fmt.Sprintf("{app=\"%s\",stream=\"%d\"}", app, i),
			Entries: []logproto.Entry{{Timestamp: time.Now(), Line: fmt.Sprintf("line %d", i)}},
		}
		err = candidate.Append(stream)
		require.NoError(t, err)
	}

	buf := bytes.NewBuffer(nil)
	_, err = candidate.Flush(buf)
	require.NoError(t, err)

	err = bucket.Upload(context.Background(), path, buf)
	require.NoError(t, err)
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

	indexPrefix := "test-prefix"
	tenant := "test-tenant"
	p, err := NewIndexBuilder(
		Config{
			BuilderConfig: indexobj.BuilderConfig{
				TargetPageSize:    128 * 1024,
				TargetObjectSize:  4 * 1024 * 1024,
				TargetSectionSize: 2 * 1024 * 1024,

				BufferSize:              4 * 1024 * 1024,
				SectionStripeMergeLimit: 2,
			},
			EventsPerIndex:     3,
			IndexStoragePrefix: indexPrefix,
			EnabledTenantIDs:   []string{tenant},
		},
		kafka.Config{},
		metastore.UpdaterConfig{},
		log.NewNopLogger(),
		"instance-id",
		bucket,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)
	p.client = client
	require.NoError(t, p.StartAsync(ctx))

	buildLogObject(t, "loki", "test-path-0", bucket)
	buildLogObject(t, "testing", "test-path-1", bucket)
	buildLogObject(t, "three", "test-path-2", bucket)

	for i := 0; i < 3; i++ {
		event := metastore.ObjectWrittenEvent{
			ObjectPath: fmt.Sprintf("test-path-%d", i),
			Tenant:     tenant,
			WriteTime:  time.Now().Format(time.RFC3339),
		}
		eventBytes, err := event.Marshal()
		require.NoError(t, err)

		p.processRecord(&kgo.Record{
			Key:   []byte(tenant),
			Value: eventBytes,
		})
	}

	indexes := readAllSectionPointers(t, bucket, indexPrefix)
	require.Equal(t, 30, len(indexes))
}

func readAllSectionPointers(t *testing.T, bucket objstore.Bucket, indexPrefix string) []pointers.SectionPointer {
	var out []pointers.SectionPointer

	directories := []string{}
	err := bucket.Iter(context.Background(), fmt.Sprintf("%s/tenant-test-tenant/indexes/", indexPrefix), func(name string) error {
		directories = append(directories, name)
		return nil
	})
	require.NoError(t, err)

	for _, directory := range directories {
		err := bucket.Iter(context.Background(), directory, func(name string) error {

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
		})
		require.NoError(t, err)
	}
	require.NoError(t, err)

	return out
}
