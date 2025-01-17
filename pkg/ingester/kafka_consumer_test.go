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

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

var (
	tenantID  = "foo"
	streamBar = logproto.Stream{
		Labels: labels.Labels{labels.Label{Name: "stream", Value: "1"}}.String(),
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
		Labels: labels.Labels{labels.Label{Name: "stream", Value: "2"}}.String(),
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
		toPush []partition.Record
		offset = int64(0)
		pusher = &fakePusher{t: t}
	)

	consumer, err := NewKafkaConsumerFactory(pusher, prometheus.NewRegistry())(&noopCommitter{}, log.NewLogfmtLogger(os.Stdout))
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
	records, err = kafka.Encode(0, "foo", streamFoo, 10000)
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

	recordChan <- toPush

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
