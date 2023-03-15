package kafka

import "github.com/Shopify/sarama"

type noopParser struct{}

func (n noopParser) Parse(message *sarama.ConsumerMessage) ([]string, error) {
	return []string{string(message.Value)}, nil
}
