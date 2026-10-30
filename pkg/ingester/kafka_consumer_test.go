package ingester

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"slices"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

var (
	tenantID  = "foo"
	streamBar = logproto.Stream{
		Labels: labels.FromStrings("stream", "1").String(),
		Entries: []logproto.Entry{
			{
				Timestamp: time.Unix(0, 1).UTC(),
				Line:      "1",
			},
			{
				Timestamp: time.Unix(0, 2).UTC(),
				Line:      "2",
			},
		},
	}
	streamFoo = logproto.Stream{
		Labels: labels.FromStrings("stream", "2").String(),
		Entries: []logproto.Entry{
			{
				Timestamp: time.Unix(0, 1).UTC(),
				Line:      "3",
			},
			{
				Timestamp: time.Unix(0, 2).UTC(),
				Line:      "4",
			},
		},
	}
)

type fakePusher struct {
	pushes []*logproto.PushRequest
	t      *testing.T
}

func (f *fakePusher) Push(ctx context.Context, in *logproto.PushRequest) (*logproto.PushResponse, error) {
	tenant, err := tenant.TenantID(ctx)
	require.NoError(f.t, err)
	require.Equal(f.t, tenant, tenant)
	// we need to copy in as it will be reused by the decoder.
	req := &logproto.PushRequest{}
	for _, s := range in.Streams {
		newStream := push.Stream{
			Labels:  s.Labels,
			Entries: make([]push.Entry, len(s.Entries)),
		}
		copy(newStream.Entries, s.Entries)
		req.Streams = append(req.Streams, newStream)
	}
	f.pushes = append(f.pushes, req)
	return nil, nil
}

type noopCommitter struct{}

func (nc *noopCommitter) EnqueueOffset(_ int64) {}

func (noopCommitter) Commit(_ context.Context, _ int64) error { return nil }

func TestConsumer(t *testing.T) {
	want := []*logproto.PushRequest{
		{
			Streams: []logproto.Stream{streamBar},
		},
		{
			Streams: []logproto.Stream{streamFoo},
		},
	}

	encoders := map[string]func(*testing.T, logproto.Stream) []*kgo.Record{
		"can consume flattened data": encodeFlat,
		"can consume nested data":    encodeNested,
	}

	for name, encoder := range encoders {
		t.Run(name, func(t *testing.T) {
			got := consume(t, encoder, streamBar, streamFoo)
			require.Equal(t, want, got)
		})
	}
}

// consume runs the records of each stream through a consumer and returns what reached the
// pusher. encode chooses the encoding the records are written in.
func consume(t *testing.T, encode func(*testing.T, logproto.Stream) []*kgo.Record, streams ...logproto.Stream) []*logproto.PushRequest {
	t.Helper()

	var (
		toPush     []partition.Record
		offset     = int64(0)
		pusher     = &fakePusher{t: t}
		numWorkers = 1
	)

	// Set the number of workers to 1 to test the consumer
	consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), numWorkers)(&noopCommitter{}, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, err)

	for _, stream := range streams {
		for _, record := range encode(t, stream) {
			toPush = append(toPush, partition.Record{
				Ctx:      context.Background(),
				TenantID: tenantID,
				Content:  record.Value,
				Offset:   offset,
			})
			offset++
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	recordChan := make(chan []partition.Record)
	wait := consumer.Start(ctx, recordChan)

	// Send records in separate batches
	recordChan <- toPush // Send streamBar record

	cancel()
	wait()

	return pusher.pushes
}

type recordingCommitter struct {
	offsets []int64
}

func (rc *recordingCommitter) EnqueueOffset(offset int64) { rc.offsets = append(rc.offsets, offset) }

func (recordingCommitter) Commit(_ context.Context, _ int64) error { return nil }

// failingPusher fails, without a retry, the push of every record whose first line is in fail.
type failingPusher struct {
	fail map[string]bool
}

func (p *failingPusher) Push(_ context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	if p.fail[req.Streams[0].Entries[0].Line] {
		return nil, errors.New("push failed")
	}
	return &logproto.PushResponse{}, nil
}

// TestConsumer_CommitsHighestOffsetBeforeGap checks that grouping a batch by stream still
// commits up to the first record that failed, whichever stream it belongs to.
func TestConsumer_CommitsHighestOffsetBeforeGap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fail   []int64 // offsets of the records to fail
		commit []int64
	}{
		{name: "all succeed", commit: []int64{9}},
		{name: "first fails", fail: []int64{1}},
		{name: "one in the middle fails", fail: []int64{5}, commit: []int64{4}},
		{name: "two streams fail", fail: []int64{8, 6}, commit: []int64{5}},
		{name: "last fails", fail: []int64{9}, commit: []int64{8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				records []partition.Record
				pusher  = &failingPusher{fail: map[string]bool{}}
			)
			// Records 1 to 9 belong to streams a, b and c in turn.
			for offset := int64(1); offset <= 9; offset++ {
				line := strconv.FormatInt(offset, 10)
				stream := logproto.Stream{
					Labels:  labels.FromStrings("stream", string(rune('a'+offset%3))).String(),
					Entries: []logproto.Entry{{Timestamp: time.Unix(offset, 0), Line: line}},
				}
				if slices.Contains(tc.fail, offset) {
					pusher.fail[line] = true
				}
				records = append(records, partition.Record{
					Ctx:      context.Background(),
					TenantID: tenantID,
					Content:  encodeFlat(t, stream)[0].Value,
					Offset:   offset,
				})
			}

			committer := &recordingCommitter{}
			consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), 4)(committer, log.NewNopLogger())
			require.NoError(t, err)
			recordsCh := make(chan []partition.Record, 1)
			recordsCh <- records
			close(recordsCh)
			consumer.Start(context.Background(), recordsCh)()

			require.Equal(t, tc.commit, committer.offsets)
		})
	}
}

// reorderingPusher holds each record of one stream until that stream's next record has been
// pushed, or until a timeout. A consumer that spreads the stream's records over several workers
// therefore applies them in reverse offset order. A consumer that applies them on one worker in
// offset order makes every wait time out instead.
type reorderingPusher struct {
	next    logproto.PusherServer
	labels  string
	timeout time.Duration
	pushed  []chan struct{} // pushed[i] is closed once record i of the stream has been pushed.
}

func newReorderingPusher(next logproto.PusherServer, labels string, records int, timeout time.Duration) *reorderingPusher {
	pushed := make([]chan struct{}, records)
	for i := range pushed {
		pushed[i] = make(chan struct{})
	}
	return &reorderingPusher{next: next, labels: labels, timeout: timeout, pushed: pushed}
}

func (p *reorderingPusher) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	if req.Streams[0].Labels != p.labels {
		return p.next.Push(ctx, req)
	}
	var i int
	if _, err := fmt.Sscanf(req.Streams[0].Entries[0].Line, "r%d-", &i); err != nil {
		return nil, err
	}
	if i+1 < len(p.pushed) {
		select {
		case <-p.pushed[i+1]:
		case <-time.After(p.timeout):
		}
	}
	resp, err := p.next.Push(ctx, req)
	close(p.pushed[i])
	return resp, err
}

// TestConsumer_ReplicasBuildIdenticalChunks consumes the same records into two ingesters, as two
// replicas of a partition do. Workers run in a different order on each replica, which the
// reorderingPusher makes deterministic on the second one. A stream's chunks are only byte
// identical, and so deduped by the chunk store (the checksum is part of the chunk key), if every
// replica applies its records in the same order. MemChunk.Close does reorder overlapping blocks,
// but which entries end up in which chunk is decided by arrival order when the stream cuts a full
// chunk.
func TestConsumer_ReplicasBuildIdenticalChunks(t *testing.T) {
	const (
		recordsPerStream = 9
		entriesPerRecord = 4
		lineSize         = 100
		numWorkers       = 16
	)
	var (
		streamA = labels.FromStrings("stream", "a").String()
		streamB = labels.FromStrings("stream", "b").String()
		start   = time.Now().Add(-time.Hour).Truncate(time.Hour)
		rnd     = rand.New(rand.NewSource(1))
	)

	// The records of both streams, interleaved, with lines that don't compress so the chunk
	// boundaries fall between records.
	var records []partition.Record
	for i := 0; i < recordsPerStream; i++ {
		for _, ls := range []string{streamA, streamB} {
			stream := logproto.Stream{Labels: ls}
			for j := 0; j < entriesPerRecord; j++ {
				line := fmt.Sprintf("r%03d-e%02d-", i, j)
				for len(line) < lineSize {
					line += strconv.FormatUint(rnd.Uint64(), 36)
				}
				stream.Entries = append(stream.Entries, logproto.Entry{
					Timestamp: start.Add(time.Duration(i*entriesPerRecord+j) * time.Second),
					Line:      line[:lineSize],
				})
			}
			for _, record := range encodeFlat(t, stream) {
				records = append(records, partition.Record{
					Ctx:      context.Background(),
					TenantID: tenantID,
					Content:  record.Value,
					Offset:   int64(len(records) + 1),
				})
			}
		}
	}

	// replica consumes the records into a fresh ingester and returns the checksums of each
	// stream's flushed chunks in time order.
	replica := func(wrap func(logproto.PusherServer) logproto.PusherServer) map[string][]uint32 {
		cfg := defaultIngesterTestConfig(t)
		cfg.ChunkEncoding = compression.None.String()
		cfg.BlockSize = 256
		cfg.TargetChunkSize = 1024
		store, ing := newTestStore(t, cfg, nil)

		consumer, err := NewKafkaConsumerFactory(wrap(ing), prometheus.NewRegistry(), numWorkers)(&noopCommitter{}, log.NewNopLogger())
		require.NoError(t, err)
		recordsCh := make(chan []partition.Record, 1)
		recordsCh <- records
		close(recordsCh)
		consumer.Start(context.Background(), recordsCh)()

		// Stopping the ingester flushes every chunk to the store.
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
		chunks := store.getChunksForUser(tenantID)
		sort.Slice(chunks, func(i, j int) bool { return chunks[i].From < chunks[j].From })
		checksums := map[string][]uint32{}
		for _, c := range chunks {
			checksums[c.Metric.String()] = append(checksums[c.Metric.String()], c.Checksum)
		}
		return checksums
	}

	inOrder := replica(func(p logproto.PusherServer) logproto.PusherServer { return p })
	reordered := replica(func(p logproto.PusherServer) logproto.PusherServer {
		return newReorderingPusher(p, streamA, recordsPerStream, 100*time.Millisecond)
	})

	require.Len(t, inOrder, 2)
	for ls, checksums := range inOrder {
		require.Greater(t, len(checksums), 2, "stream %s needs several chunks for the test to cut chunks by size", ls)
	}
	require.Equal(t, inOrder, reordered)
}

// BenchmarkConsume measures how long the consumer takes to apply a batch of records to an
// ingester, with the records spread evenly over many streams or mostly in one hot stream.
func BenchmarkConsume(b *testing.B) {
	const (
		batchSize        = 1000
		entriesPerRecord = 5
		numWorkers       = 8
	)
	for _, tc := range []struct {
		name    string
		streams int
		hot     float64 // share of the records in stream 0, on top of their even share
	}{
		{name: "uniform", streams: 100},
		{name: "one stream has half", streams: 100, hot: 0.5},
		{name: "one stream has 90%", streams: 100, hot: 0.9},
		{name: "single stream", streams: 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			_, ing := newTestStore(b, defaultIngesterTestConfig(b), nil)
			defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck
			consumer, err := NewKafkaConsumerFactory(ing, prometheus.NewRegistry(), numWorkers)(&noopCommitter{}, log.NewNopLogger())
			require.NoError(b, err)
			kc := consumer.(*kafkaConsumer)

			var (
				rnd     = rand.New(rand.NewSource(1))
				streams = make([]string, tc.streams)
				ts      = time.Now().Add(-time.Hour)
				offset  int64
			)
			for i := range streams {
				streams[i] = labels.FromStrings("app", "bench", "stream", strconv.Itoa(i)).String()
			}
			batch := func() []partition.Record {
				records := make([]partition.Record, 0, batchSize)
				for range batchSize {
					stream := logproto.Stream{Labels: streams[rnd.Intn(len(streams))]}
					if rnd.Float64() < tc.hot {
						stream.Labels = streams[0]
					}
					for range entriesPerRecord {
						ts = ts.Add(time.Microsecond)
						stream.Entries = append(stream.Entries, logproto.Entry{
							Timestamp: ts,
							Line:      fmt.Sprintf("level=info caller=bench.go:42 msg=\"request done\" duration=%dms status=200 trace_id=%016x", rnd.Intn(1000), rnd.Uint64()),
						})
					}
					encoded, err := kafka.Encode(0, tenantID, stream, 10000)
					require.NoError(b, err)
					offset++
					records = append(records, partition.Record{Ctx: context.Background(), TenantID: tenantID, Content: encoded[0].Value, Offset: offset})
				}
				return records
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				records := batch()
				b.StartTimer()
				kc.consume(context.Background(), records)
			}
			b.ReportMetric(float64(b.N*batchSize)/b.Elapsed().Seconds(), "records/s")
		})
	}
}

func encodeFlat(t *testing.T, stream logproto.Stream) []*kgo.Record {
	t.Helper()
	records, err := kafka.Encode(0, tenantID, stream, 10000)
	require.NoError(t, err)
	return records
}

func encodeNested(t *testing.T, stream logproto.Stream) []*kgo.Record {
	t.Helper()
	nested := logproto.FromStream(stream)
	data, err := nested.Marshal()
	require.NoError(t, err)
	return []*kgo.Record{{Key: []byte(tenantID), Value: data}}
}
