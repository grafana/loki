package kafka

import (
	"context"
	"errors"
	"fmt"

	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/util"
)

var TopicPollInterval = 30 * time.Second

type TopicManager interface {
	Topics() ([]string, error)
}

// TargetSyncerConfig contains specific TargetSyncer configuration.
// It allows to make the TargetSyncer creation independent from the scrape config structure.
type TargetSyncerConfig struct {
	RelabelConfigs       []*relabel.Config
	UseIncomingTimestamp bool
	Labels               model.LabelSet
	GroupID              string
}

type TargetSyncer struct {
	logger   log.Logger
	cfg      *TargetSyncerConfig
	pipeline *stages.Pipeline
	reg      prometheus.Registerer
	client   api.EntryHandler

	topicManager TopicManager
	consumer
	close func() error

	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	previousTopics []string
	messageParser  MessageParser
}

// NewSyncerFromScrapeConfig creates TargetSyncer from scrape config
func NewSyncerFromScrapeConfig(
	reg prometheus.Registerer,
	logger log.Logger,
	cfg scrapeconfig.Config,
	pushClient api.EntryHandler,
) (*TargetSyncer, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	version, err := sarama.ParseKafkaVersion(cfg.KafkaConfig.Version)
	if err != nil {
		return nil, err
	}
	config := sarama.NewConfig()
	config.Version = version
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	switch cfg.KafkaConfig.Assignor {
	case sarama.StickyBalanceStrategyName:
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategySticky
	case sarama.RoundRobinBalanceStrategyName:
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	case sarama.RangeBalanceStrategyName, "":
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	default:
		return nil, fmt.Errorf("unrecognized consumer group partition assignor: %s", cfg.KafkaConfig.Assignor)
	}
	config, err = withAuthentication(*config, cfg.KafkaConfig.Authentication)
	if err != nil {
		return nil, fmt.Errorf("error setting up kafka authentication: %w", err)
	}
	client, err := sarama.NewClient(cfg.KafkaConfig.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %w", err)
	}
	group, err := sarama.NewConsumerGroup(cfg.KafkaConfig.Brokers, cfg.KafkaConfig.GroupID, config)
	if err != nil {
		return nil, fmt.Errorf("error creating consumer group client: %w", err)
	}
	pipeline, err := stages.NewPipeline(log.With(logger, "component", "kafka_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
	if err != nil {
		return nil, fmt.Errorf("error creating pipeline: %w", err)
	}

	targetSyncConfig := &TargetSyncerConfig{
		RelabelConfigs:       cfg.RelabelConfigs,
		UseIncomingTimestamp: cfg.KafkaConfig.UseIncomingTimestamp,
		Labels:               cfg.KafkaConfig.Labels,
		GroupID:              cfg.KafkaConfig.GroupID,
	}

	t, err := NewSyncer(context.Background(), reg, logger, pushClient, pipeline, group, client, messageParser{}, cfg.KafkaConfig.Topics, targetSyncConfig)
	if err != nil {
		return nil, fmt.Errorf("error starting kafka target: %w", err)
	}
	return t, nil
}

// NewSyncer creates TargetSyncer
func NewSyncer(ctx context.Context,
	reg prometheus.Registerer,
	logger log.Logger,
	pushClient api.EntryHandler,
	pipeline *stages.Pipeline,
	group sarama.ConsumerGroup,
	client sarama.Client,
	messageParser MessageParser,
	topics []string,
	cfg *TargetSyncerConfig,
) (*TargetSyncer, error) {
	topicManager, err := newTopicManager(client, topics)
	if err != nil {
		return nil, fmt.Errorf("error creating topic manager: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	t := &TargetSyncer{
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		topicManager: topicManager,
		cfg:          cfg,
		reg:          reg,
		client:       pushClient,
		pipeline:     pipeline,
		close: func() error {
			if err := group.Close(); err != nil {
				level.Warn(logger).Log("msg", "error while closing consumer group", "err", err)
			}
			return client.Close()
		},
		consumer: consumer{
			ctx:           context.Background(),
			cancel:        func() {},
			ConsumerGroup: group,
			logger:        logger,
		},
		messageParser: messageParser,
	}

	t.discoverer = t
	t.loop()

	return t, nil
}

func withAuthentication(cfg sarama.Config, authCfg scrapeconfig.KafkaAuthentication) (*sarama.Config, error) {
	if len(authCfg.Type) == 0 || authCfg.Type == scrapeconfig.KafkaAuthenticationTypeNone {
		return &cfg, nil
	}

	switch authCfg.Type {
	case scrapeconfig.KafkaAuthenticationTypeSSL:
		return withSSLAuthentication(cfg, authCfg)
	case scrapeconfig.KafkaAuthenticationTypeSASL:
		return withSASLAuthentication(cfg, authCfg)
	default:
		return nil, fmt.Errorf("unsupported authentication type %s", authCfg.Type)
	}
}

func withSSLAuthentication(cfg sarama.Config, authCfg scrapeconfig.KafkaAuthentication) (*sarama.Config, error) {
	cfg.Net.TLS.Enable = true
	tc, err := createTLSConfig(authCfg.TLSConfig)
	if err != nil {
		return nil, err
	}
	cfg.Net.TLS.Config = tc
	return &cfg, nil
}

func withSASLAuthentication(cfg sarama.Config, authCfg scrapeconfig.KafkaAuthentication) (*sarama.Config, error) {
	cfg.Net.SASL.Enable = true
	cfg.Net.SASL.User = authCfg.SASLConfig.User
	cfg.Net.SASL.Password = authCfg.SASLConfig.Password.String()
	cfg.Net.SASL.Mechanism = authCfg.SASLConfig.Mechanism
	if cfg.Net.SASL.Mechanism == "" {
		cfg.Net.SASL.Mechanism = sarama.SASLTypePlaintext
	}

	supportedMechanism := []string{
		sarama.SASLTypeSCRAMSHA512,
		sarama.SASLTypeSCRAMSHA256,
		sarama.SASLTypePlaintext,
	}
	if !util.StringsContain(supportedMechanism, string(authCfg.SASLConfig.Mechanism)) {
		return nil, fmt.Errorf("error unsupported sasl mechanism: %s", authCfg.SASLConfig.Mechanism)
	}

	if cfg.Net.SASL.Mechanism == sarama.SASLTypeSCRAMSHA512 {
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{
				HashGeneratorFcn: SHA512,
			}
		}
	}
	if cfg.Net.SASL.Mechanism == sarama.SASLTypeSCRAMSHA256 {
		cfg.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient {
			return &XDGSCRAMClient{
				HashGeneratorFcn: SHA256,
			}
		}
	}
	if authCfg.SASLConfig.UseTLS {
		tc, err := createTLSConfig(authCfg.SASLConfig.TLSConfig)
		if err != nil {
			return nil, err
		}
		cfg.Net.TLS.Config = tc
		cfg.Net.TLS.Enable = true
	}
	return &cfg, nil
}

func (ts *TargetSyncer) loop() {
	topicChanged := make(chan []string)
	ts.wg.Add(2)
	go func() {
		defer ts.wg.Done()
		for {
			select {
			case <-ts.ctx.Done():
				return
			case topics := <-topicChanged:
				level.Info(ts.logger).Log("msg", "new topics received", "topics", fmt.Sprintf("%+v", topics))
				ts.stop()
				if len(topics) > 0 { // no topics we don't need to start.
					ts.start(ts.ctx, topics)
				}
			}
		}
	}()
	go func() {
		defer ts.wg.Done()
		ticker := time.NewTicker(TopicPollInterval)
		defer ticker.Stop()

		tick := func() {
			select {
			case <-ts.ctx.Done():
			case <-ticker.C:
			}
		}
		for ; true; tick() { // instant tick.
			if ts.ctx.Err() != nil {
				ts.stop()
				close(topicChanged)
				return
			}
			newTopics, ok, err := ts.fetchTopics()
			if err != nil {
				level.Warn(ts.logger).Log("msg", "failed to fetch topics", "err", err)
				continue
			}
			if ok {
				topicChanged <- newTopics
			}

		}
	}()
}

// fetchTopics fetches and return new topics, if there's a difference with previous found topics
// it will return true as second return value.
func (ts *TargetSyncer) fetchTopics() ([]string, bool, error) {
	newTopics, err := ts.topicManager.Topics()
	if err != nil {
		return nil, false, err
	}
	if len(ts.previousTopics) != len(newTopics) {
		ts.previousTopics = newTopics
		return newTopics, true, nil
	}
	for i, v := range ts.previousTopics {
		if v != newTopics[i] {
			ts.previousTopics = newTopics
			return newTopics, true, nil
		}
	}
	return nil, false, nil
}

func (ts *TargetSyncer) Stop() error {
	ts.cancel()
	ts.wg.Wait()
	return ts.close()
}

// ActiveTargets returns active targets from its consumer
func (ts *TargetSyncer) ActiveTargets() []target.Target {
	return ts.getActiveTargets()
}

// DroppedTargets returns dropped targets from its consumer
func (ts *TargetSyncer) DroppedTargets() []target.Target {
	return ts.getDroppedTargets()
}

// NewTarget creates a new targets based on the current kafka claim and group session.
func (ts *TargetSyncer) NewTarget(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (RunnableTarget, error) {
	discoveredLabels := model.LabelSet{
		"__meta_kafka_topic":     model.LabelValue(claim.Topic()),
		"__meta_kafka_partition": model.LabelValue(fmt.Sprintf("%d", claim.Partition())),
		"__meta_kafka_member_id": model.LabelValue(session.MemberID()),
		"__meta_kafka_group_id":  model.LabelValue(ts.cfg.GroupID),
	}
	details := newDetails(session, claim)
	labelMap := make(map[string]string)
	for k, v := range discoveredLabels.Clone().Merge(ts.cfg.Labels) {
		labelMap[string(k)] = string(v)
	}
	labelOut := format(labels.FromMap(labelMap), ts.cfg.RelabelConfigs)
	if len(labelOut) == 0 {
		level.Warn(ts.logger).Log("msg", "dropping target", "reason", "no labels", "details", details, "discovered_labels", discoveredLabels.String())
		return &runnableDroppedTarget{
			Target: target.NewDroppedTarget("dropping target, no labels", discoveredLabels),
			runFn: func() {
				for range claim.Messages() { //nolint:revive
				}
			},
		}, nil
	}
	t := NewTarget(
		ts.logger,
		session,
		claim,
		discoveredLabels,
		labelOut,
		ts.cfg.RelabelConfigs,
		ts.pipeline.Wrap(ts.client),
		ts.cfg.UseIncomingTimestamp,
		ts.messageParser,
	)

	return t, nil
}

func validateConfig(cfg *scrapeconfig.Config) error {
	if cfg.KafkaConfig == nil {
		return errors.New("Kafka configuration is empty")
	}
	if cfg.KafkaConfig.Version == "" {
		cfg.KafkaConfig.Version = "2.1.1"
	}
	if len(cfg.KafkaConfig.Brokers) == 0 {
		return errors.New("no Kafka bootstrap brokers defined")
	}

	if len(cfg.KafkaConfig.Topics) == 0 {
		return errors.New("no topics given to be consumed")
	}

	if cfg.KafkaConfig.GroupID == "" {
		cfg.KafkaConfig.GroupID = "promtail"
	}
	return nil
}
