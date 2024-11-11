package kafka

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
)

// Consumergroup handler
type testConsumerGroupHandler struct {
	handler sarama.ConsumerGroupHandler
	ctx     context.Context
	topics  []string
	mu      *sync.Mutex

	returnErr error

	consuming atomic.Bool
}

func (c *testConsumerGroupHandler) Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	if c.returnErr != nil {
		return c.returnErr
	}
	c.ctx = ctx
	c.mu.Lock()
	c.topics = topics
	c.mu.Unlock()
	c.handler = handler
	c.consuming.Store(true)
	<-ctx.Done()
	c.consuming.Store(false)
	return nil
}

func (c testConsumerGroupHandler) Errors() <-chan error {
	return nil
}

func (c testConsumerGroupHandler) Close() error {
	return nil
}

func (c testConsumerGroupHandler) Pause(_ map[string][]int32)  {}
func (c testConsumerGroupHandler) Resume(_ map[string][]int32) {}
func (c testConsumerGroupHandler) PauseAll()                   {}
func (c testConsumerGroupHandler) ResumeAll()                  {}

type testSession struct {
	markedMessage []*sarama.ConsumerMessage
}

func (s *testSession) Claims() map[string][]int32                       { return nil }
func (s *testSession) MemberID() string                                 { return "foo" }
func (s *testSession) GenerationID() int32                              { return 10 }
func (s *testSession) MarkOffset(_ string, _ int32, _ int64, _ string)  {}
func (s *testSession) Commit()                                          {}
func (s *testSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
func (s *testSession) MarkMessage(msg *sarama.ConsumerMessage, _ string) {
	s.markedMessage = append(s.markedMessage, msg)
}
func (s *testSession) Context() context.Context { return context.Background() }

type testClaim struct {
	topic     string
	partition int32
	offset    int64
	messages  chan *sarama.ConsumerMessage
}

func newTestClaim(topic string, partition int32, offset int64) *testClaim {
	return &testClaim{
		topic:     topic,
		partition: partition,
		offset:    offset,
		messages:  make(chan *sarama.ConsumerMessage),
	}
}

func (t *testClaim) Topic() string                            { return t.topic }
func (t *testClaim) Partition() int32                         { return t.partition }
func (t *testClaim) InitialOffset() int64                     { return t.offset }
func (t *testClaim) HighWaterMarkOffset() int64               { return 0 }
func (t *testClaim) Messages() <-chan *sarama.ConsumerMessage { return t.messages }
func (t *testClaim) Send(m *sarama.ConsumerMessage) {
	t.messages <- m
}

func (t *testClaim) Stop() {
	close(t.messages)
}

func Test_TargetRun(t *testing.T) {
	tc := []struct {
		name           string
		inMessageKey   string
		inLS           model.LabelSet
		inDiscoveredLS model.LabelSet
		relabels       []*relabel.Config
		expectedLS     model.LabelSet
	}{
		{
			name:           "no relabel config",
			inMessageKey:   "foo",
			inDiscoveredLS: model.LabelSet{"__meta_kafka_foo": "bar"},
			inLS:           model.LabelSet{"buzz": "bazz"},
			relabels:       nil,
			expectedLS:     model.LabelSet{"buzz": "bazz"},
		},
		{
			name:           "message key with relabel config",
			inMessageKey:   "foo",
			inDiscoveredLS: model.LabelSet{"__meta_kafka_foo": "bar"},
			inLS:           model.LabelSet{"buzz": "bazz"},
			relabels: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__meta_kafka_message_key"},
					Regex:        relabel.MustNewRegexp("(.*)"),
					TargetLabel:  "message_key",
					Replacement:  "$1",
					Action:       "replace",
				},
			},
			expectedLS: model.LabelSet{"buzz": "bazz", "message_key": "foo"},
		},
		{
			name:           "no message key with relabel config",
			inMessageKey:   "",
			inDiscoveredLS: model.LabelSet{"__meta_kafka_foo": "bar"},
			inLS:           model.LabelSet{"buzz": "bazz"},
			relabels: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__meta_kafka_message_key"},
					Regex:        relabel.MustNewRegexp("(.*)"),
					TargetLabel:  "message_key",
					Replacement:  "$1",
					Action:       "replace",
				},
			},
			expectedLS: model.LabelSet{"buzz": "bazz", "message_key": "none"},
		},
	}
	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			session, claim := &testSession{}, newTestClaim("footopic", 10, 12)
			var closed bool
			fc := fake.New(
				func() {
					closed = true
				},
			)
			tg := NewTarget(nil, session, claim, tt.inDiscoveredLS, tt.inLS, tt.relabels, fc, true, messageParser{})

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				tg.run()
			}()

			for i := 0; i < 10; i++ {
				claim.Send(&sarama.ConsumerMessage{
					Timestamp: time.Unix(0, int64(i)),
					Value:     []byte(fmt.Sprintf("%d", i)),
					Key:       []byte(tt.inMessageKey),
				})
			}
			claim.Stop()
			wg.Wait()
			re := fc.Received()

			require.Len(t, session.markedMessage, 10)
			require.Len(t, re, 10)
			require.True(t, closed)
			for _, e := range re {
				require.Equal(t, tt.expectedLS.String(), e.Labels.String())
			}
		})
	}
}
