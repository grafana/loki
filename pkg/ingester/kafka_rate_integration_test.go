package ingester

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type rateRecordingPusher struct {
	calculator *StreamRateCalculator
	mu         sync.Mutex
	entries    []logproto.Entry
}

func (p *rateRecordingPusher) PushWithTimestamp(ctx context.Context, push TimestampedPush) (*logproto.PushResponse, error) {
	for _, stream := range push.Streams {
		bytes := 0
		for _, e := range stream.Entries {
			bytes += util.EntryTotalSize(&e)
		}
		p.calculator.Record(ctx, "tenant", xxhash.Sum64String(stream.Labels), 7, bytes)
		p.mu.Lock()
		for _, e := range stream.Entries {
			clonedEntry := e
			clonedEntry.StructuredMetadata = append([]logproto.LabelAdapter(nil), e.StructuredMetadata...)
			p.entries = append(p.entries, clonedEntry)
		}
		p.mu.Unlock()
	}
	return &logproto.PushResponse{}, nil
}

// Exercise real Kafka fetching, both wire formats, parallel consumers, and snapshot serialization.
func TestKafkaRateSnapshotPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := newManualKafkaStreamRateCalculator()
	pusher := &rateRecordingPusher{calculator: c}
	producer := startKafkaRatePipeline(ctx, t, c, pusher)
	records, expectedEntries := mixedKafkaRateRecords(t, 1)
	require.NoError(t, producer.ProduceSync(ctx, records...).FirstErr())
	last := records[len(records)-1].Offset
	require.Eventually(t, func() bool {
		state := c.kafka
		state.mu.Lock()
		defer state.mu.Unlock()
		return state.completed == last && state.watermarkKnown
	}, 5*time.Second, time.Millisecond)
	c.updateRates()
	response := c.buildRatesResponse()
	require.True(t, response.Kafka.Ready)
	require.True(t, response.Kafka.ProgressValid)
	require.Equal(t, last, response.Kafka.EndOffset)
	require.GreaterOrEqual(t, response.Kafka.HighWatermark, last)

	pusher.mu.Lock()
	entries := append([]logproto.Entry(nil), pusher.entries...)
	pusher.mu.Unlock()
	require.Len(t, entries, len(expectedEntries))
	for _, list := range [][]logproto.Entry{entries, expectedEntries} {
		sort.Slice(list, func(i, j int) bool { return list[i].Line < list[j].Line })
	}
	for i, expected := range expectedEntries {
		require.Equal(t, expected.Timestamp, entries[i].Timestamp)
		require.Equal(t, expected.Line, entries[i].Line)
		require.ElementsMatch(t, expected.StructuredMetadata, entries[i].StructuredMetadata)
	}
	var expectedBytes int64
	for _, entry := range expectedEntries {
		expectedBytes += int64(util.EntryTotalSize(&entry))
	}
	requireStreamRateTotals(t, response, expectedBytes, uint32(len(records)))

	encoded, err := response.Marshal()
	require.NoError(t, err)
	decoded := &logproto.StreamRatesResponse{}
	require.NoError(t, decoded.Unmarshal(encoded))
	require.Equal(t, response, decoded)
	// The record offsets, not fetched batch maxima, define the completed prefix.
	require.EqualValues(t, len(records)-1, response.Kafka.EndOffset)
}

type failingRatePusher struct {
	*rateRecordingPusher
	failure func() error
}

func (p failingRatePusher) PushWithTimestamp(ctx context.Context, push TimestampedPush) (*logproto.PushResponse, error) {
	response, err := p.rateRecordingPusher.PushWithTimestamp(ctx, push)
	if push.Streams[0].Entries[0].Line == "reject" {
		return response, p.failure()
	}
	return response, err
}

func TestKafkaRateConsumerTerminalOutcomes(t *testing.T) {
	terminalErr := fmt.Errorf("terminal rejection")
	for _, tc := range []struct {
		name              string
		attemptErrors     []error
		cancelRetry       bool
		undecodable       bool
		wantProgressValid bool
		wantCommit        int64
		wantPushes        uint32
	}{
		{name: "success", attemptErrors: []error{nil}, wantProgressValid: true, wantCommit: 3, wantPushes: 3},
		{name: "terminal rejection", attemptErrors: []error{terminalErr}, wantProgressValid: true, wantCommit: 1, wantPushes: 3},
		{name: "undecodable record", undecodable: true, wantCommit: 1, wantPushes: 2},
		{name: "retry succeeds", attemptErrors: []error{ErrReadOnly, nil}, wantCommit: 3, wantPushes: 4},
		{name: "retry ends with terminal rejection", attemptErrors: []error{ErrReadOnly, terminalErr}, wantCommit: 1, wantPushes: 4},
		{name: "retry cancelled", attemptErrors: []error{ErrReadOnly}, cancelRetry: true, wantCommit: 1, wantPushes: 3},
	} {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("%s/workers=%d", tc.name, workers), func(t *testing.T) {
				c := newReadyKafkaStreamRateCalculator(4)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				attempts := 0
				pusher := failingRatePusher{
					rateRecordingPusher: &rateRecordingPusher{calculator: c},
					failure: func() error {
						if tc.cancelRetry {
							cancel()
						}
						err := tc.attemptErrors[min(attempts, len(tc.attemptErrors)-1)]
						attempts++
						return err
					},
				}
				committer := &recordingCommitter{}
				consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), workers, c)(committer, log.NewNopLogger())
				require.NoError(t, err)
				records := make([]partition.Record, 3)
				for i, line := range []string{"first", "reject", "last"} {
					stream := logproto.Stream{Labels: fmt.Sprintf(`{stream="%d"}`, i), Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}}}
					encoded, err := kafka.Encode(0, "tenant", stream, 1<<20)
					require.NoError(t, err)
					records[i] = partition.Record{Ctx: context.Background(), TenantID: "tenant", Offset: int64(i + 1), Content: encoded[0].Value}
				}
				if tc.undecodable {
					records[1].Content = []byte{0xff}
				}
				consumeKafkaRateBatch(ctx, consumer, records)
				c.updateRates()
				response := c.buildRatesResponse()
				require.EqualValues(t, 3, response.Kafka.EndOffset, "all records must complete, including after cancellation")
				require.Equal(t, []int64{tc.wantCommit}, committer.offsets, "rate tracking must not change acknowledgement")
				require.Equal(t, tc.wantProgressValid, response.Kafka.ProgressValid)
				require.Equal(t, len(tc.attemptErrors), attempts)
				expectedBytes := int64(len("first") + len("last") + attempts*len("reject"))
				requireStreamRateTotals(t, response, expectedBytes, tc.wantPushes)
				c.updateRates()
				require.True(t, c.buildRatesResponse().Kafka.ProgressValid, "failures only invalidate their own sample")
				require.Empty(t, c.buildRatesResponse().StreamRates, "attempts must not be counted twice")

				// Every outcome must allow progress in the next batch.
				next := records[0]
				next.Offset = 4
				c.setKafkaHighWatermark(5)
				consumeKafkaRateBatch(context.Background(), consumer, []partition.Record{next})
				c.updateRates()
				response = c.buildRatesResponse()
				require.True(t, response.Kafka.ProgressValid)
				require.EqualValues(t, 3, response.Kafka.StartOffset)
				require.EqualValues(t, 4, response.Kafka.EndOffset)
				require.Equal(t, []int64{tc.wantCommit, 4}, committer.offsets)

			})
		}
	}
}

// Compare WAL-backed direct ingestion with Kafka ingestion and rate tracking.
func TestKafkaRateTrackingPreservesIngestion(t *testing.T) {
	encoded, expectedEntries := mixedKafkaRateRecords(t, 12)
	records := make([]partition.Record, len(encoded))
	for i, record := range encoded {
		records[i] = partition.Record{Ctx: context.Background(), TenantID: tenantID, Content: record.Value, Offset: int64(i + 1)}
	}
	var expectedBytes int64
	for _, entry := range expectedEntries {
		expectedBytes += int64(util.EntryTotalSize(&entry))
	}

	baseline, baselineChunks := runKafkaRateIngestion(t, records, false)
	tracked, trackedChunks := runKafkaRateIngestion(t, records, true)
	for _, response := range []*logproto.StreamRatesResponse{baseline, tracked} {
		requireStreamRateTotals(t, response, expectedBytes, uint32(len(records)))
	}
	require.Nil(t, baseline.Kafka)
	require.True(t, tracked.Kafka.Ready)
	require.True(t, tracked.Kafka.ProgressValid)
	require.EqualValues(t, len(records), tracked.Kafka.EndOffset)
	require.Len(t, baselineChunks, 2)
	for _, sums := range baselineChunks {
		require.Greater(t, len(sums), 1)
	}
	require.Equal(t, baselineChunks, trackedChunks)
}

func startKafkaRatePipeline(ctx context.Context, t *testing.T, c *StreamRateCalculator, pusher timestampedPusher) *kgo.Client {
	t.Helper()
	_, cfg := testkafka.CreateCluster(t, 1, "rate-snapshots")
	reader, err := partition.NewReaderService(cfg, 0, "rate-test", NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), 8, c), log.NewNopLogger(), prometheus.NewRegistry(), nil, partition.WithHighWatermarkObserver(c.setKafkaHighWatermark))
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(ctx, reader))
	t.Cleanup(func() { require.NoError(t, services.StopAndAwaitTerminated(context.Background(), reader)) })
	c.markKafkaConsumerRunning()
	c.updateRates()
	producer, err := client.NewWriterClient("rate-test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	return producer
}

// mixedKafkaRateRecords returns native and nested records with their expected expanded entries.
func mixedKafkaRateRecords(t *testing.T, recordsPerFormat int) ([]*kgo.Record, []logproto.Entry) {
	t.Helper()
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)
	var records []*kgo.Record
	var expected []logproto.Entry
	for i := range recordsPerFormat {
		for _, format := range []string{"native", "nested"} {
			entry := logproto.Entry{
				Timestamp:          start.Add(time.Duration(i) * time.Second),
				Line:               fmt.Sprintf("%s-%02d-%s", format, i, strings.Repeat("x", 180)),
				StructuredMetadata: []logproto.LabelAdapter{{Name: "log", Value: "value"}},
			}
			stream := logproto.Stream{Labels: fmt.Sprintf(`{app="%s"}`, format), Entries: []logproto.Entry{entry}}
			var encoded []*kgo.Record
			var err error
			if format == "native" {
				encoded, err = kafka.Encode(0, tenantID, stream, 1<<20)
			} else {
				nested := logproto.InternalStreamAdapter{
					Labels: stream.Labels,
					ResourceLogs: []logproto.ResourceLogs{{
						Attrs: []logproto.LabelAdapter{{Name: "resource", Value: "shared"}},
						ScopeLogs: []logproto.ScopeLogs{{
							Attrs:   []logproto.LabelAdapter{{Name: "scope", Value: "shared"}},
							Entries: stream.Entries,
						}},
					}},
				}
				encoded, err = kafka.EncodeInternal(0, tenantID, nested, 1<<20)
				entry.StructuredMetadata = append(entry.StructuredMetadata,
					logproto.LabelAdapter{Name: "resource", Value: "shared"},
					logproto.LabelAdapter{Name: "scope", Value: "shared"})
			}
			require.NoError(t, err)
			require.Len(t, encoded, 1)
			records = append(records, encoded...)
			expected = append(expected, entry)
		}
	}
	return records, expected
}

func consumeKafkaRateBatch(ctx context.Context, consumer partition.Consumer, records []partition.Record) {
	input := make(chan []partition.Record, 1)
	input <- records
	close(input)
	consumer.Start(ctx, input)()
}

func requireStreamRateTotals(t *testing.T, response *logproto.StreamRatesResponse, wantBytes int64, wantPushes uint32) {
	t.Helper()
	var bytes int64
	var pushes uint32
	for _, rate := range response.StreamRates {
		bytes += rate.Rate
		pushes += rate.Pushes
	}
	require.Equal(t, wantBytes, bytes)
	require.Equal(t, wantPushes, pushes)
}

func runKafkaRateIngestion(t *testing.T, records []partition.Record, enabled bool) (*logproto.StreamRatesResponse, map[string][]uint32) {
	t.Helper()
	cfg := defaultIngesterTestConfigWithWAL(t, t.TempDir())
	cfg.WAL.FlushOnShutdown = true
	cfg.WAL.CheckpointDuration = time.Hour
	cfg.ChunkEncoding = compression.None.String()
	cfg.BlockSize, cfg.TargetChunkSize = 256, 1024
	store, ing := newTestStore(t, cfg, nil)
	t.Cleanup(func() { require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing)) })
	// Rotate explicitly so disk/scheduler delays cannot split the assertion across samples.
	ing.streamRateCalculator.Stop()
	var c *StreamRateCalculator
	if enabled {
		c = newReadyKafkaStreamRateCalculator(int64(len(records) + 1))
	} else {
		c = newManualStreamRateCalculator()
	}
	ing.streamRateCalculator = c
	if enabled {
		consumer, err := NewKafkaConsumerFactory(ing, prometheus.NewRegistry(), 8, c)(&noopCommitter{}, log.NewNopLogger())
		require.NoError(t, err)
		consumeKafkaRateBatch(context.Background(), consumer, records)
	} else {
		decoder, err := kafka.NewDecoder()
		require.NoError(t, err)
		for _, record := range records {
			stream, err := decoder.DecodeWithoutLabels(record.Content)
			require.NoError(t, err)
			ctx := user.InjectOrgID(record.Ctx, record.TenantID)
			_, err = ing.PushWithTimestamp(ctx, TimestampedPush{PushRequest: &logproto.PushRequest{Streams: []logproto.Stream{stream}}})
			require.NoError(t, err)
		}
	}
	c.updateRates()
	response, err := ing.GetStreamRates(context.Background(), &logproto.StreamRatesRequest{})
	require.NoError(t, err)
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
	chunks := store.getChunksForUser(tenantID)
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].From < chunks[j].From })
	checksums := map[string][]uint32{}
	for _, chunk := range chunks {
		checksums[chunk.Metric.String()] = append(checksums[chunk.Metric.String()], chunk.Checksum)
	}
	return response, checksums
}
