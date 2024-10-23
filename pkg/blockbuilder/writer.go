package blockbuilder

import (
	"context"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/kafka"
)

type kafkaConsumer struct {
	logger  log.Logger
	decoder *kafka.Decoder
}

func (c *kafkaConsumer) Write(ctx context.Context, req *push.PushRequest) error {
	return nil
}

func (c *kafkaConsumer) Commit(ctx context.Context) error {
	return nil
}
