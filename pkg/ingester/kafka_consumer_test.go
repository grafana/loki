package ingester

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

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
	var (
		toPush     []partition.Record
		offset     = int64(0)
		pusher     = &fakePusher{t: t}
		numWorkers = 1
	)

	// Set the number of workers to 1 to test the consumer
	consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), numWorkers)(&noopCommitter{}, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, err)

	records, err := kafka.Encode(0, tenantID, streamBar, 10000)
	require.NoError(t, err)

	for _, record := range records {
		toPush = append(toPush, partition.Record{
			Ctx:      context.Background(),
			TenantID: tenantID,
			Content:  record.Value,
			Offset:   offset,
		})
		offset++
	}
	records, err = kafka.Encode(0, tenantID, streamFoo, 10000)
	require.NoError(t, err)
	for _, record := range records {
		toPush = append(toPush, partition.Record{
			Ctx:      context.Background(),
			TenantID: tenantID,
			Content:  record.Value,
			Offset:   offset,
		})
		offset++
	}

	ctx, cancel := context.WithCancel(context.Background())
	recordChan := make(chan []partition.Record)
	wait := consumer.Start(ctx, recordChan)

	// Send records in separate batches
	recordChan <- toPush // Send streamBar record

	cancel()
	wait()

	require.Equal(t, []*logproto.PushRequest{
		{
			Streams: []logproto.Stream{streamBar},
		},
		{
			Streams: []logproto.Stream{streamFoo},
		},
	}, pusher.pushes)
}

type failThenSucceedPusher struct {
	failCount    int
	pushAttempts atomic.Int32
	pushes       []*logproto.PushRequest
	t            *testing.T
}

func (f *failThenSucceedPusher) Push(ctx context.Context, in *logproto.PushRequest) (*logproto.PushResponse, error) {
	attempt := int(f.pushAttempts.Add(1))
	if attempt <= f.failCount {
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "maximum active stream limit exceeded")
	}

	_, err := tenant.TenantID(ctx)
	require.NoError(f.t, err)

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

func TestConsumer_Retries429(t *testing.T) {
	pusher := &failThenSucceedPusher{
		failCount: 3,
		t:         t,
	}

	consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry(), 1)(&noopCommitter{}, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, err)

	consumer.(*kafkaConsumer).retryBackoff = backoff.Config{
		MinBackoff: time.Millisecond,
		MaxBackoff: 5 * time.Millisecond,
		MaxRetries: 0,
	}

	records, err := kafka.Encode(0, tenantID, streamBar, 10000)
	require.NoError(t, err)

	var toPush []partition.Record
	for i, record := range records {
		toPush = append(toPush, partition.Record{
			Ctx:      context.Background(),
			TenantID: tenantID,
			Content:  record.Value,
			Offset:   int64(i + 1),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	recordChan := make(chan []partition.Record)
	wait := consumer.Start(ctx, recordChan)

	recordChan <- toPush
	close(recordChan)
	wait()

	require.Equal(t, int32(4), pusher.pushAttempts.Load(), "expected 3 failed attempts + 1 successful attempt")
	require.Len(t, pusher.pushes, 1, "expected exactly one successful push")
	require.Equal(t, []*logproto.PushRequest{
		{Streams: []logproto.Stream{streamBar}},
	}, pusher.pushes)
}
