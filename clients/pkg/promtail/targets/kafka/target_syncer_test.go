package kafka

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_TopicDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	group := &testConsumerGroupHandler{}
	TopicPollInterval = time.Microsecond
	var closed bool
	kClient := &mockKafkaClient{
		topics: []string{"topic1"},
	}
	ts := &TargetSyncer{
		ctx:          ctx,
		cancel:       cancel,
		logger:       log.NewNopLogger(),
		reg:          prometheus.DefaultRegisterer,
		topicManager: mustNewTopicsManager(kClient, []string{"topic1", "topic2"}),
		close: func() error {
			closed = true
			return nil
		},
		clientConfigs: []client.Config{
			{},
		},
		consumer: consumer{
			ctx:           context.Background(),
			cancel:        func() {},
			ConsumerGroup: group,
			logger:        log.NewNopLogger(),
			discoverer: DiscovererFn(func(s sarama.ConsumerGroupSession, c sarama.ConsumerGroupClaim) (RunnableTarget, error) {
				return nil, nil
			}),
		},
		cfg: scrapeconfig.Config{
			JobName:        "foo",
			RelabelConfigs: []*relabel.Config{},
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               []string{"topic1", "topic2"},
			},
		},
	}

	ts.loop()
	require.Eventually(t, func() bool {
		if !group.consuming.Load() {
			return false
		}
		return assert.Equal(t, group.topics, []string{"topic1"})
	}, 200*time.Millisecond, time.Millisecond)

	kClient.topics = []string{"topic1", "topic2"} // introduce new topics

	require.Eventually(t, func() bool {
		if !group.consuming.Load() {
			return false
		}
		return assert.Equal(t, group.topics, []string{"topic1", "topic2"})
	}, 200*time.Millisecond, time.Millisecond)

	require.NoError(t, ts.Stop())
	require.True(t, closed)
}

func Test_NewTarget(t *testing.T) {
	DefaultClientFactory = func(reg prometheus.Registerer, logger log.Logger, cfgs ...client.Config) (client.Client, error) {
		return fake.New(func() {}), nil
	}
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		reg:    prometheus.DefaultRegisterer,
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
				Topics:               []string{"topic1", "topic2"},
				Labels:               model.LabelSet{"static": "static1"},
			},
		},
	}
	tg, err := ts.NewTarget(&testSession{}, newTestClaim("foo", 10, 1))

	require.NoError(t, err)
	require.Equal(t, ConsumerDetails{
		MemberID:      "foo",
		GenerationID:  10,
		Topic:         "foo",
		Partition:     10,
		InitialOffset: 1,
	}, tg.Details())
	require.Equal(t, model.LabelSet{"static": "static1", "topic": "foo"}, tg.Labels())
	require.Equal(t, model.LabelSet{"__member_id": "foo", "__partition": "10", "__topic": "foo"}, tg.DiscoveredLabels())
}

func Test_NewDroppedTarget(t *testing.T) {
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		reg:    prometheus.DefaultRegisterer,
		cfg: scrapeconfig.Config{
			JobName: "foo",
			KafkaConfig: &scrapeconfig.KafkaTargetConfig{
				WorkerPerPartition:   1,
				UseIncomingTimestamp: true,
				Topics:               []string{"topic1", "topic2"},
			},
		},
	}
	tg, err := ts.NewTarget(&testSession{}, newTestClaim("foo", 10, 1))

	require.NoError(t, err)
	require.Equal(t, "dropping target, no labels", tg.Details())
	require.Equal(t, model.LabelSet(nil), tg.Labels())
	require.Equal(t, model.LabelSet{"__member_id": "foo", "__partition": "10", "__topic": "foo"}, tg.DiscoveredLabels())
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
					GroupID: "foo",
					Topics:  []string{"bar"},
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: []string{"foo"},
					GroupID: "bar",
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: []string{"foo"},
					Topics:  []string{"bar"},
				},
			},
			true,
			nil,
		},
		{
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: []string{"foo"},
					Topics:  []string{"bar"},
					GroupID: "foo",
				},
			},
			false,
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers:            []string{"foo"},
					Topics:             []string{"bar"},
					GroupID:            "foo",
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
