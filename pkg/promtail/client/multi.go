package client

import (
	"errors"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
)

// MultiClient is client pushing to one or more loki instances.
type MultiClient []Client

// NewMulti creates a new client
func NewMulti(logger log.Logger, cfgs ...Config) (Client, error) {
	if len(cfgs) == 0 {
		return nil, errors.New("at least one client config should be provided")
	}
	var clients []Client
	for _, cfg := range cfgs {
		clients = append(clients, New(cfg, logger))
	}
	return MultiClient(clients), nil
}

// Handle Implements api.EntryHandler
func (m MultiClient) Handle(labels model.LabelSet, time time.Time, entry string) error {
	var result util.MultiError
	for _, client := range m {
		if err := client.Handle(labels, time, entry); err != nil {
			result.Add(err)
		}
	}
	return result.Err()
}

// Stop implements Client
func (m MultiClient) Stop() {
	for _, c := range m {
		c.Stop()
	}
}
