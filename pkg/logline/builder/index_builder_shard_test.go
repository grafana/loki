package builder

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func makeShardedConfig(t *testing.T, shardCount int) Config {
	t.Helper()
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
			ShardCount:       shardCount,
			ShardAlgorithm:   "first_byte",
		},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    t.TempDir(),
	}
	require.NoError(t, cfg.Validate())
	return cfg
}

func TestBuilder_Sharded_BucketCount(t *testing.T) {
	cfg := makeShardedConfig(t, 4)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now()
	stream := &logproto.Stream{
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "error: connection failed to database server"},
			{Timestamp: now, Line: "warning: retry attempt number one"},
			{Timestamp: now, Line: "info: successfully established connection"},
		},
	}
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	// With 4 shards, the merge produces 1-4 files for today's date, depending
	// on which shards the ngrams route to.
	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
	require.LessOrEqual(t, len(files), 4)
}

func TestBuilder_Sharded_UnshardedFallback(t *testing.T) {
	// shardCount=0 must produce exactly one bucket per date, keyed "date:0".
	cfg := makeShardedConfig(t, 0)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now()
	stream := &logproto.Stream{
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "error: connection failed"},
		},
	}
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, 0, files[0].shardValue, "unsharded builder should use shard 0")
}

func TestBuilder_Sharded_PrepareIndexes_ShardFields(t *testing.T) {
	cfg := makeShardedConfig(t, 4)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now()
	stream := &logproto.Stream{
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "error: connection failed to database server with timeout"},
		},
	}
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, f := range files {
		require.GreaterOrEqual(t, f.shardValue, 0)
		require.Less(t, f.shardValue, 4)
	}
}

func TestBuilder_Sharded_NgramRouting(t *testing.T) {
	// Verify shard count on all output files.
	cfg := makeShardedConfig(t, 2)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now()
	stream := &logproto.Stream{
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "AAAAAA BBBBBB error connection"},
		},
	}
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	// Each file should have a valid shard value.
	for _, f := range files {
		require.GreaterOrEqual(t, f.shardValue, 0)
		require.Less(t, f.shardValue, 2)
	}
}

func TestBuilder_Sharded_LabelValueRouting(t *testing.T) {
	cfg := makeShardedConfig(t, 2)
	cfg.Logline.IndexVersion = "v3"
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now().UTC()
	stream := &logproto.Stream{
		Labels: `{app="aaaaaa"}`,
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "x"},
		},
	}
	_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, 1, files[0].shardValue)

	reader, _, err := logline.OpenFile(files[0].file.Name())
	require.NoError(t, err)
	defer reader.Close()

	idx, err := reader.FindTerm("AAAAAA")
	require.NoError(t, err)
	require.GreaterOrEqual(t, idx, 0)
}

func TestBuilder_Sharded_PrepareProducesFiles(t *testing.T) {
	// Verify that prepareIndexes succeeds and produces valid, non-empty index files.
	cfg := makeShardedConfig(t, 4)
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	now := time.Now()
	for i := range 20 {
		stream := &logproto.Stream{
			Entries: []logproto.Entry{
				{Timestamp: now.Add(time.Duration(i) * time.Millisecond),
					Line: "error: request failed with status 500 at endpoint /api/v1/resource"},
			},
		}
		_ = b.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})
	}

	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)
}
