package kafka

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func Test_TopicDiscovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	group := &testConsumerGroupHandler{mu: &sync.Mutex{}}
	TopicPollInterval = time.Microsecond
	var closed bool
	client := &mockKafkaClient{
		topics: []string{"topic1"},
	}
	ts := &TargetSyncer{
		ctx:          ctx,
		cancel:       cancel,
		logger:       log.NewNopLogger(),
		reg:          prometheus.DefaultRegisterer,
		topicManager: mustNewTopicsManager(client, []string{"topic1", "topic2"}),
		close: func() error {
			closed = true
			return nil
		},
		consumer: consumer{
			ctx:           context.Background(),
			cancel:        func() {},
			ConsumerGroup: group,
			logger:        log.NewNopLogger(),
			discoverer: DiscovererFn(func(_ sarama.ConsumerGroupSession, _ sarama.ConsumerGroupClaim) (RunnableTarget, error) {
				return nil, nil
			}),
		},
	}

	ts.loop()
	tmpTopics := []string{}
	require.Eventually(t, func() bool {
		if !group.consuming.Load() {
			return false
		}
		group.mu.Lock()
		defer group.mu.Unlock()
		tmpTopics = group.topics
		return reflect.DeepEqual([]string{"topic1"}, group.topics)
	}, 200*time.Millisecond, time.Millisecond, "expected topics: %v, got: %v", []string{"topic1"}, tmpTopics)

	client.mu.Lock()
	client.topics = []string{"topic1", "topic2"} // introduce new topics
	client.mu.Unlock()

	require.Eventually(t, func() bool {
		if !group.consuming.Load() {
			return false
		}
		tmpTopics = group.topics
		return reflect.DeepEqual([]string{"topic1", "topic2"}, group.topics)
	}, 200*time.Millisecond, time.Millisecond, "expected topics: %v, got: %v", []string{"topic1", "topic2"}, tmpTopics)

	require.NoError(t, ts.Stop())
	require.True(t, closed)
}

func Test_NewTarget(t *testing.T) {
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		reg:    prometheus.DefaultRegisterer,
		client: fake.New(func() {}),
		cfg: &TargetSyncerConfig{
			GroupID: "group_1",
			RelabelConfigs: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__meta_kafka_topic"},
					TargetLabel:  "topic",
					Replacement:  "$1",
					Action:       relabel.Replace,
					Regex:        relabel.MustNewRegexp("(.*)"),
				},
			},
			Labels: model.LabelSet{"static": "static1"},
		},
	}
	jobName := "foo"
	pipeline, err := stages.NewPipeline(ts.logger, nil, &jobName, ts.reg)
	require.NoError(t, err)
	ts.pipeline = pipeline
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
	require.Equal(t, model.LabelSet{"__meta_kafka_member_id": "foo", "__meta_kafka_partition": "10", "__meta_kafka_topic": "foo", "__meta_kafka_group_id": "group_1"}, tg.DiscoveredLabels())
}

func Test_NewDroppedTarget(t *testing.T) {
	ts := &TargetSyncer{
		logger: log.NewNopLogger(),
		reg:    prometheus.DefaultRegisterer,
		cfg: &TargetSyncerConfig{
			GroupID: "group1",
		},
	}
	tg, err := ts.NewTarget(&testSession{}, newTestClaim("foo", 10, 1))

	require.NoError(t, err)
	require.Equal(t, "dropping target, no labels", tg.Details())
	require.Equal(t, model.LabelSet(nil), tg.Labels())
	require.Equal(t, model.LabelSet{"__meta_kafka_member_id": "foo", "__meta_kafka_partition": "10", "__meta_kafka_topic": "foo", "__meta_kafka_group_id": "group1"}, tg.DiscoveredLabels())
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
			false,
			&scrapeconfig.Config{
				KafkaConfig: &scrapeconfig.KafkaTargetConfig{
					Brokers: []string{"foo"},
					Topics:  []string{"bar"},
					GroupID: "promtail",
					Version: "2.1.1",
				},
			},
		},
	}

	for i, tt := range tests {
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

func Test_withAuthentication(t *testing.T) {
	var (
		tlsConf = config.TLSConfig{
			CAFile:             "testdata/example.com.ca.pem",
			CertFile:           "testdata/example.com.pem",
			KeyFile:            "testdata/example.com-key.pem",
			ServerName:         "example.com",
			InsecureSkipVerify: true,
		}
		expectedTLSConf, _ = createTLSConfig(config.TLSConfig{
			CAFile:             "testdata/example.com.ca.pem",
			CertFile:           "testdata/example.com.pem",
			KeyFile:            "testdata/example.com-key.pem",
			ServerName:         "example.com",
			InsecureSkipVerify: true,
		})
		cfg = sarama.NewConfig()
	)

	// no authentication
	noAuthCfg, err := withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: scrapeconfig.KafkaAuthenticationTypeNone,
	})
	assert.Nil(t, err)
	assert.Equal(t, false, noAuthCfg.Net.TLS.Enable)
	assert.Equal(t, false, noAuthCfg.Net.SASL.Enable)
	assert.NoError(t, noAuthCfg.Validate())

	// specify unsupported auth type
	illegalAuthTypeCfg, err := withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: "illegal",
	})
	assert.NotNil(t, err)
	assert.Nil(t, illegalAuthTypeCfg)

	// mTLS authentication
	mTLSCfg, err := withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type:      scrapeconfig.KafkaAuthenticationTypeSSL,
		TLSConfig: tlsConf,
	})
	assert.Nil(t, err)
	assert.Equal(t, true, mTLSCfg.Net.TLS.Enable)
	assert.NotNil(t, mTLSCfg.Net.TLS.Config)
	assert.Equal(t, "example.com", mTLSCfg.Net.TLS.Config.ServerName)
	assert.Equal(t, true, mTLSCfg.Net.TLS.Config.InsecureSkipVerify)
	assert.Equal(t, expectedTLSConf.Certificates, mTLSCfg.Net.TLS.Config.Certificates)
	assert.NotNil(t, mTLSCfg.Net.TLS.Config.RootCAs)
	assert.NoError(t, mTLSCfg.Validate())

	// mTLS authentication expect ignore sasl
	mTLSCfg, err = withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type:      scrapeconfig.KafkaAuthenticationTypeSSL,
		TLSConfig: tlsConf,
		SASLConfig: scrapeconfig.KafkaSASLConfig{
			Mechanism: sarama.SASLTypeSCRAMSHA256,
			User:      "user",
			Password:  flagext.SecretWithValue("pass"),
			UseTLS:    false,
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, false, mTLSCfg.Net.SASL.Enable)

	// SASL/PLAIN
	saslCfg, err := withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: scrapeconfig.KafkaAuthenticationTypeSASL,
		SASLConfig: scrapeconfig.KafkaSASLConfig{
			Mechanism: sarama.SASLTypePlaintext,
			User:      "user",
			Password:  flagext.SecretWithValue("pass"),
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, false, saslCfg.Net.TLS.Enable)
	assert.Equal(t, true, saslCfg.Net.SASL.Enable)
	assert.Equal(t, "user", saslCfg.Net.SASL.User)
	assert.Equal(t, "pass", saslCfg.Net.SASL.Password)
	assert.Equal(t, sarama.SASLTypePlaintext, string(saslCfg.Net.SASL.Mechanism))
	assert.NoError(t, saslCfg.Validate())

	// SASL/SCRAM
	saslCfg, err = withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: scrapeconfig.KafkaAuthenticationTypeSASL,
		SASLConfig: scrapeconfig.KafkaSASLConfig{
			Mechanism: sarama.SASLTypeSCRAMSHA512,
			User:      "user",
			Password:  flagext.SecretWithValue("pass"),
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, false, saslCfg.Net.TLS.Enable)
	assert.Equal(t, true, saslCfg.Net.SASL.Enable)
	assert.Equal(t, "user", saslCfg.Net.SASL.User)
	assert.Equal(t, "pass", saslCfg.Net.SASL.Password)
	assert.Equal(t, sarama.SASLTypeSCRAMSHA512, string(saslCfg.Net.SASL.Mechanism))
	assert.NoError(t, saslCfg.Validate())

	// SASL unsupported mechanism
	_, err = withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: scrapeconfig.KafkaAuthenticationTypeSASL,
		SASLConfig: scrapeconfig.KafkaSASLConfig{
			Mechanism: sarama.SASLTypeGSSAPI,
			User:      "user",
			Password:  flagext.SecretWithValue("pass"),
		},
	})
	assert.Error(t, err)
	assert.Equal(t, err.Error(), "error unsupported sasl mechanism: GSSAPI")

	// SASL over TLS
	saslCfg, err = withAuthentication(*cfg, scrapeconfig.KafkaAuthentication{
		Type: scrapeconfig.KafkaAuthenticationTypeSASL,
		SASLConfig: scrapeconfig.KafkaSASLConfig{
			Mechanism: sarama.SASLTypeSCRAMSHA512,
			User:      "user",
			Password:  flagext.SecretWithValue("pass"),
			UseTLS:    true,
			TLSConfig: tlsConf,
		},
	})
	assert.Nil(t, err)
	assert.Equal(t, true, saslCfg.Net.TLS.Enable)
	assert.Equal(t, true, saslCfg.Net.SASL.Enable)
	assert.NotNil(t, saslCfg.Net.TLS.Config)
	assert.Equal(t, "example.com", saslCfg.Net.TLS.Config.ServerName)
	assert.Equal(t, true, saslCfg.Net.TLS.Config.InsecureSkipVerify)
	assert.Equal(t, expectedTLSConf.Certificates, saslCfg.Net.TLS.Config.Certificates)
	assert.NotNil(t, saslCfg.Net.TLS.Config.RootCAs)
	assert.NoError(t, saslCfg.Validate())
}
