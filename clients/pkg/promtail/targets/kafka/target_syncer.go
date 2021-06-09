package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/util"
)

var TopicPollInterval = 30 * time.Second

type TopicManager interface {
	Topics() ([]string, error)
}

type Consumer interface {
	getActiveTargets() []target.Target
	getDroppedTargets() []target.Target
	start(ctx context.Context, topics []string)
	stop()
}

type TargetSyncer struct {
	logger        log.Logger
	cfg           scrapeconfig.Config
	reg           prometheus.Registerer
	clientConfigs []client.Config

	topicManager TopicManager
	consumer
	close func() error

	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	previousTopics []string
}

func NewSyncer(
	reg prometheus.Registerer,
	logger log.Logger,
	cfg scrapeconfig.Config,
	clientConfigs ...client.Config,
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
	case "sticky":
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategySticky
	case "roundrobin":
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	case "range", "":
		config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	default:
		return nil, fmt.Errorf("unrecognized consumer group partition assignor: %s", cfg.KafkaConfig.Assignor)
	}
	client, err := sarama.NewClient(cfg.KafkaConfig.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %w", err)
	}
	group, err := sarama.NewConsumerGroup(cfg.KafkaConfig.Brokers, cfg.KafkaConfig.GroupID, config)
	if err != nil {
		return nil, fmt.Errorf("error creating consumer group client: %w", err)
	}
	topicManager, err := newTopicManager(client, cfg.KafkaConfig.Topics)
	if err != nil {
		return nil, fmt.Errorf("error creating topic manager: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	t := &TargetSyncer{
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
		topicManager: topicManager,
		cfg:          cfg,
		reg:          reg,
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
		clientConfigs: clientConfigs,
	}
	t.discoverer = t
	t.loop()
	return t, nil
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
	new, err := ts.topicManager.Topics()
	if err != nil {
		return nil, false, err
	}
	if len(ts.previousTopics) != len(new) {
		ts.previousTopics = new
		return new, true, nil
	}
	for i, v := range ts.previousTopics {
		if v != new[i] {
			ts.previousTopics = new
			return new, true, nil
		}
	}
	return nil, false, nil
}

func (ts *TargetSyncer) Stop() error {
	ts.cancel()
	ts.wg.Wait()
	return ts.close()
}

// NewTarget creates a new targets based on the current kafka claim and group session.
func (ts *TargetSyncer) NewTarget(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (RunnableTarget, error) {
	discoveredLabels := model.LabelSet{
		"__topic":     model.LabelValue(claim.Topic()),
		"__partition": model.LabelValue(fmt.Sprintf("%d", claim.Partition())),
		"__member_id": model.LabelValue(session.MemberID()),
	}
	labelMap := make(map[string]string)
	for k, v := range discoveredLabels.Clone().Merge(ts.cfg.KafkaConfig.Labels) {
		labelMap[string(k)] = string(v)
	}
	lbs := relabel.Process(labels.FromMap(labelMap), ts.cfg.RelabelConfigs...)
	details := newDetails(session, claim)
	labelOut := model.LabelSet(util.LabelsToMetric(lbs))
	for k := range labelOut {
		if strings.HasPrefix(string(k), "__") {
			delete(labelOut, k)
		}
	}
	if len(labelOut) == 0 {
		level.Warn(ts.logger).Log("msg", "dropping target", "reason", "no labels", "details", details, "discovered_labels", discoveredLabels.String())
		return &runnableDroppedTarget{
			Target: target.NewDroppedTarget("dropping target, no labels", discoveredLabels),
			runFn: func() {
				for range claim.Messages() {
				}
			},
		}, nil
	}

	c, err := NewFanOutHandler(
		ts.cfg.KafkaConfig.WorkerPerPartition,
		ts.logger,
		ts.reg,
		func() (api.EntryMiddleware, error) {
			return stages.NewPipeline(log.With(ts.logger, "component", "kafka_pipeline"), ts.cfg.PipelineStages, &ts.cfg.JobName, ts.reg)
		},
		ts.clientConfigs...)
	if err != nil {
		return nil, err
	}

	t := NewTarget(
		session,
		claim,
		discoveredLabels,
		labelOut,
		c,
		ts.cfg.KafkaConfig.UseIncomingTimestamp,
	)

	return t, nil
}

func validateConfig(cfg *scrapeconfig.Config) error {
	if cfg.KafkaConfig == nil {
		return errors.New("Kafka configuration is empty")
	}
	if cfg.KafkaConfig.WorkerPerPartition == 0 {
		cfg.KafkaConfig.WorkerPerPartition = 1
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

	if len(cfg.KafkaConfig.GroupID) == 0 {
		return errors.New("no Kafka consumer group defined")
	}
	return nil
}
