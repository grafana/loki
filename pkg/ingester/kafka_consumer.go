package ingester

import (
	"context"
	math "math"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func NewKafkaConsumerFactory(pusher logproto.PusherServer, logger log.Logger) partition.ConsumerFactory {
	return func(committer partition.Committer) (partition.Consumer, error) {
		decoder, err := kafka.NewDecoder()
		if err != nil {
			return nil, err
		}
		return &kafkaConsumer{
			pusher:  pusher,
			logger:  logger,
			decoder: decoder,
		}, nil
	}
}

type kafkaConsumer struct {
	pusher  logproto.PusherServer
	logger  log.Logger
	decoder *kafka.Decoder
}

func (kc *kafkaConsumer) Start(ctx context.Context, recordsChan <-chan []partition.Record) func() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				level.Info(kc.logger).Log("msg", "shutting down kafka consumer")
				return
			case records := <-recordsChan:
				kc.consume(records)
			}
		}
	}()
	return wg.Wait
}

func (kc *kafkaConsumer) consume(records []partition.Record) {
	if len(records) == 0 {
		return
	}
	var (
		minOffset = int64(math.MaxInt64)
		maxOffset = int64(0)
	)
	for _, record := range records {
		minOffset = min(minOffset, record.Offset)
		maxOffset = max(maxOffset, record.Offset)
	}
	level.Debug(kc.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset)
	for _, record := range records {
		stream, err := kc.decoder.DecodeWithoutLabels(record.Content)
		if err != nil {
			level.Error(kc.logger).Log("msg", "failed to decode record", "error", err)
			continue
		}
		ctx := user.InjectOrgID(record.Ctx, record.TenantID)
		if _, err := kc.pusher.Push(ctx, &logproto.PushRequest{
			Streams: []logproto.Stream{stream},
		}); err != nil {
			level.Error(kc.logger).Log("msg", "failed to push records", "error", err)
		}
	}
}
