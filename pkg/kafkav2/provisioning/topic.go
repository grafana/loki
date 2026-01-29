package provisioning

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
)

// A TopicEntry contains a single topic.
type TopicEntry struct {
	Name              string `json:"name" yaml:"name"`
	Partitions        int32  `json:"partitions" yaml:"partitions"`
	ReplicationFactor int16  `json:"replication_factor" yaml:"replication_factor"`
}

// A TopicProvisioner provisions Kafka topics.
type TopicProvisioner struct {
	client *kadm.Client
}

// NewTopicProvisioner returns a new TopicProvisioner.
func NewTopicProvisioner(client *kadm.Client) *TopicProvisioner {
	return &TopicProvisioner{
		client: client,
	}
}

// Provision provisions each topic in entries. It returns an error if any
// topic could be not be created.
func (p *TopicProvisioner) Provision(ctx context.Context, entries []TopicEntry) error {
	for _, entry := range entries {
		if err := p.ProvisionTopic(ctx, entry.Name, entry.Partitions, entry.ReplicationFactor); err != nil {
			return fmt.Errorf("failed to provision topic %s: %w", entry.Name, err)
		}
	}
	return nil
}

// ProvisionTopic provisions a Kafka topic.
func (p *TopicProvisioner) ProvisionTopic(ctx context.Context, topic string, partitions int32, replicationFactor int16) error {
	resp, err := p.client.CreateTopics(ctx, partitions, replicationFactor, nil, topic)
	if err != nil {
		// err will be non-nil if the request could not be sent, or a response
		// was not received from the broker.
		return err
	}
	// err will be non-nil if the a request and response was received, but
	// the topic could not be created. For example, if the topic already exists.
	if err = resp.Error(); err != nil {
		return err
	}
	return nil
}
