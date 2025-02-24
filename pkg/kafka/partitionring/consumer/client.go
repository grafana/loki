package consumer

import (
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type Client struct {
	*kgo.Client
	partitionRing ring.PartitionRingReader
	logger        log.Logger
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewGroupClient creates a new Kafka consumer group client that participates in cooperative group consumption.
// It joins the specified consumer group and consumes messages from the configured Kafka topic.
//
// The client uses a cooperative-active-sticky balancing strategy which ensures active partitions are evenly
// distributed across group members while maintaining sticky assignments for optimal processing. Inactive partitions
// are still assigned and monitored but not processed, allowing quick activation when needed. Partition handoffs
// happen cooperatively to avoid stop-the-world rebalances.
//
// The client runs a background goroutine that monitors the partition ring for changes. When the set of active
// partitions changes (e.g. due to scaling or failures), it triggers a rebalance to ensure partitions are
// properly redistributed across the consumer group members. This maintains optimal processing as the active
// partition set evolves.
func NewGroupClient(kafkaCfg kafka.Config, partitionRing ring.PartitionRingReader, groupName string, metrics *kprom.Metrics, logger log.Logger, opts ...kgo.Opt) (*Client, error) {
	defaultOpts := []kgo.Opt{
		kgo.ConsumerGroup(groupName),
		kgo.ConsumeRegex(),
		kgo.ConsumeTopics(kafkaCfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.RebalanceTimeout(5 * time.Minute),
	}

	// Combine remaining options with our defaults
	allOpts := append(defaultOpts, opts...)

	client, err := client.NewReaderClient(kafkaCfg, metrics, logger, allOpts...)
	if err != nil {
		return nil, err
	}

	c := &Client{
		Client:        client,
		partitionRing: partitionRing,
		stopCh:        make(chan struct{}),
		logger:        logger,
	}

	// Start the partition monitor goroutine
	c.wg.Add(1)
	go c.monitorPartitions()

	return c, nil
}

func (c *Client) monitorPartitions() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Get initial partition count from the ring
	lastPartitionCount := c.partitionRing.PartitionRing().PartitionsCount()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Get current partition count from the ring
			currentPartitionCount := c.partitionRing.PartitionRing().PartitionsCount()
			if currentPartitionCount != lastPartitionCount {
				level.Info(c.logger).Log(
					"msg", "partition count changed, triggering rebalance",
					"previous_count", lastPartitionCount,
					"current_count", currentPartitionCount,
				)
				// Trigger a rebalance to update partition assignments
				// All consumers trigger the rebalance, but only the group leader will actually perform it
				// For non-leader consumers, triggering the rebalance has no effect
				c.ForceRebalance()
				lastPartitionCount = currentPartitionCount
			}
		}
	}
}

func (c *Client) Close() {
	close(c.stopCh)  // Signal the monitor goroutine to stop
	c.wg.Wait()      // Wait for the monitor goroutine to exit
	c.Client.Close() // Close the underlying client
}
