package partitionring

import (
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

// KafkaClient combines a [kgo.Client] with awareness of the partition ring.
// It uses a special cooperative sticky balancer that operates over the
// set of active partitions in the partition ring to ensure fair distribution
// work, while consuming inactive partitions in a round-robin fashion to ensure
// inactive partitions are drained.
//
// Each client instance runs a background goroutine that watches the partition
// ring for changes. When a partition becomes active or inactive, the goroutine
// triggers a rebalance to ensure the set of active partitions remains fairly
// distributed across all members of the consumer group.
type KafkaClient struct {
	*kgo.Client
	ringReader ring.PartitionRingReader
	logger     log.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewKafkaClient returns a new [KafkaClient].
func NewKafkaClient(
	cfg kafka.Config,
	ringReader ring.PartitionRingReader,
	group string,
	logger log.Logger,
	reg prometheus.Registerer,
	opts ...kgo.Opt,
) (*KafkaClient, error) {
	defaultOpts := []kgo.Opt{
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.Balancers(newCooperativeActiveStickyBalancer(ringReader)),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.RebalanceTimeout(5 * time.Minute),
	}
	// Combine remaining options with our defaults.
	allOpts := append(defaultOpts, opts...)
	client, err := client.NewReaderClient(group, cfg, logger, reg, allOpts...)
	if err != nil {
		return nil, err
	}
	c := &KafkaClient{
		Client:     client,
		ringReader: ringReader,
		stopCh:     make(chan struct{}),
		logger:     logger,
	}
	// Start the partition monitor goroutine
	c.wg.Add(1)
	go c.monitorPartitions()
	return c, nil
}

func (c *KafkaClient) monitorPartitions() {
	defer c.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Get initial partition count from the ring
	lastPartitionCount := c.ringReader.PartitionRing().PartitionsCount()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			// Get current partition count from the ring
			currentPartitionCount := c.ringReader.PartitionRing().PartitionsCount()
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

func (c *KafkaClient) Close() {
	close(c.stopCh)  // Signal the monitor goroutine to stop
	c.wg.Wait()      // Wait for the monitor goroutine to exit
	c.Client.Close() // Close the underlying client
}
