package kafka

import (
	"github.com/Shopify/sarama"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/logproto"
)

const (
	defaultKafkaMessageKey  = "none"
	labelKeyKafkaMessageKey = "__meta_kafka_message_key"
)

// KafkaTarget allows making message handling independent from the internals of the specific Target internals
type KafkaTarget interface {
	// RelabelConfig returns relabel configs
	RelabelConfig() []*relabel.Config
	// Labels returns labels
	Labels() model.LabelSet
	// Client returns EntryHandler
	Client() api.EntryHandler
	// UseIncomingTimestamp tells to use a timestamp from the incoming message
	UseIncomingTimestamp() bool
	// Session returns sarama consumer group session
	Session() sarama.ConsumerGroupSession
	// Logger returns logger
	Logger() log.Logger
}

// messageHandler is a default message handler for a kafka target
func messageHandler(message *sarama.ConsumerMessage, t KafkaTarget) {
	mk := string(message.Key)
	if len(mk) == 0 {
		mk = defaultKafkaMessageKey
	}

	// TODO: Possibly need to format after merging with discovered labels because we can specify multiple labels in source labels
	// https://github.com/grafana/loki/pull/4745#discussion_r750022234
	lbs := format([]labels.Label{{
		Name:  labelKeyKafkaMessageKey,
		Value: mk,
	}}, t.RelabelConfig())

	out := t.Labels().Clone()
	if len(lbs) > 0 {
		out = out.Merge(lbs)
	}
	t.Client().Chan() <- api.Entry{
		Entry: logproto.Entry{
			Line:      string(message.Value),
			Timestamp: timestamp(t.UseIncomingTimestamp(), message.Timestamp),
		},
		Labels: out,
	}
	t.Session().MarkMessage(message, "")
}
