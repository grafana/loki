package ingester

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/kafka"
	kafkaclient "github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestIngester_QuerySample_ShouldHonorSampleOrdering(t *testing.T) {
	const tenant = "fake"

	// Pre-condition check: this test generate two log stream labels whose stable hash collides.
	const collidePodA, collidePodB = "39ae2fcfd732c147", "f35246e8ca75a99b"
	collide := func(pod string) labels.Labels {
		return labels.FromStrings("cluster", "prod", "namespace", "team", "pod", pod)
	}
	collideHash := labels.StableHash(collide(collidePodA))
	require.NotEqual(t, collidePodA, collidePodB)
	require.Equalf(t, collideHash, labels.StableHash(collide(collidePodB)),
		"collision fixture no longer collides on StableHash — regenerate via TestFindStableHashCollision")

	// Pre-condition check: the raw fingerprint collides (these log stream labels have no __name__).
	rawA, _ := collide(collidePodA).HashWithoutLabels(nil)
	rawB, _ := collide(collidePodB).HashWithoutLabels(nil)
	require.Equal(t, collideHash, rawA, "without __name__, HashWithoutLabels must equal StableHash")
	require.Equal(t, rawA, rawB, "a StableHash collision on __name__-less labels must also collide the raw fingerprint")

	// Prepare utilities.
	var (
		now  = time.Now()
		at   = func(offsetMillis int) time.Time { return now.Add(time.Duration(offsetMillis) * time.Millisecond) }
		line = func(i int) string { return fmt.Sprintf("line-%d", i) }
	)

	type entry struct {
		t    time.Time
		line string
	}
	type fixture struct {
		lbls    labels.Labels
		entries []entry
	}

	// Create fixture log lines.
	fixtures := []fixture{
		// Interleaved timestamps between two streams: distinguishes timestamp-first from stream-first.
		{labels.FromStrings("cluster", "prod", "app", "a"), []entry{{at(-50), line(1)}, {at(-30), line(2)}, {at(-10), line(3)}}},
		{labels.FromStrings("cluster", "prod", "app", "b"), []entry{{at(-40), line(4)}, {at(-20), line(5)}}},
		// Same timestamp across streams (shares at(-20) with app=b): tie-break.
		{labels.FromStrings("cluster", "prod", "app", "c"), []entry{{at(-20), line(6)}}},
		// Intra-stream duplicate timestamp (distinct lines -> two samples at one ts).
		{labels.FromStrings("cluster", "prod", "app", "d"), []entry{{at(-15), line(7)}, {at(-15), line(8)}, {at(-5), line(9)}}},
		// Single-sample stream.
		{labels.FromStrings("cluster", "prod", "app", "f"), []entry{{at(-1), line(10)}}},
		// Hash-colliding pair: identical StableHash, distinct labels (differ only in the pod value).
		{collide(collidePodA), []entry{{at(-25), line(200)}, {at(-12), line(201)}}},
		{collide(collidePodB), []entry{{at(-25), line(202)}, {at(-12), line(203)}}},
	}
	// Dense stream (many samples), appended programmatically.
	dense := fixture{lbls: labels.FromStrings("cluster", "prod", "app", "e")}
	for k := 0; k < 8; k++ {
		dense.entries = append(dense.entries, entry{at(-100 + k), line(100 + k)})
	}
	fixtures = append(fixtures, dense)

	// Produce all fixtures to Kafka before starting the ingester, so startup replay consumes them.
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
	producer, err := kafkaclient.NewWriterClient("test", kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	for _, f := range fixtures {
		s := logproto.Stream{Labels: f.lbls.String()}
		for _, e := range f.entries {
			s.Entries = append(s.Entries, logproto.Entry{Timestamp: e.t, Line: e.line})
		}
		recs, err := kafka.Encode(0, tenant, s, 10<<20)
		require.NoError(t, err)
		require.NoError(t, producer.ProduceSync(context.Background(), recs...).FirstErr())
	}

	// Ingester wired to the fake Kafka via the partition ingest path.
	cfg := defaultIngesterTestConfig(t)
	cfg.KafkaIngestion.Enabled = true
	cfg.KafkaIngestion.KafkaConfig = kafkaCfg
	partitionKV, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetPartitionRingCodec(), nil, log.NewNopLogger())
	require.NoError(t, err)
	cfg.KafkaIngestion.PartitionRingConfig.KVStore.Mock = partitionKV
	cfg.KafkaIngestion.PartitionRingConfig.MinOwnersCount = 0
	cfg.KafkaIngestion.PartitionRingConfig.MinOwnersDuration = 0

	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)
	store := &mockStore{chunks: map[string][]chunk.Chunk{}}

	i, err := New(cfg, client.Config{}, store, limits, runtime.DefaultTenantConfigs(), prometheus.NewRegistry(),
		writefailures.Cfg{}, constants.Loki, log.NewNopLogger(), nil,
		mockReadRingWithOneActiveIngester(), mockPartitionRingReader{ring: newMockPartitionRingWithActivePartitions(0)})
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), i)) // blocks until Kafka replay caught up
	t.Cleanup(func() { require.NoError(t, services.StopAndAwaitTerminated(context.Background(), i)) })

	ctx := user.InjectOrgID(context.Background(), tenant)
	querySamples := func(selector string, order logproto.SampleOrder) []receivedSample {
		sink := &sampleSink{ctx: ctx}
		require.NoError(t, i.QuerySample(&logproto.SampleQueryRequest{
			Selector: selector,
			Start:    now.Add(-time.Hour),
			End:      now.Add(time.Minute),
			Order:    order,
			Plan:     &plan.QueryPlan{AST: syntax.MustParseExpr(selector)},
		}, sink))
		return sink.receivedSamples
	}

	// A single query over every fixture stream (all share cluster="prod"), including the two
	// hash-colliding streams, exercised under both sample orders.
	query := `count_over_time({cluster="prod"}[1h])`
	byTimestamp := querySamples(query, logproto.SAMPLE_ORDER_BY_TIMESTAMP)
	byStream := querySamples(query, logproto.SAMPLE_ORDER_BY_STREAM)
	require.NotEmpty(t, byTimestamp)

	t.Run("should return the same exact samples regardless of ordering", func(t *testing.T) {
		require.ElementsMatch(t, byTimestamp, byStream)
	})

	t.Run("order by timestamp should return samples sorted by timestamp", func(t *testing.T) {
		lastTSByLabel := map[string]int64{}
		for _, s := range byTimestamp {
			if prev, ok := lastTSByLabel[s.labels]; ok {
				require.LessOrEqualf(t, prev, s.ts, "timestamp-first: ts not non-decreasing within stream %s", s.labels)
			}
			lastTSByLabel[s.labels] = s.ts
		}
	})

	t.Run("order by stream should return samples sorted by log stream, and then timestamp", func(t *testing.T) {
		groupKey := func(s receivedSample) string { return fmt.Sprintf("%d\x00%s", s.streamHash, s.labels) }
		seen := map[string]bool{}

		for k := 0; k < len(byStream); k++ {
			cur := byStream[k]
			if k > 0 {
				prev := byStream[k-1]
				switch {
				case cur.streamHash != prev.streamHash:
					require.Greaterf(t, cur.streamHash, prev.streamHash, "stream-first: streamHash not ascending at index %d", k)
				case cur.labels != prev.labels:
					require.Greaterf(t, cur.labels, prev.labels, "stream-first: labels tie-break not ascending at index %d", k)
				default:
					require.LessOrEqualf(t, prev.ts, cur.ts, "stream-first: ts not non-decreasing within a stream at index %d", k)
				}
				if groupKey(cur) != groupKey(prev) {
					require.Falsef(t, seen[groupKey(cur)], "stream-first: stream group %q recurred", groupKey(cur))
				}
			}
			seen[groupKey(cur)] = true
		}
	})

	t.Run("the ingester's fp mapper keeps hash-colliding streams as distinct in-memory streams", func(t *testing.T) {
		// For label sets without __name__ a StableHash collision is also a raw-fingerprint collision,
		// so the fp mapper must remap one of them; both then share labelHash but keep distinct fps.
		inst, err := i.GetOrCreateInstance(tenant)
		require.NoError(t, err)
		fps := map[string]uint64{}
		require.NoError(t, inst.streams.ForEach(func(s *stream) (bool, error) {
			if s.labels.Get("namespace") == "team" {
				require.Equalf(t, collideHash, s.labelHash, "collision stream %s must expose the shared StableHash", s.labels)
				fps[s.labels.String()] = uint64(s.fp)
			}
			return true, nil
		}))
		require.Len(t, fps, 2, "the two colliding streams must be distinct in-memory streams")
		fpA, fpB := fps[collide(collidePodA).String()], fps[collide(collidePodB).String()]
		require.NotEqual(t, fpA, fpB, "the two colliding streams must have distinct fingerprints")
		// Exactly one keeps the raw (colliding) fp; the other is remapped into the reserved fp space
		// (<= maxMappedFP). This proves the fp mapper actually resolved a raw-fingerprint collision,
		// rather than the two streams just happening to have different fps.
		require.True(t, (fpA <= maxMappedFP) != (fpB <= maxMappedFP),
			"exactly one colliding stream must be remapped into the reserved fp space")
	})

	t.Run("order by stream should return the two hash-colliding streams as distinct adjacent groups", func(t *testing.T) {
		labelsA, labelsB := collide(collidePodA).String(), collide(collidePodB).String()
		var got []receivedSample
		for _, s := range byStream {
			if s.labels == labelsA || s.labels == labelsB {
				got = append(got, s)
			}
		}

		// Both streams share collideHash but keep their own labels; stream-first orders them by the
		// labels tie-break (pod "39ae…" < "f35…"), each stream's samples contiguous and ts-ascending.
		require.Equal(t, []receivedSample{
			{labelsA, collideHash, at(-25).UnixNano(), 1},
			{labelsA, collideHash, at(-12).UnixNano(), 1},
			{labelsB, collideHash, at(-25).UnixNano(), 1},
			{labelsB, collideHash, at(-12).UnixNano(), 1},
		}, got)
	})
}

// receivedSample is one streamed sample flattened back to its identity + point.
type receivedSample struct {
	labels     string
	streamHash uint64
	ts         int64
	value      float64
}

// sampleSink collects everything QuerySample streams. QuerySample is a streaming gRPC handler, so a
// server sink is unavoidable — but this one only records batches, it mocks no behaviour.
type sampleSink struct {
	grpc.ServerStream

	ctx             context.Context
	receivedSamples []receivedSample
}

func (s *sampleSink) Context() context.Context { return s.ctx }

func (s *sampleSink) Send(resp *logproto.SampleQueryResponse) error {
	for _, series := range resp.Series {
		for _, smp := range series.Samples {
			s.receivedSamples = append(s.receivedSamples, receivedSample{series.Labels, series.StreamHash, smp.Timestamp, smp.Value})
		}
	}
	return nil
}
