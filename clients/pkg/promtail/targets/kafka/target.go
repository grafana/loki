package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	cortexutil "github.com/cortexproject/cortex/pkg/util"
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

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

var defaultBackOff = cortexutil.BackoffConfig{
	MinBackoff: 1 * time.Second,
	MaxBackoff: 60 * time.Second,
	MaxRetries: 20,
}

type TargetSyncer struct {
	logger        log.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	cfg           scrapeconfig.Config
	reg           prometheus.Registerer
	clientConfigs []client.Config

	group sarama.ConsumerGroup

	mutex sync.Mutex // used during rebalancing setup and tear down

	activeTargets  []target.Target
	droppedTargets []target.Target
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
	client, err := sarama.NewConsumerGroup(strings.Split(cfg.KafkaConfig.Brokers, ","), cfg.KafkaConfig.Group, config)
	if err != nil {
		return nil, fmt.Errorf("error creating consumer group client: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	t := &TargetSyncer{
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		group:         client,
		cfg:           cfg,
		reg:           reg,
		clientConfigs: clientConfigs,
	}
	t.consume()
	return t, nil
}

func (ts *TargetSyncer) consume() {
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		backoff := cortexutil.NewBackoff(ts.ctx, defaultBackOff)
		for {
			// Calling Consume in an infinite loop in case rebalancing is kicking in.
			// In which case all claims will be renewed.
			if err := ts.group.Consume(ts.ctx, strings.Split(ts.cfg.KafkaConfig.Topics, ","), ts); err != nil {
				level.Error(ts.logger).Log("msg", "error from the consumer, retrying in 5s", "err", err)
				// backoff before re-trying.
				backoff.Wait()
				if backoff.Ongoing() {
					continue
				}
				level.Error(ts.logger).Log("msg", "maximun error from the consumer reached", "last_err", err)
				ts.resetTargets()
				return
			}
			if ts.ctx.Err() != nil {
				return
			}
			backoff.Reset()
		}
	}()
}

func (ts *TargetSyncer) Stop() error {
	ts.cancel()
	ts.wg.Wait()
	return ts.group.Close()
}

func (ts *TargetSyncer) resetTargets() {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.activeTargets = nil
	ts.droppedTargets = nil
}

func (ts *TargetSyncer) getActiveTargets() []target.Target {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.activeTargets
}

func (ts *TargetSyncer) getDroppedTargets() []target.Target {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.droppedTargets
}

// Setup is run at the beginning of a new session, before ConsumeClaim
func (ts *TargetSyncer) Setup(session sarama.ConsumerGroupSession) error {
	ts.resetTargets()
	return nil
}

// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
func (ts *TargetSyncer) Cleanup(sarama.ConsumerGroupSession) error {
	ts.resetTargets()
	return nil
}

// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages().
func (ts *TargetSyncer) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
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
		ts.addDroppedTarget(target.NewDroppedTarget("dropping target, no labels", discoveredLabels))
		for range claim.Messages() {
		}
		return nil
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
		return err
	}
	defer c.Stop()

	t := NewTarget(
		session,
		claim,
		discoveredLabels,
		labelOut,
		c,
		ts.cfg.KafkaConfig.UseIncomingTimestamp,
	)
	ts.addTarget(t)
	level.Info(ts.logger).Log("msg", "consuming topic", "details", details)

	t.run()

	return nil
}

func (ts *TargetSyncer) addTarget(t target.Target) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.activeTargets = append(ts.activeTargets, t)
}

func (ts *TargetSyncer) addDroppedTarget(t target.Target) {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.droppedTargets = append(ts.droppedTargets, t)
}

type ConsumerDetails struct {

	// MemberID returns the cluster member ID.
	MemberID string

	// GenerationID returns the current generation ID.
	GenerationID int32

	Topic         string
	Partition     int32
	InitialOffset int64
}

func newDetails(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) ConsumerDetails {
	return ConsumerDetails{
		MemberID:      session.MemberID(),
		GenerationID:  session.GenerationID(),
		Topic:         claim.Topic(),
		Partition:     claim.Partition(),
		InitialOffset: claim.InitialOffset(),
	}
}

type Target struct {
	discoveredLabels     model.LabelSet
	lbs                  model.LabelSet
	details              ConsumerDetails
	claim                sarama.ConsumerGroupClaim
	session              sarama.ConsumerGroupSession
	client               api.EntryHandler
	useIncomingTimestamp bool
}

func NewTarget(
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
	discoveredLabels, lbs model.LabelSet,
	client api.EntryHandler,
	useIncomingTimestamp bool,
) *Target {
	return &Target{
		discoveredLabels:     discoveredLabels,
		lbs:                  lbs,
		details:              newDetails(session, claim),
		claim:                claim,
		session:              session,
		client:               client,
		useIncomingTimestamp: useIncomingTimestamp,
	}
}

func (t *Target) run() {
	for message := range t.claim.Messages() {
		t.client.Chan() <- api.Entry{
			Entry: logproto.Entry{
				Line:      string(message.Value),
				Timestamp: timestamp(t.useIncomingTimestamp, message.Timestamp),
			},
			Labels: t.lbs.Clone(),
		}
		t.session.MarkMessage(message, "")
	}
}

func timestamp(useIncoming bool, incoming time.Time) time.Time {
	if useIncoming {
		return incoming
	}
	return time.Now()
}

func (t *Target) Type() target.TargetType {
	return target.KafkaTargetType
}

func (t *Target) Ready() bool {
	return true
}

func (t *Target) DiscoveredLabels() model.LabelSet {
	return t.discoveredLabels
}

func (t *Target) Labels() model.LabelSet {
	return t.lbs
}

// Details returns target-specific details.
func (t *Target) Details() interface{} {
	return t.details
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
	if len(cfg.KafkaConfig.Group) == 0 {
		return errors.New("no Kafka consumer group defined")
	}
	return nil
}
