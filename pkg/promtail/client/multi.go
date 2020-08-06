package client

import (
	"errors"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/flagext"
)

// MultiClient is client pushing to one or more loki instances.
type MultiClient []Client

// NewMulti creates a new client
func NewMulti(logger log.Logger, externalLabels flagext.LabelSet, cfgs ...Config) (Client, error) {
	if len(cfgs) == 0 {
		return nil, errors.New("at least one client config should be provided")
	}

	clients := make([]Client, 0, len(cfgs))
	for _, cfg := range cfgs {
		// Merge the provided external labels from the single client config/command line with each client config from
		// `clients`. This is done to allow --client.external-labels=key=value passed at command line to apply to all clients
		// The order here is specified to allow the yaml to override the command line flag if there are any labels
		// which exist in both the command line arguments as well as the yaml, and while this is
		// not typically the order of precedence, the assumption here is someone providing a specific config in
		// yaml is doing so explicitly to make a key specific to a client.
		cfg.ExternalLabels = flagext.LabelSet{externalLabels.Merge(cfg.ExternalLabels.LabelSet)}
		client, err := New(cfg, logger)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
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
