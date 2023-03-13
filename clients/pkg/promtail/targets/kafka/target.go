package kafka

import (
	"fmt"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

type runnableDroppedTarget struct {
	target.Target
	runFn func()
}

func (d *runnableDroppedTarget) run() {
	d.runFn()
}

// MessageHandler defines processing for each incoming message
type MessageHandler func(message *sarama.ConsumerMessage, t KafkaTarget)

type Target struct {
	logger               log.Logger
	discoveredLabels     model.LabelSet
	lbs                  model.LabelSet
	details              ConsumerDetails
	claim                sarama.ConsumerGroupClaim
	session              sarama.ConsumerGroupSession
	client               api.EntryHandler
	relabelConfig        []*relabel.Config
	useIncomingTimestamp bool
	messageHandler       MessageHandler
}

func NewTarget(
	logger log.Logger,
	session sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
	discoveredLabels, lbs model.LabelSet,
	relabelConfig []*relabel.Config,
	client api.EntryHandler,
	useIncomingTimestamp bool,
	messageHandler MessageHandler,
) *Target {
	return &Target{
		logger:               logger,
		discoveredLabels:     discoveredLabels,
		lbs:                  lbs,
		details:              newDetails(session, claim),
		claim:                claim,
		session:              session,
		client:               client,
		relabelConfig:        relabelConfig,
		useIncomingTimestamp: useIncomingTimestamp,
		messageHandler:       messageHandler,
	}
}

func (t *Target) run() {
	defer t.client.Stop()
	for message := range t.claim.Messages() {
		if t.messageHandler == nil {
			messageHandler(message, t)
			continue
		}
		t.messageHandler(message, t)
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

func (t *Target) RelabelConfig() []*relabel.Config {
	return t.relabelConfig
}

func (t *Target) Client() api.EntryHandler {
	return t.client
}

func (t *Target) UseIncomingTimestamp() bool {
	return t.useIncomingTimestamp
}

func (t *Target) Session() sarama.ConsumerGroupSession {
	return t.session
}

func (t *Target) Logger() log.Logger {
	return t.logger
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

func (c ConsumerDetails) String() string {
	return fmt.Sprintf("member_id=%s generation_id=%d topic=%s partition=%d initial_offset=%d", c.MemberID, c.GenerationID, c.Topic, c.Partition, c.InitialOffset)
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
