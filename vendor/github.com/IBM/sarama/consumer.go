package sarama

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rcrowley/go-metrics"
)

// ConsumerMessage encapsulates a Kafka message returned by the consumer.
type ConsumerMessage struct {
	Headers        []*RecordHeader // only set if kafka is version 0.11+
	Timestamp      time.Time       // only set if kafka is version 0.10+, inner message timestamp
	BlockTimestamp time.Time       // only set if kafka is version 0.10+, outer (compressed) block timestamp

	Key, Value []byte
	Topic      string
	Partition  int32
	Offset     int64
}

// ConsumerError is what is provided to the user when an error occurs.
// It wraps an error and includes the topic and partition.
type ConsumerError struct {
	Topic     string
	Partition int32
	Err       error
}

func (ce ConsumerError) Error() string {
	return fmt.Sprintf("kafka: error while consuming %s/%d: %s", ce.Topic, ce.Partition, ce.Err)
}

func (ce ConsumerError) Unwrap() error {
	return ce.Err
}

// ConsumerErrors is a type that wraps a batch of errors and implements the Error interface.
// It can be returned from the PartitionConsumer's Close methods to avoid the need to manually drain errors
// when stopping.
type ConsumerErrors []*ConsumerError

func (ce ConsumerErrors) Error() string {
	return fmt.Sprintf("kafka: %d errors while consuming", len(ce))
}

// Consumer manages PartitionConsumers which process Kafka messages from brokers. You MUST call Close()
// on a consumer to avoid leaks, it will not be garbage-collected automatically when it passes out of
// scope.
type Consumer interface {
	// Topics returns the set of available topics as retrieved from the cluster
	// metadata. This method is the same as Client.Topics(), and is provided for
	// convenience.
	Topics() ([]string, error)

	// Partitions returns the sorted list of all partition IDs for the given topic.
	// This method is the same as Client.Partitions(), and is provided for convenience.
	Partitions(topic string) ([]int32, error)

	// ConsumePartition creates a PartitionConsumer on the given topic/partition with
	// the given offset. It will return an error if this Consumer is already consuming
	// on the given topic/partition. Offset can be a literal offset, or OffsetNewest
	// or OffsetOldest
	ConsumePartition(topic string, partition int32, offset int64) (PartitionConsumer, error)

	// HighWaterMarks returns the current high water marks for each topic and partition.
	// Consistency between partitions is not guaranteed since high water marks are updated separately.
	HighWaterMarks() map[string]map[int32]int64

	// Close shuts down the consumer. It must be called after all child
	// PartitionConsumers have already been closed.
	Close() error

	// Pause suspends fetching from the requested partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	Pause(topicPartitions map[string][]int32)

	// Resume resumes specified partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	Resume(topicPartitions map[string][]int32)

	// PauseAll suspends fetching from all partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	PauseAll()

	// ResumeAll resumes all partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	ResumeAll()
}

// max time to wait for more partition subscriptions
const partitionConsumersBatchTimeout = 100 * time.Millisecond

type consumer struct {
	conf            *Config
	children        map[string]map[int32]*partitionConsumer
	brokerConsumers map[*Broker]*brokerConsumer
	client          Client
	metricRegistry  metrics.Registry
	lock            sync.Mutex
}

// NewConsumer creates a new consumer using the given broker addresses and configuration.
func NewConsumer(addrs []string, config *Config) (Consumer, error) {
	client, err := NewClient(addrs, config)
	if err != nil {
		return nil, err
	}
	return newConsumer(client)
}

// NewConsumerFromClient creates a new consumer using the given client. It is still
// necessary to call Close() on the underlying client when shutting down this consumer.
func NewConsumerFromClient(client Client) (Consumer, error) {
	// For clients passed in by the client, ensure we don't
	// call Close() on it.
	cli := &nopCloserClient{client}
	return newConsumer(cli)
}

func newConsumer(client Client) (Consumer, error) {
	// Check that we are not dealing with a closed Client before processing any other arguments
	if client.Closed() {
		return nil, ErrClosedClient
	}

	c := &consumer{
		client:          client,
		conf:            client.Config(),
		children:        make(map[string]map[int32]*partitionConsumer),
		brokerConsumers: make(map[*Broker]*brokerConsumer),
		metricRegistry:  newCleanupRegistry(client.Config().MetricRegistry),
	}

	return c, nil
}

func (c *consumer) Close() error {
	c.metricRegistry.UnregisterAll()
	return c.client.Close()
}

func (c *consumer) Topics() ([]string, error) {
	return c.client.Topics()
}

func (c *consumer) Partitions(topic string) ([]int32, error) {
	return c.client.Partitions(topic)
}

func (c *consumer) ConsumePartition(topic string, partition int32, offset int64) (PartitionConsumer, error) {
	child := &partitionConsumer{
		consumer:             c,
		conf:                 c.conf,
		topic:                topic,
		partition:            partition,
		messages:             make(chan *ConsumerMessage, c.conf.ChannelBufferSize),
		errors:               make(chan *ConsumerError, c.conf.ChannelBufferSize),
		feeder:               make(chan *partitionConsumerResponse, 1),
		leaderEpoch:          invalidLeaderEpoch,
		preferredReadReplica: invalidPreferredReplicaID,
		trigger:              make(chan none, 1),
		dying:                make(chan none),
		dispatcherStop:       make(chan none),
		fetchSize:            c.conf.Consumer.Fetch.Default,
	}

	if err := child.chooseStartingOffset(offset); err != nil {
		return nil, err
	}

	leader, epoch, err := c.client.LeaderAndEpoch(child.topic, child.partition)
	if err != nil {
		return nil, err
	}

	if err := c.addChild(child); err != nil {
		return nil, err
	}

	go withRecover(child.dispatcher)
	go withRecover(child.responseFeeder)

	child.leaderEpoch = epoch
	for {
		child.broker = c.refBrokerConsumer(leader)
		child.brokerSubscription = newBrokerSubscription(child)
		if child.broker.queueSubscription(child.brokerSubscription) {
			break
		}

		child.brokerSubscription.release()
		c.unrefBrokerConsumer(child.broker)
	}

	return child, nil
}

func (c *consumer) HighWaterMarks() map[string]map[int32]int64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	hwms := make(map[string]map[int32]int64)
	for topic, p := range c.children {
		hwm := make(map[int32]int64, len(p))
		for partition, pc := range p {
			hwm[partition] = pc.HighWaterMarkOffset()
		}
		hwms[topic] = hwm
	}

	return hwms
}

func (c *consumer) addChild(child *partitionConsumer) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	topicChildren := c.children[child.topic]
	if topicChildren == nil {
		topicChildren = make(map[int32]*partitionConsumer)
		c.children[child.topic] = topicChildren
	}

	if topicChildren[child.partition] != nil {
		return ConfigurationError("That topic/partition is already being consumed")
	}

	topicChildren[child.partition] = child
	return nil
}

func (c *consumer) removeChild(child *partitionConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.children[child.topic], child.partition)
}

func (c *consumer) refBrokerConsumer(broker *Broker) *brokerConsumer {
	c.lock.Lock()
	defer c.lock.Unlock()

	bc := c.brokerConsumers[broker]
	if bc == nil {
		bc = c.newBrokerConsumer(broker)
		c.brokerConsumers[broker] = bc
	}

	bc.refs++

	return bc
}

func (c *consumer) unrefBrokerConsumer(brokerWorker *brokerConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	brokerWorker.refs--

	if brokerWorker.refs == 0 {
		brokerWorker.stopConsuming()
		if c.brokerConsumers[brokerWorker.broker] == brokerWorker {
			delete(c.brokerConsumers, brokerWorker.broker)
		}
	}
}

func (c *consumer) abandonBrokerConsumer(brokerWorker *brokerConsumer) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.brokerConsumers, brokerWorker.broker)
}

// Pause implements Consumer.
func (c *consumer) Pause(topicPartitions map[string][]int32) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for topic, partitions := range topicPartitions {
		for _, partition := range partitions {
			if topicConsumers, ok := c.children[topic]; ok {
				if partitionConsumer, ok := topicConsumers[partition]; ok {
					partitionConsumer.Pause()
				}
			}
		}
	}
}

// Resume implements Consumer.
func (c *consumer) Resume(topicPartitions map[string][]int32) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for topic, partitions := range topicPartitions {
		for _, partition := range partitions {
			if topicConsumers, ok := c.children[topic]; ok {
				if partitionConsumer, ok := topicConsumers[partition]; ok {
					partitionConsumer.Resume()
				}
			}
		}
	}
}

// PauseAll implements Consumer.
func (c *consumer) PauseAll() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, partitions := range c.children {
		for _, partitionConsumer := range partitions {
			partitionConsumer.Pause()
		}
	}
}

// ResumeAll implements Consumer.
func (c *consumer) ResumeAll() {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, partitions := range c.children {
		for _, partitionConsumer := range partitions {
			partitionConsumer.Resume()
		}
	}
}

// PartitionConsumer

// PartitionConsumer processes Kafka messages from a given topic and partition. You MUST call one of Close() or
// AsyncClose() on a PartitionConsumer to avoid leaks; it will not be garbage-collected automatically when it passes out
// of scope.
//
// The simplest way of using a PartitionConsumer is to loop over its Messages channel using a for/range
// loop. The PartitionConsumer will only stop itself in one case: when the offset being consumed is reported
// as out of range by the brokers. In this case you should decide what you want to do (try a different offset,
// notify a human, etc) and handle it appropriately. For all other error cases, it will just keep retrying.
// By default, it logs these errors to sarama.Logger; if you want to be notified directly of all errors, set
// your config's Consumer.Return.Errors to true and read from the Errors channel, using a select statement
// or a separate goroutine. Check out the Consumer examples to see implementations of these different approaches.
//
// To terminate such a for/range loop while the loop is executing, call AsyncClose. This will kick off the process of
// consumer tear-down & return immediately. Continue to loop, servicing the Messages channel until the teardown process
// AsyncClose initiated closes it (thus terminating the for/range loop). If you've already ceased reading Messages, call
// Close; this will signal the PartitionConsumer's goroutines to begin shutting down (just like AsyncClose), but will
// also drain the Messages channel, harvest all errors & return them once cleanup has completed.
type PartitionConsumer interface {
	// AsyncClose initiates a shutdown of the PartitionConsumer. This method will return immediately, after which you
	// should continue to service the 'Messages' and 'Errors' channels until they are empty. It is required to call this
	// function, or Close before a consumer object passes out of scope, as it will otherwise leak memory. You must call
	// this before calling Close on the underlying client.
	AsyncClose()

	// Close stops the PartitionConsumer from fetching messages. It will initiate a shutdown just like AsyncClose, drain
	// the Messages channel, harvest any errors & return them to the caller. Note that if you are continuing to service
	// the Messages channel when this function is called, you will be competing with Close for messages; consider
	// calling AsyncClose, instead. It is required to call this function (or AsyncClose) before a consumer object passes
	// out of scope, as it will otherwise leak memory. You must call this before calling Close on the underlying client.
	Close() error

	// Messages returns the read channel for the messages that are returned by
	// the broker.
	Messages() <-chan *ConsumerMessage

	// Errors returns a read channel of errors that occurred during consuming, if
	// enabled. By default, errors are logged and not returned over this channel.
	// If you want to implement any custom error handling, set your config's
	// Consumer.Return.Errors setting to true, and read from this channel.
	Errors() <-chan *ConsumerError

	// HighWaterMarkOffset returns the high water mark offset of the partition,
	// i.e. the offset that will be used for the next message that will be produced.
	// You can use this to determine how far behind the processing is.
	HighWaterMarkOffset() int64

	// Pause suspends fetching from this partition. Future calls to the broker will not return
	// any records from these partition until it have been resumed using Resume().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	Pause()

	// Resume resumes this partition which have been paused with Pause().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	// If the partition was not previously paused, this method is a no-op.
	Resume()

	// IsPaused indicates if this partition consumer is paused or not
	IsPaused() bool
}

type partitionConsumerResponse struct {
	broker       *brokerConsumer
	subscription *brokerSubscription
	response     *FetchResponse
}

type brokerSubscription struct {
	child       *partitionConsumer
	released    chan none
	releaseOnce sync.Once
}

func newBrokerSubscription(child *partitionConsumer) *brokerSubscription {
	return &brokerSubscription{
		child:    child,
		released: make(chan none),
	}
}

func (s *brokerSubscription) release() {
	s.releaseOnce.Do(func() {
		close(s.released)
	})
}

type partitionConsumer struct {
	highWaterMarkOffset atomic.Int64 // must be at the top of the struct because https://golang.org/pkg/sync/atomic/#pkg-note-BUG

	consumer           *consumer
	conf               *Config
	broker             *brokerConsumer
	brokerSubscription *brokerSubscription
	messages           chan *ConsumerMessage
	errors             chan *ConsumerError
	feeder             chan *partitionConsumerResponse

	leaderEpoch                int32
	preferredReadReplica       int32
	preferredReadReplicaExpiry time.Time

	trigger, dying     chan none
	dispatcherStop     chan none
	closeOnce          sync.Once
	dispatcherStopOnce sync.Once
	topic              string
	partition          int32
	responseResult     error
	fetchSize          int32
	offset             int64
	retries            atomic.Int32

	paused atomic.Bool // accessed atomically, 0 = not paused, 1 = paused
}

var errTimedOut = errors.New("timed out feeding messages to the user") // not user-facing

// log every Nth consecutive failure (~20s apart at the default backoff)
const stuckRetryThreshold = 10

func (child *partitionConsumer) sendError(err error) {
	cErr := &ConsumerError{
		Topic:     child.topic,
		Partition: child.partition,
		Err:       err,
	}

	if child.conf.Consumer.Return.Errors {
		child.errors <- cErr
	} else {
		Logger.Println(cErr)
	}
}

// notifyError delivers an abort error and queues a redispatch unless shutdown
// is already in progress
func (child *partitionConsumer) notifyError(err error) {
	cErr := &ConsumerError{
		Topic:     child.topic,
		Partition: child.partition,
		Err:       err,
	}

	select {
	case <-child.dying:
		// shutdown is already in progress; skip error delivery
		return
	default:
	}

	if child.conf.Consumer.Return.Errors {
		child.errors <- cErr
	} else {
		Logger.Println(cErr)
	}

	child.triggerRedispatch()
}

// triggerRedispatch queues a redispatch signal unless one is already pending.
// If the child is shutting down, the signal is ignored.
func (child *partitionConsumer) triggerRedispatch() {
	select {
	case <-child.dispatcherStop:
		return
	case <-child.dying:
		return
	default:
	}

	select {
	case child.trigger <- none{}:
	default:
	}
}

func (child *partitionConsumer) stopDispatcher() {
	child.dispatcherStopOnce.Do(func() {
		close(child.dispatcherStop)
	})
}

func (child *partitionConsumer) computeBackoff() time.Duration {
	retries := child.retries.Add(1)
	if retries >= stuckRetryThreshold && retries%stuckRetryThreshold == 0 {
		Logger.Printf("consumer/%s/%d still retrying after %d consecutive failures\n",
			child.topic, child.partition, retries)
	}
	if child.conf.Consumer.Retry.BackoffFunc != nil {
		return child.conf.Consumer.Retry.BackoffFunc(int(retries))
	}
	return child.conf.Consumer.Retry.Backoff
}

func (child *partitionConsumer) dispatcher() {
	defer func() {
		if child.broker != nil {
			child.consumer.unrefBrokerConsumer(child.broker)
		}
		child.consumer.removeChild(child)
		close(child.feeder)
	}()

	var backoff <-chan time.Time
	for {
		select {
		case <-child.dispatcherStop:
			return
		case <-child.dying:
			child.waitForBrokerHandover()
			return
		case <-child.trigger:
			if max := child.conf.Consumer.Retry.Max; max > 0 && int(child.retries.Load()) >= max {
				Logger.Printf("consumer/%s/%d giving up after %d consecutive failures\n",
					child.topic, child.partition, child.retries.Load())
				child.sendError(ErrConsumerRetriesExhausted)
				child.AsyncClose()
				child.waitForBrokerHandover()
				return
			}
			// only set the timer when none is pending, so retries increments
			// once per dispatch attempt rather than once per trigger
			if backoff == nil {
				backoff = time.After(child.computeBackoff())
			}
		case <-backoff:
			backoff = nil
			if child.broker != nil {
				child.consumer.unrefBrokerConsumer(child.broker)
				child.broker = nil
			}
			if err := child.dispatch(); err != nil {
				select {
				case <-child.dispatcherStop:
					return
				case <-child.dying:
					return
				default:
					child.sendError(err)
					child.triggerRedispatch()
				}
			}
		}
	}
}

// waitForBrokerHandover blocks until the brokerConsumer releases the current
// subscription, so the deferred close(feeder) cannot race an in-flight
// write to the feeder from subscriptionConsumer.
func (child *partitionConsumer) waitForBrokerHandover() {
	if child.broker == nil {
		return
	}
	select {
	case <-child.dispatcherStop:
	case <-child.brokerSubscription.released:
	}
}

func (child *partitionConsumer) preferredBroker() (*Broker, int32, error) {
	if child.preferredReadReplica >= 0 {
		// expire the preference periodically so the consumer returns to the
		// leader to re-evaluate, otherwise a follower that has dropped out of
		// the ISR (KAFKA-14372) would keep serving stale data forever (#2464)
		if !child.preferredReadReplicaExpiry.IsZero() && time.Now().After(child.preferredReadReplicaExpiry) {
			Logger.Printf(
				"consumer/%s/%d preferred read replica %d expired - will fallback to leader",
				child.topic, child.partition, child.preferredReadReplica)
			child.preferredReadReplica = invalidPreferredReplicaID
			child.preferredReadReplicaExpiry = time.Time{}
		} else {
			broker, err := child.consumer.client.Broker(child.preferredReadReplica)
			if err == nil {
				return broker, child.leaderEpoch, nil
			}
			Logger.Printf(
				"consumer/%s/%d failed to find active broker for preferred read replica %d - will fallback to leader",
				child.topic, child.partition, child.preferredReadReplica)

			// if we couldn't find it, discard the replica preference and trigger a
			// metadata refresh whilst falling back to consuming from the leader again
			child.preferredReadReplica = invalidPreferredReplicaID
			child.preferredReadReplicaExpiry = time.Time{}
			_ = child.consumer.client.RefreshMetadata(child.topic)
		}
	}

	// if preferred replica cannot be found fallback to leader
	return child.consumer.client.LeaderAndEpoch(child.topic, child.partition)
}

func (child *partitionConsumer) preferredReadReplicaLease() time.Duration {
	if child.conf.Metadata.RefreshFrequency > 0 {
		return child.conf.Metadata.RefreshFrequency
	}
	return defaultMetadataRefreshFrequency
}

func (child *partitionConsumer) dispatch() error {
	if err := child.consumer.client.RefreshMetadata(child.topic); err != nil {
		return err
	}

	broker, epoch, err := child.preferredBroker()
	if err != nil {
		return err
	}
	child.leaderEpoch = epoch
	for {
		child.broker = child.consumer.refBrokerConsumer(broker)
		child.brokerSubscription = newBrokerSubscription(child)
		if child.broker.queueSubscription(child.brokerSubscription) {
			return nil
		}

		child.brokerSubscription.release()
		child.consumer.unrefBrokerConsumer(child.broker)
	}
}

func (child *partitionConsumer) chooseStartingOffset(offset int64) error {
	newestOffset, err := child.consumer.client.GetOffset(child.topic, child.partition, OffsetNewest)
	if err != nil {
		return err
	}

	child.highWaterMarkOffset.Store(newestOffset)

	oldestOffset, err := child.consumer.client.GetOffset(child.topic, child.partition, OffsetOldest)
	if err != nil {
		return err
	}

	switch {
	case offset == OffsetNewest:
		child.offset = newestOffset
	case offset == OffsetOldest:
		child.offset = oldestOffset
	case offset >= oldestOffset && offset <= newestOffset:
		child.offset = offset
	default:
		return ErrOffsetOutOfRange
	}

	return nil
}

func (child *partitionConsumer) Messages() <-chan *ConsumerMessage {
	return child.messages
}

func (child *partitionConsumer) Errors() <-chan *ConsumerError {
	return child.errors
}

func (child *partitionConsumer) AsyncClose() {
	// this tells the current broker to abandon this child and lets the
	// dispatcher shut itself down, which eventually closes messages and errors
	child.closeOnce.Do(func() {
		close(child.dying)
	})
}

func (child *partitionConsumer) Close() error {
	child.AsyncClose()

	var consumerErrors ConsumerErrors
	for err := range child.errors {
		consumerErrors = append(consumerErrors, err)
	}

	if len(consumerErrors) > 0 {
		return consumerErrors
	}
	return nil
}

func (child *partitionConsumer) HighWaterMarkOffset() int64 {
	return child.highWaterMarkOffset.Load()
}

func (child *partitionConsumer) responseFeeder() {
	var msgs []*ConsumerMessage
	expiryTicker := time.NewTicker(child.conf.Consumer.MaxProcessingTime)
	firstAttempt := true

feederLoop:
	for feederResponse := range child.feeder {
		broker := feederResponse.broker
		subscription := feederResponse.subscription

		msgs, child.responseResult = child.parseResponse(feederResponse.response)

		if child.responseResult == nil {
			child.retries.Store(0)
		}

		for i, msg := range msgs {
			child.interceptors(msg)
		messageSelect:
			select {
			case <-child.dying:
				broker.acks.Done()
				continue feederLoop
			case child.messages <- msg:
				firstAttempt = true
			case <-expiryTicker.C:
				if !firstAttempt {
					child.responseResult = errTimedOut
					broker.acks.Done()
				remainingLoop:
					for _, msg = range msgs[i:] {
						child.interceptors(msg)
						select {
						case child.messages <- msg:
						case <-child.dying:
							break remainingLoop
						}
					}
					if !broker.queueSubscription(subscription) {
						// the broker is shutting down; release so any waiter on
						// the dispatcher side can make progress
						subscription.release()
						child.triggerRedispatch()
					}
					continue feederLoop
				} else {
					// current message has not been sent, return to select
					// statement
					firstAttempt = false
					goto messageSelect
				}
			}
		}

		broker.acks.Done()
	}

	expiryTicker.Stop()
	close(child.messages)
	close(child.errors)
}

func (child *partitionConsumer) parseMessages(msgSet *MessageSet) ([]*ConsumerMessage, error) {
	var messages []*ConsumerMessage
	for _, msgBlock := range msgSet.Messages {
		for _, msg := range msgBlock.Messages() {
			offset := msg.Offset
			timestamp := msg.Msg.Timestamp
			if msg.Msg.Version >= 1 {
				baseOffset := msgBlock.Offset - msgBlock.Messages()[len(msgBlock.Messages())-1].Offset
				offset += baseOffset
				if msg.Msg.LogAppendTime {
					timestamp = msgBlock.Msg.Timestamp
				}
			}
			if offset < child.offset {
				continue
			}
			messages = append(messages, &ConsumerMessage{
				Topic:          child.topic,
				Partition:      child.partition,
				Key:            msg.Msg.Key,
				Value:          msg.Msg.Value,
				Offset:         offset,
				Timestamp:      timestamp,
				BlockTimestamp: msgBlock.Msg.Timestamp,
			})
			child.offset = offset + 1
		}
	}
	if len(messages) == 0 {
		child.offset++
	}
	return messages, nil
}

func (child *partitionConsumer) parseRecords(batch *RecordBatch) ([]*ConsumerMessage, error) {
	messages := make([]*ConsumerMessage, 0, len(batch.Records))

	for _, rec := range batch.Records {
		offset := batch.FirstOffset + rec.OffsetDelta
		if offset < child.offset {
			continue
		}
		timestamp := batch.FirstTimestamp.Add(rec.TimestampDelta)
		if batch.LogAppendTime {
			timestamp = batch.MaxTimestamp
		}
		messages = append(messages, &ConsumerMessage{
			Topic:     child.topic,
			Partition: child.partition,
			Key:       rec.Key,
			Value:     rec.Value,
			Offset:    offset,
			Timestamp: timestamp,
			Headers:   rec.Headers,
		})
		child.offset = offset + 1
	}
	if len(messages) == 0 {
		child.offset++
	}
	return messages, nil
}

func (child *partitionConsumer) parseResponse(response *FetchResponse) ([]*ConsumerMessage, error) {
	var consumerBatchSizeMetric metrics.Histogram
	if child.consumer != nil && child.consumer.metricRegistry != nil {
		consumerBatchSizeMetric = getOrRegisterHistogram("consumer-batch-size", child.consumer.metricRegistry)
	}

	// If request was throttled and empty we log and return without error
	if response.ThrottleTime != time.Duration(0) && len(response.Blocks) == 0 {
		Logger.Printf(
			"consumer/broker/%d FetchResponse throttled %v\n",
			child.broker.broker.ID(), response.ThrottleTime)
		return nil, nil
	}

	block := response.GetBlock(child.topic, child.partition)
	if block == nil {
		return nil, ErrIncompleteResponse
	}

	if !errors.Is(block.Err, ErrNoError) {
		return nil, block.Err
	}

	nRecs, err := block.numRecords()
	if err != nil {
		return nil, err
	}

	if consumerBatchSizeMetric != nil {
		consumerBatchSizeMetric.Update(int64(nRecs))
	}

	if block.PreferredReadReplica != invalidPreferredReplicaID {
		child.preferredReadReplica = block.PreferredReadReplica
		child.preferredReadReplicaExpiry = time.Now().Add(child.preferredReadReplicaLease())
	}

	if nRecs == 0 {
		partialTrailingMessage, err := block.isPartial()
		if err != nil {
			return nil, err
		}
		// We got no messages. If we got a trailing one then we need to ask for more data.
		// Otherwise we just poll again and wait for one to be produced...
		if partialTrailingMessage {
			if child.conf.Consumer.Fetch.Max > 0 && child.fetchSize == child.conf.Consumer.Fetch.Max {
				// we can't ask for more data, we've hit the configured limit
				child.sendError(ErrMessageTooLarge)
				child.offset++ // skip this one so we can keep processing future messages
			} else {
				// if the broker told us the exact size of the partial batch, request that
				// directly; otherwise fall back to doubling the fetch size
				if block.partialBatchSize > child.fetchSize {
					child.fetchSize = block.partialBatchSize
				} else {
					child.fetchSize *= 2
				}
				// check int32 overflow
				if child.fetchSize < 0 {
					child.fetchSize = math.MaxInt32
				}
				if child.conf.Consumer.Fetch.Max > 0 && child.fetchSize > child.conf.Consumer.Fetch.Max {
					child.fetchSize = child.conf.Consumer.Fetch.Max
				}
			}
		} else if block.recordsNextOffset != nil && *block.recordsNextOffset <= block.HighWaterMarkOffset {
			// check last record next offset to avoid stuck if high watermark was not reached
			Logger.Printf("consumer/broker/%d received batch with zero records but high watermark was not reached, topic %s, partition %d, next offset %d\n", child.broker.broker.ID(), child.topic, child.partition, *block.recordsNextOffset)
			child.offset = *block.recordsNextOffset
		}

		return nil, nil
	}

	// we got messages, reset our fetch size in case it was increased for a previous request
	child.fetchSize = child.conf.Consumer.Fetch.Default
	child.highWaterMarkOffset.Store(block.HighWaterMarkOffset)

	// abortedProducerIDs contains producerID which message should be ignored as uncommitted
	// - producerID are added when the partitionConsumer iterate over the offset at which an aborted transaction begins (abortedTransaction.FirstOffset)
	// - producerID are removed when partitionConsumer iterate over an aborted controlRecord, meaning the aborted transaction for this producer is over
	abortedProducerIDs := make(map[int64]struct{}, len(block.AbortedTransactions))
	abortedTransactions := block.getAbortedTransactions()

	var messages []*ConsumerMessage
	for _, records := range block.RecordsSet {
		switch records.recordsType {
		case legacyRecords:
			messageSetMessages, err := child.parseMessages(records.MsgSet)
			if err != nil {
				return nil, err
			}

			messages = append(messages, messageSetMessages...)
		case defaultRecords:
			// Consume remaining abortedTransaction up to last offset of current batch
			for _, txn := range abortedTransactions {
				if txn.FirstOffset > records.RecordBatch.LastOffset() {
					break
				}
				abortedProducerIDs[txn.ProducerID] = struct{}{}
				// Pop abortedTransactions so that we never add it again
				abortedTransactions = abortedTransactions[1:]
			}

			recordBatchMessages, err := child.parseRecords(records.RecordBatch)
			if err != nil {
				return nil, err
			}

			// Parse and commit offset but do not expose messages that are:
			// - control records
			// - part of an aborted transaction when set to `ReadCommitted`

			// control record
			isControl, err := records.isControl()
			if err != nil {
				// I don't know why there is this continue in case of error to begin with
				// Safe bet is to ignore control messages if ReadUncommitted
				// and block on them in case of error and ReadCommitted
				if child.conf.Consumer.IsolationLevel == ReadCommitted {
					return nil, err
				}
				continue
			}
			if isControl {
				controlRecord, err := records.getControlRecord()
				if err != nil {
					return nil, err
				}

				if controlRecord.Type == ControlRecordAbort {
					delete(abortedProducerIDs, records.RecordBatch.ProducerID)
				}
				continue
			}

			// filter aborted transactions
			if child.conf.Consumer.IsolationLevel == ReadCommitted {
				_, isAborted := abortedProducerIDs[records.RecordBatch.ProducerID]
				if records.RecordBatch.IsTransactional && isAborted {
					continue
				}
			}

			messages = append(messages, recordBatchMessages...)
		default:
			return nil, fmt.Errorf("unknown records type: %v", records.recordsType)
		}
	}

	return messages, nil
}

func (child *partitionConsumer) interceptors(msg *ConsumerMessage) {
	for _, interceptor := range child.conf.Consumer.Interceptors {
		msg.safelyApplyInterceptor(interceptor)
	}
}

// Pause implements PartitionConsumer.
func (child *partitionConsumer) Pause() {
	child.paused.Store(true)
}

// Resume implements PartitionConsumer.
func (child *partitionConsumer) Resume() {
	child.paused.Store(false)
}

// IsPaused implements PartitionConsumer.
func (child *partitionConsumer) IsPaused() bool {
	return child.paused.Load()
}

type brokerConsumer struct {
	consumer         *consumer
	broker           *Broker
	input            chan *brokerSubscription
	newSubscriptions chan []*brokerSubscription
	subscriptions    map[*partitionConsumer]*brokerSubscription
	acks             sync.WaitGroup
	refs             int
	stop             chan none
	stopOnce         sync.Once
}

func (c *consumer) newBrokerConsumer(broker *Broker) *brokerConsumer {
	bc := &brokerConsumer{
		consumer:         c,
		broker:           broker,
		input:            make(chan *brokerSubscription),
		newSubscriptions: make(chan []*brokerSubscription),
		subscriptions:    make(map[*partitionConsumer]*brokerSubscription),
		refs:             0,
		stop:             make(chan none),
	}

	go withRecover(bc.subscriptionManager)
	go withRecover(bc.subscriptionConsumer)

	return bc
}

// The subscriptionManager constantly accepts new subscriptions on `input` (even when the main subscriptionConsumer
// goroutine is in the middle of a network request) and batches it up. The main worker goroutine picks
// up a batch of new subscriptions between every network request by reading from `newSubscriptions`, so we give
// it nil if no new subscriptions are available.
func (bc *brokerConsumer) subscriptionManager() {
	defer close(bc.newSubscriptions)

	for {
		var subscriptions []*brokerSubscription
		stopping := false

		// Check for any partition consumer asking to subscribe if there aren't
		// any, trigger the network request (to fetch Kafka messages) by sending "nil" to the
		// newSubscriptions channel
		select {
		case <-bc.stop:
			return
		case subscription := <-bc.input:
			subscriptions = append(subscriptions, subscription)
		case bc.newSubscriptions <- nil:
			continue
		}

		// drain input of any further incoming subscriptions
		timer := time.NewTimer(partitionConsumersBatchTimeout)
		for batchComplete := false; !batchComplete; {
			select {
			case <-bc.stop:
				stopping = true
				batchComplete = true
			case subscription := <-bc.input:
				subscriptions = append(subscriptions, subscription)
			case <-timer.C:
				batchComplete = true
			}
		}
		timer.Stop()

		Logger.Printf(
			"consumer/broker/%d accumulated %d new subscriptions\n",
			bc.broker.ID(), len(subscriptions))

		bc.newSubscriptions <- subscriptions
		if stopping {
			return
		}
	}
}

// subscriptionConsumer ensures we will get nil right away if no new subscriptions is available
// this is the main loop that fetches Kafka messages
func (bc *brokerConsumer) subscriptionConsumer() {
	for newSubscriptions := range bc.newSubscriptions {
		bc.updateSubscriptions(newSubscriptions)

		if len(bc.subscriptions) == 0 {
			// We're about to be shut down or we're about to receive more subscriptions.
			// Take a small nap to avoid burning the CPU.
			time.Sleep(partitionConsumersBatchTimeout)
			continue
		}

		response, err := bc.fetchNewMessages()
		if err != nil {
			Logger.Printf("consumer/broker/%d disconnecting due to error processing FetchRequest: %s\n", bc.broker.ID(), err)
			bc.abort(err)
			return
		}

		// if there isn't response, it means that not fetch was made
		// so we don't need to handle any response
		if response == nil {
			time.Sleep(partitionConsumersBatchTimeout)
			continue
		}

		bc.acks.Add(len(bc.subscriptions))
		for child, subscription := range bc.subscriptions {
			select {
			case <-child.dying:
				Logger.Printf("consumer/broker/%d closed dead subscription to %s/%d\n", bc.broker.ID(), child.topic, child.partition)
				bc.releaseSubscription(child)
				child.stopDispatcher()
				bc.acks.Done()
				continue
			default:
			}

			if _, ok := response.Blocks[child.topic]; !ok {
				bc.acks.Done()
				continue
			}

			if _, ok := response.Blocks[child.topic][child.partition]; !ok {
				bc.acks.Done()
				continue
			}

			child.feeder <- &partitionConsumerResponse{
				broker:       bc,
				subscription: subscription,
				response:     response,
			}
		}
		bc.acks.Wait()
		bc.handleResponses()
	}
}

func (bc *brokerConsumer) updateSubscriptions(newSubscriptions []*brokerSubscription) {
	for _, subscription := range newSubscriptions {
		child := subscription.child
		bc.subscriptions[child] = subscription
		Logger.Printf("consumer/broker/%d added subscription to %s/%d\n", bc.broker.ID(), child.topic, child.partition)
	}

	for child := range bc.subscriptions {
		select {
		case <-child.dying:
			Logger.Printf("consumer/broker/%d closed dead subscription to %s/%d\n", bc.broker.ID(), child.topic, child.partition)
			bc.releaseSubscription(child)
			child.stopDispatcher()
		default:
			// no-op
		}
	}
}

// handleResponses handles the response codes left for us by our subscriptions, and abandons ones that have been closed
func (bc *brokerConsumer) handleResponses() {
	for child := range bc.subscriptions {
		select {
		case <-child.dying:
			bc.releaseSubscription(child)
			child.stopDispatcher()
			continue
		default:
		}

		result := child.responseResult
		child.responseResult = nil

		if result == nil {
			if preferredBroker, _, err := child.preferredBroker(); err == nil {
				if bc.broker.ID() != preferredBroker.ID() {
					// not an error but needs redispatching to consume from preferred replica
					Logger.Printf(
						"consumer/broker/%d abandoned in favor of preferred replica broker/%d\n",
						bc.broker.ID(), preferredBroker.ID())
					child.triggerRedispatch()
					bc.releaseSubscription(child)
				}
			}
			continue
		}

		// Discard any replica preference.
		child.preferredReadReplica = invalidPreferredReplicaID
		child.preferredReadReplicaExpiry = time.Time{}

		if errors.Is(result, errTimedOut) {
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because consuming was taking too long\n",
				bc.broker.ID(), child.topic, child.partition)
			// responseFeeder already requeued this subscription onto bc.input
			// so it will loop back through subscriptionManager so no need to
			// release it here
			delete(bc.subscriptions, child)
		} else if errors.Is(result, ErrOffsetOutOfRange) {
			// there's no point in retrying this it will just fail the same way again
			// shut it down and force the user to choose what to do
			child.sendError(result)
			Logger.Printf("consumer/%s/%d shutting down because %s\n", child.topic, child.partition, result)
			child.stopDispatcher()
			child.AsyncClose()
			bc.releaseSubscription(child)
		} else if errors.Is(result, ErrUnknownTopicOrPartition) ||
			errors.Is(result, ErrNotLeaderForPartition) ||
			errors.Is(result, ErrLeaderNotAvailable) ||
			errors.Is(result, ErrReplicaNotAvailable) ||
			errors.Is(result, ErrFencedLeaderEpoch) ||
			errors.Is(result, ErrUnknownLeaderEpoch) {
			// not an error, but does need redispatching
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because %s\n",
				bc.broker.ID(), child.topic, child.partition, result)
			child.triggerRedispatch()
			bc.releaseSubscription(child)
		} else {
			// dunno, tell the user and try redispatching
			child.sendError(result)
			Logger.Printf("consumer/broker/%d abandoned subscription to %s/%d because %s\n",
				bc.broker.ID(), child.topic, child.partition, result)
			child.triggerRedispatch()
			bc.releaseSubscription(child)
		}
	}
}

func (bc *brokerConsumer) abort(err error) {
	bc.consumer.abandonBrokerConsumer(bc)
	bc.stopConsuming()
	_ = bc.broker.Close() // we don't care about the error this might return, we already have one

	for child := range bc.subscriptions {
		bc.releaseSubscription(child)
		select {
		case <-child.dying:
			child.stopDispatcher()
		default:
			child.notifyError(err)
		}
	}

	for newSubscriptions := range bc.newSubscriptions {
		for _, subscription := range newSubscriptions {
			child := subscription.child
			subscription.release()
			select {
			case <-child.dying:
				child.stopDispatcher()
			default:
				child.notifyError(err)
			}
		}
	}
}

// fetchNewMessages can be nil if no fetch is made, it can occur when
// all partitions are paused
func (bc *brokerConsumer) fetchNewMessages() (*FetchResponse, error) {
	request := &FetchRequest{
		MinBytes:    bc.consumer.conf.Consumer.Fetch.Min,
		MaxWaitTime: int32(bc.consumer.conf.Consumer.MaxWaitTime / time.Millisecond),
	}
	// pick the highest Fetch version supported by the negotiated Kafka version
	switch {
	// Version 12 adds flexible version support and last fetched epoch.
	case bc.consumer.conf.Version.IsAtLeast(V2_7_0_0):
		request.Version = 12
	// Version 11 adds RackID for KIP-392 fetch from closest replica.
	case bc.consumer.conf.Version.IsAtLeast(V2_3_0_0):
		request.Version = 11
	// Version 9 adds CurrentLeaderEpoch (KIP-320); version 10 allows the ZStd
	// compression algorithm (KIP-110).
	case bc.consumer.conf.Version.IsAtLeast(V2_1_0_0):
		request.Version = 10
	// Version 8 is the same as version 7.
	case bc.consumer.conf.Version.IsAtLeast(V2_0_0_0):
		request.Version = 8
	// Version 7 adds incremental fetch request support.
	case bc.consumer.conf.Version.IsAtLeast(V1_1_0_0):
		request.Version = 7
	// Version 6 is the same as version 5.
	case bc.consumer.conf.Version.IsAtLeast(V1_0_0_0):
		request.Version = 6
	// Version 4 adds IsolationLevel and requires Kafka log message format
	// version 2; version 5 adds LogStartOffset.
	case bc.consumer.conf.Version.IsAtLeast(V0_11_0_0):
		request.Version = 5
	// Version 3 adds MaxBytes and makes partition ordering significant:
	// partitions are processed in the order they appear in the request.
	case bc.consumer.conf.Version.IsAtLeast(V0_10_1_0):
		request.Version = 3
	// Starting in version 2, the requestor must be able to handle Kafka log
	// message format version 1.
	case bc.consumer.conf.Version.IsAtLeast(V0_10_0_0):
		request.Version = 2
	// Version 1 is the same as version 0.
	case bc.consumer.conf.Version.IsAtLeast(V0_9_0_0):
		request.Version = 1
	}

	if request.Version >= 3 {
		request.MaxBytes = bc.consumer.conf.Consumer.Fetch.MaxBytes
	}
	if request.Version >= 4 {
		request.Isolation = bc.consumer.conf.Consumer.IsolationLevel
	}
	if request.Version >= 7 {
		// We do not currently implement KIP-227 FetchSessions. Setting the id to 0
		// and the epoch to -1 tells the broker not to generate as session ID we're going
		// to just ignore anyway.
		request.SessionID = 0
		request.SessionEpoch = -1
	}
	if request.Version >= 11 {
		request.RackID = bc.consumer.conf.RackID
	}

	for child := range bc.subscriptions {
		select {
		case <-child.dying:
			bc.releaseSubscription(child)
			child.stopDispatcher()
			continue
		default:
		}

		if !child.IsPaused() {
			request.AddBlock(child.topic, child.partition, child.offset, child.fetchSize, child.leaderEpoch)
		}
	}

	// avoid to fetch when there is no block
	if len(request.blocks) == 0 {
		return nil, nil
	}

	return bc.broker.Fetch(request)
}

func (bc *brokerConsumer) stopConsuming() {
	bc.stopOnce.Do(func() {
		close(bc.stop)
	})
}

func (bc *brokerConsumer) releaseSubscription(child *partitionConsumer) {
	if subscription, ok := bc.subscriptions[child]; ok {
		delete(bc.subscriptions, child)
		subscription.release()
	}
}

func (bc *brokerConsumer) queueSubscription(subscription *brokerSubscription) bool {
	select {
	case <-bc.stop:
		return false
	case bc.input <- subscription:
		return true
	}
}
