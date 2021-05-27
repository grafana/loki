package kafka

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"

	"github.com/grafana/loki/pkg/logproto"
)

// Consumergroup handler
type testConsumerGroupHandler struct {
	handler sarama.ConsumerGroupHandler
	ctx     context.Context
	topics  []string

	returnErr error
}

func (c *testConsumerGroupHandler) Consume(ctx context.Context, topics []string, handler sarama.ConsumerGroupHandler) error {
	if c.returnErr != nil {
		return c.returnErr
	}
	c.ctx = ctx
	c.topics = topics
	c.handler = handler
	<-ctx.Done()
	return nil
}

func (c testConsumerGroupHandler) Errors() <-chan error {
	return nil
}

func (c testConsumerGroupHandler) Close() error {
	return nil
}

type testSession struct {
	markedMessage []*sarama.ConsumerMessage
}

func (s *testSession) Claims() map[string][]int32                                               { return nil }
func (s *testSession) MemberID() string                                                         { return "foo" }
func (s *testSession) GenerationID() int32                                                      { return 10 }
func (s *testSession) MarkOffset(topic string, partition int32, offset int64, metadata string)  {}
func (s *testSession) Commit()                                                                  {}
func (s *testSession) ResetOffset(topic string, partition int32, offset int64, metadata string) {}
func (s *testSession) MarkMessage(msg *sarama.ConsumerMessage, metadata string) {
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

func Test_Syncer_Consume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &testConsumerGroupHandler{}
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		ctx:    ctx,
		cancel: cancel,
		reg:    prometheus.DefaultRegisterer,
		group:  c,
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
	require.NoError(t, ts.Stop())
	require.Equal(t, []string{"topic1", "topic2"}, c.topics)
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

func Test_Syncer_TargetsActive(t *testing.T) {
	var stopped bool
	var f *fake.Client
	DefaultClientFactory = func(reg prometheus.Registerer, logger log.Logger, cfgs ...client.Config) (client.Client, error) {
		f = fake.New(func() {
			stopped = true
		})
		return f, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &testConsumerGroupHandler{}
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		ctx:    ctx,
		cancel: cancel,
		reg:    prometheus.DefaultRegisterer,
		group:  c,
		clientConfigs: []client.Config{
			{},
		},
		cfg: scrapeconfig.Config{
			JobName: "foo",
			RelabelConfigs: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__topic"},
					TargetLabel:  "topic",
					Replacement:  "$1",
					Action:       relabel.Replace,
					Regex:        relabel.MustNewRegexp("(.*)"),
				},
			},
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               "topic1,topic2",
				Labels:               model.LabelSet{"static": "static1"},
			},
		},
	}
	claim := newTestClaim("topic1", 1, 10)
	session := &testSession{}
	go func() {
		for i := 0; i < 10; i++ {
			claim.Send(&sarama.ConsumerMessage{
				Timestamp: time.Unix(0, int64(i)),
				Value:     []byte(fmt.Sprintf("%d", i)),
			})
		}
		claim.Stop()
	}()
	require.NoError(t, ts.Setup(session))
	require.NoError(t, ts.ConsumeClaim(session, claim))

	require.NoError(t, ts.Stop())
	require.True(t, stopped)
	require.Len(t, f.Received(), 10)
	for i, e := range f.Received() {
		require.Equal(t, api.Entry{
			Labels: model.LabelSet{
				"topic":  "topic1",
				"static": "static1",
			},
			Entry: logproto.Entry{
				Timestamp: time.Unix(0, int64(i)),
				Line:      fmt.Sprintf("%d", i),
			},
		}, e)
	}
	require.Len(t, session.markedMessage, 10)
	require.Len(t, ts.getActiveTargets(), 1)
	require.Len(t, ts.getDroppedTargets(), 0)
	require.Equal(t, newDetails(session, claim), ts.getActiveTargets()[0].Details())
	require.NoError(t, ts.Cleanup(session))
	require.Len(t, ts.getActiveTargets(), 0)
	require.Len(t, ts.getDroppedTargets(), 0)
}

func Test_Syncer_TargetsDropped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &testConsumerGroupHandler{}
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		ctx:    ctx,
		cancel: cancel,
		reg:    prometheus.DefaultRegisterer,
		group:  c,
		clientConfigs: []client.Config{
			{},
		},
		cfg: scrapeconfig.Config{
			JobName:        "foo",
			RelabelConfigs: []*relabel.Config{},
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               "topic1,topic2",
			},
		},
	}
	claim := newTestClaim("topic1", 1, 10)
	session := &testSession{}
	go func() {
		for i := 0; i < 10; i++ {
			claim.Send(&sarama.ConsumerMessage{
				Timestamp: time.Unix(0, int64(i)),
				Value:     []byte(fmt.Sprintf("%d", i)),
			})
		}
		claim.Stop()
	}()
	require.NoError(t, ts.Setup(session))
	require.NoError(t, ts.ConsumeClaim(session, claim))

	require.NoError(t, ts.Stop())

	require.Len(t, session.markedMessage, 0)
	require.Len(t, ts.getActiveTargets(), 0)
	require.Len(t, ts.getDroppedTargets(), 1)
	require.NoError(t, ts.Cleanup(session))
	require.Len(t, ts.getActiveTargets(), 0)
	require.Len(t, ts.getDroppedTargets(), 0)
}

func Test_validateConfig(t *testing.T) {
	tests := []struct {
		cfg      *scrapeconfig.Config
		wantErr  bool
		expected *scrapeconfig.Config
	}{
		{
			&scrapeconfig.Config{
				KafkaConfig: nil,
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Group:  "foo",
					Topics: "bar",
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: "foo",
					Group:   "bar",
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: "foo",
					Topics:  "bar",
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: "foo",
					Topics:  "bar",
					Group:   "foo",
				},
			},
			false,
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers:            "foo",
					Topics:             "bar",
					Group:              "foo",
					WorkerPerPartition: 1,
					Version:            "2.1.1",
				},
			},
		},
	}
	for i, tt := range tests {
		tt := tt
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				require.Equal(t, tt.expected, tt.cfg)
			}
		})
	}
}
