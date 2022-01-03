package objectstore

import (
	"github.com/grafana/loki/pkg/storage/chunk"
)

type Client interface {
	ReceiveMessage(timeout int64) ([]messageObject, error)
	acknowledgeMessage(message interface{}) ackMessage
}

type ackMessage func() error

type messageObject struct {
	Object      chunk.StorageObject
	Acknowledge ackMessage
}
