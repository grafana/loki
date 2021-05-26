package kafka

import (
	"context"
	"testing"

	"github.com/Shopify/sarama"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/prometheus/client_golang/prometheus"
)

// Consumergroup handler
type testConsumerGroupHandler struct {
	handler sarama.ConsumerGroupHandler
	ctx     context.Context
	topics  []string

	returnErr error
}

func (C testConsumerGroupHandler) Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	if C.returnErr != nil {
		return C.returnErr
	}
	C.ctx = ctx
	C.topics = topics
	C.handler = handler
	<-ctx.Done()
	return nil
}

func (C testConsumerGroupHandler) Errors() <-chan error {
	return nil
}

func (C testConsumerGroupHandler) Close() error {
	return nil
}

// type session struct{}

// func (s *session) Claims() map[string][]int32                                               { return nil }
// func (s *session) MemberID() string                                                         { return "foo" }
// func (s *session) GenerationID() int32                                                      { return 10 }
// func (s *session) MarkOffset(topic string, partition int32, offset int64, metadata string)  {}
// func (s *session) Commit()                                                                  {}
// func (s *session) ResetOffset(topic string, partition int32, offset int64, metadata string) {}
// func (s *session) MarkMessage(msg *sarama.ConsumerMessage, metadata string)                 {}
// func (s *session) Context() context.Context                                                 { return context.Background() }

func Test_Syncer_Consume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		ctx:    ctx,
		cancel: cancel,
		reg:    prometheus.DefaultRegisterer,
		group:  &testConsumerGroupHandler{},
		cfg: scrapeconfig.Config{
			JobName: "foo",
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               "topic1,topic2",
			},
		},
	}
	ts.consume()
	cancel()
	ts.wg.Wait()
}

func Test_Syncer_Consume_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		ctx:    ctx,
		cancel: cancel,
		reg:    prometheus.DefaultRegisterer,
		group:  &testConsumerGroupHandler{returnErr: sarama.ErrKafkaStorageError},
		cfg: scrapeconfig.Config{
			JobName: "foo",
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               "topic1,topic2",
			},
		},
	}
	ts.consume()
	cancel()
	ts.wg.Wait()
}
