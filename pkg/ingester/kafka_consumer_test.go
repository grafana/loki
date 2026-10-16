package ingester

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

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
