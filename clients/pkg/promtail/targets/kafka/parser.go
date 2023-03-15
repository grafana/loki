package kafka

import "github.com/Shopify/sarama"

type noopMessageParser struct{}

func (n noopMessageParser) Parse(message *sarama.ConsumerMessage) ([]string, error) {
	return []string{string(message.Value)}, nil
}
