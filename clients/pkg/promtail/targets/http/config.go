package http

import (
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/server"
)

type Config struct {
	// Server is the weaveworks server config for listening connections
	Server server.Config `yaml:"server"`

	// Labels optionally holds labels to associate with each record received on the push api.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp uses the timestamp in the received log entry. If false,
	// Promtail will assign the current timestamp to the log entry when it was processed.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`
}
