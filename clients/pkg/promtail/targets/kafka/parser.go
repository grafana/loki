package kafka

import (
	"github.com/Shopify/sarama"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/logproto"
)

// messageParser implements MessageParser. It doesn't modify the content of the original `message.Value`.
type messageParser struct{}

func (n messageParser) Parse(message *sarama.ConsumerMessage, labels model.LabelSet, relabels []*relabel.Config, useIncomingTimestamp bool) ([]api.Entry, error) {
	return []api.Entry{
		{
			Labels: labels,
			Entry: logproto.Entry{
				Timestamp: timestamp(useIncomingTimestamp, message.Timestamp),
				Line:      string(message.Value),
			},
		},
	}, nil
}
