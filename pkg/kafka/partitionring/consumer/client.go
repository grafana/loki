package consumer

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type Client struct {
	*kgo.Client
	logger log.Logger
	wg     sync.WaitGroup
	stopCh chan struct{}
}

// NewGroupClient creates a new Kafka consumer group client that participates in cooperative group consumption.
// It joins the specified consumer group and consumes messages from the configured Kafka topic.
func NewGroupClient(kafkaCfg kafka.Config, groupName string, codec distributor.TenantPrefixCodec, metrics *kprom.Metrics, logger log.Logger, opts ...kgo.Opt) (*Client, error) {
	defaultOpts := []kgo.Opt{
		kgo.ConsumerGroup(groupName),
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
		Client: client,
		logger: logger,
		stopCh: make(chan struct{}),
		wg:     sync.WaitGroup{},
	}

	c.wg.Add(1)
	go c.checkTopicThroughputAndSubscribe(&c.wg, groupName, codec)

	return c, nil
}

type topicOffsets struct {
	currentOffset       int64
	lastCommittedOffset int64
}

func (c *Client) checkTopicThroughputAndSubscribe(wg *sync.WaitGroup, groupName string, codec distributor.TenantPrefixCodec) {
	defer wg.Done()

	c.checkTopicLag(groupName, codec)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.checkTopicLag(groupName, codec)
		}
	}
}

func (c *Client) checkTopicLag(groupName string, codec distributor.TenantPrefixCodec) {
	level.Info(c.logger).Log("msg", "checking topic lag")
	topics := listRelevantTopics(c, codec)
	if topics == nil || len(topics) == 0 {
		level.Info(c.logger).Log("msg", "no topics found")
		return
	}

	level.Info(c.logger).Log("msg", "topics", "topics", topics)

	listLatestOffsets(c, topics)
	listCommittedOffsets(c, topics, groupName)

	consumedTopics := c.Client.GetConsumeTopics()
	consumedTopicsMap := map[string]struct{}{}
	for _, topic := range consumedTopics {
		consumedTopicsMap[topic] = struct{}{}
	}
	toAdd := []string{}
	toRemove := []string{}
	for topic, offsets := range topics {
		level.Info(c.logger).Log("msg", "topic lag determined", "topic", topic, "currentOffset", offsets.currentOffset, "lastCommittedOffset", offsets.lastCommittedOffset, "lag", offsets.currentOffset-offsets.lastCommittedOffset)
		diff := offsets.currentOffset - offsets.lastCommittedOffset
		if diff < 0 {
			// We fell off the end, so consume the topic
			if _, ok := consumedTopicsMap[topic]; !ok {
				level.Info(c.logger).Log("msg", "adding topic to consume because we last push was less than our current offset, so probably we're behind", "topic", topic)
				toAdd = append(toAdd, topic)
			}
		} else if diff > 10_000_000 {
			// We have ~100k records to consume; hopefully that is almost enough to fill an object so start consuming.
			if _, ok := consumedTopicsMap[topic]; !ok {
				level.Info(c.logger).Log("msg", "adding topic to consume because there are 100k records since last commit", "topic", topic)
				toAdd = append(toAdd, topic)
			}
		} else {
			if _, ok := consumedTopicsMap[topic]; ok {
				level.Info(c.logger).Log("msg", "removing topic from consume because our lag is small", "topic", topic)
				toRemove = append(toRemove, topic)
			}
		}
	}

	level.Info(c.logger).Log("msg", "adding topics to consume", "topics", strings.Join(toAdd, ", "))
	level.Info(c.logger).Log("msg", "removing topics from consume", "topics", strings.Join(toRemove, ", "))
	c.Client.AddConsumeTopics(toAdd...)
	c.Client.PurgeTopicsFromConsuming(toRemove...)
	level.Info(c.logger).Log("msg", "now consuming topics", "topics", strings.Join(c.Client.GetConsumeTopics(), ", "))
}

func listRelevantTopics(c *Client, codec distributor.TenantPrefixCodec) map[string]*topicOffsets {
	topics := map[string]*topicOffsets{}
	req := kmsg.NewPtrMetadataRequest()
	req.Topics = nil // Nil to fetch all topics

	response, err := c.Client.Request(context.Background(), req)
	if err != nil {
		level.Error(c.logger).Log("msg", "error fetching metadata", "err", err)
		return nil
	}
	mdResp, ok := response.(*kmsg.MetadataResponse)
	if !ok {
		level.Error(c.logger).Log("msg", "unexpected response type")
		return nil
	}
	for _, topic := range mdResp.Topics {
		if topic.IsInternal {
			continue
		}
		_, _, err := codec.Decode(*topic.Topic)
		if err != nil {
			continue
		}
		topics[*topic.Topic] = &topicOffsets{}
	}
	return topics
}

func listLatestOffsets(c *Client, topics map[string]*topicOffsets) {
	r := kmsg.NewPtrListOffsetsRequest()

	for topic := range topics {
		r.Topics = append(r.Topics, kmsg.ListOffsetsRequestTopic{
			Topic: topic,
			Partitions: []kmsg.ListOffsetsRequestTopicPartition{
				{
					Partition:          0,
					Timestamp:          -1, // Latest offset
					CurrentLeaderEpoch: -1,
				},
			},
		})
	}
	responses := c.Client.RequestSharded(context.Background(), r)
	for _, response := range responses {
		if response.Err != nil {
			level.Error(c.logger).Log("msg", "error fetching list offsets", "err", response.Err)
			continue
		}
		offsetResp, ok := response.Resp.(*kmsg.ListOffsetsResponse)
		if !ok {
			level.Error(c.logger).Log("msg", "unexpected response type", "type", reflect.TypeOf(response.Resp))
			continue
		}
		for _, topic := range offsetResp.Topics {
			if topic.Partitions[0].ErrorCode != 0 {
				level.Error(c.logger).Log("msg", "error fetching list offsets", "err", topic.Partitions[0].ErrorCode)
				continue
			}
			topics[topic.Topic].currentOffset = topic.Partitions[0].Offset
		}
	}
}

func listCommittedOffsets(c *Client, topics map[string]*topicOffsets, groupName string) {
	offsetReq := kmsg.NewPtrOffsetFetchRequest()
	for topic := range topics {
		offsetReq.Topics = append(offsetReq.Topics, kmsg.OffsetFetchRequestTopic{
			Topic:      topic,
			Partitions: []int32{0},
		})
	}
	offsetReq.Group = groupName

	responses := c.Client.RequestSharded(context.Background(), offsetReq)
	for _, response := range responses {
		if response.Err != nil {
			level.Error(c.logger).Log("msg", "error fetching metadata", "err", response.Err)
			continue
		}
		offsetResp, ok := response.Resp.(*kmsg.OffsetFetchResponse)
		if !ok {
			level.Error(c.logger).Log("msg", "unexpected response type", "type", reflect.TypeOf(response.Resp))
			continue
		}
		for _, topic := range offsetResp.Topics {
			if len(topic.Partitions) > 1 {
				level.Warn(c.logger).Log("msg", "multiple partitions found for topic", "topic", topic.Topic)
			}
			for _, partition := range topic.Partitions {
				if partition.ErrorCode != 0 {
					level.Error(c.logger).Log("msg", "error fetching offset fetch", "err", partition.ErrorCode)
					continue
				}
				topics[topic.Topic].lastCommittedOffset = partition.Offset
			}
		}
	}
}

func (c *Client) Close() {
	close(c.stopCh)  // Signal the monitor goroutine to stop
	c.wg.Wait()      // Wait for the monitor goroutine to exit
	c.Client.Close() // Close the underlying client
}
