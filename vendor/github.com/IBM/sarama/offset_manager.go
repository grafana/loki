package sarama

import (
	"sync"
	"time"
)

// Offset Manager

// OffsetManager uses Kafka to store and fetch consumed partition offsets.
type OffsetManager interface {
	// ManagePartition creates a PartitionOffsetManager on the given topic/partition.
	// It will return an error if this OffsetManager is already managing the given
	// topic/partition.
	ManagePartition(topic string, partition int32) (PartitionOffsetManager, error)

	// Close stops the OffsetManager from managing offsets. It is required to call
	// this function before an OffsetManager object passes out of scope, as it
	// will otherwise leak memory. You must call this after all the
	// PartitionOffsetManagers are closed.
	Close() error

	// Commit commits the offsets. This method can be used if AutoCommit.Enable is
	// set to false.
	Commit()
}

type offsetManager struct {
	client          Client
	conf            *Config
	group           string
	ticker          *time.Ticker
	sessionCanceler func()

	memberID        string
	groupInstanceId *string
	generation      int32

	broker     *Broker
	brokerLock sync.RWMutex

	poms     map[string]map[int32]*partitionOffsetManager
	pomsLock sync.RWMutex

	closeOnce sync.Once
	closing   chan none
	closed    chan none
}

// NewOffsetManagerFromClient creates a new OffsetManager from the given client.
// It is still necessary to call Close() on the underlying client when finished with the partition manager.
func NewOffsetManagerFromClient(group string, client Client) (OffsetManager, error) {
	return newOffsetManagerFromClient(group, "", GroupGenerationUndefined, client, nil)
}

func newOffsetManagerFromClient(group, memberID string, generation int32, client Client, sessionCanceler func()) (*offsetManager, error) {
	// Check that we are not dealing with a closed Client before processing any other arguments
	if client.Closed() {
		return nil, ErrClosedClient
	}

	conf := client.Config()
	om := &offsetManager{
		client:          client,
		conf:            conf,
		group:           group,
		poms:            make(map[string]map[int32]*partitionOffsetManager),
		sessionCanceler: sessionCanceler,

		memberID:   memberID,
		generation: generation,

		closing: make(chan none),
		closed:  make(chan none),
	}
	if conf.Consumer.Group.InstanceId != "" {
		om.groupInstanceId = &conf.Consumer.Group.InstanceId
	}
	if conf.Consumer.Offsets.AutoCommit.Enable {
		om.ticker = time.NewTicker(conf.Consumer.Offsets.AutoCommit.Interval)
		go withRecover(om.mainLoop)
	}

	return om, nil
}

func (om *offsetManager) ManagePartition(topic string, partition int32) (PartitionOffsetManager, error) {
	pom, err := om.newPartitionOffsetManager(topic, partition)
	if err != nil {
		return nil, err
	}

	om.pomsLock.Lock()
	defer om.pomsLock.Unlock()

	topicManagers := om.poms[topic]
	if topicManagers == nil {
		topicManagers = make(map[int32]*partitionOffsetManager)
		om.poms[topic] = topicManagers
	}

	if topicManagers[partition] != nil {
		return nil, ConfigurationError("That topic/partition is already being managed")
	}

	topicManagers[partition] = pom
	return pom, nil
}

func (om *offsetManager) Close() error {
	om.closeOnce.Do(func() {
		// exit the mainLoop
		close(om.closing)
		if om.conf.Consumer.Offsets.AutoCommit.Enable {
			<-om.closed
		}

		// mark all POMs as closed
		om.asyncClosePOMs()

		// flush one last time
		if om.conf.Consumer.Offsets.AutoCommit.Enable {
			for attempt := 0; attempt <= om.conf.Consumer.Offsets.Retry.Max; attempt++ {
				om.flushToBroker()
				if om.releasePOMs(false) == 0 {
					break
				}
			}
		}

		om.releasePOMs(true)
		om.brokerLock.Lock()
		om.broker = nil
		om.brokerLock.Unlock()
	})
	return nil
}

func (om *offsetManager) computeBackoff(retries int) time.Duration {
	if om.conf.Metadata.Retry.BackoffFunc != nil {
		return om.conf.Metadata.Retry.BackoffFunc(retries, om.conf.Metadata.Retry.Max)
	} else {
		return om.conf.Metadata.Retry.Backoff
	}
}

func (om *offsetManager) fetchInitialOffset(topic string, partition int32, retries int) (int64, int32, string, error) {
	broker, err := om.coordinator()
	if err != nil {
		if retries <= 0 {
			return 0, 0, "", err
		}
		return om.fetchInitialOffset(topic, partition, retries-1)
	}

	partitions := map[string][]int32{topic: {partition}}
	req := NewOffsetFetchRequest(om.conf.Version, om.group, partitions)
	resp, err := broker.FetchOffset(req)
	if err != nil {
		if retries <= 0 {
			return 0, 0, "", err
		}
		om.releaseCoordinator(broker)
		return om.fetchInitialOffset(topic, partition, retries-1)
	}

	block := resp.GetBlock(topic, partition)
	if block == nil {
		return 0, 0, "", ErrIncompleteResponse
	}

	switch block.Err {
	case ErrNoError:
		return block.Offset, block.LeaderEpoch, block.Metadata, nil
	case ErrNotCoordinatorForConsumer:
		if retries <= 0 {
			return 0, 0, "", block.Err
		}
		om.releaseCoordinator(broker)
		return om.fetchInitialOffset(topic, partition, retries-1)
	case ErrOffsetsLoadInProgress:
		if retries <= 0 {
			return 0, 0, "", block.Err
		}
		backoff := om.computeBackoff(retries)
		select {
		case <-om.closing:
			return 0, 0, "", block.Err
		case <-time.After(backoff):
		}
		return om.fetchInitialOffset(topic, partition, retries-1)
	default:
		return 0, 0, "", block.Err
	}
}

func (om *offsetManager) coordinator() (*Broker, error) {
	om.brokerLock.RLock()
	broker := om.broker
	om.brokerLock.RUnlock()

	if broker != nil {
		return broker, nil
	}

	om.brokerLock.Lock()
	defer om.brokerLock.Unlock()

	if broker := om.broker; broker != nil {
		return broker, nil
	}

	if err := om.client.RefreshCoordinator(om.group); err != nil {
		return nil, err
	}

	broker, err := om.client.Coordinator(om.group)
	if err != nil {
		return nil, err
	}

	om.broker = broker
	return broker, nil
}

func (om *offsetManager) releaseCoordinator(b *Broker) {
	om.brokerLock.Lock()
	if om.broker == b {
		om.broker = nil
	}
	om.brokerLock.Unlock()
}

func (om *offsetManager) mainLoop() {
	defer om.ticker.Stop()
	defer close(om.closed)

	for {
		select {
		case <-om.ticker.C:
			om.Commit()
		case <-om.closing:
			return
		}
	}
}

func (om *offsetManager) Commit() {
	om.flushToBroker()
	om.releasePOMs(false)
}

func (om *offsetManager) flushToBroker() {
	broker, err := om.coordinator()
	if err != nil {
		om.handleError(err)
		return
	}

	// Care needs to be taken to unlock this. Don't want to defer the unlock as this would
	// cause the lock to be held while waiting for the broker to reply.
	broker.lock.Lock()
	req := om.constructRequest()
	if req == nil {
		broker.lock.Unlock()
		return
	}
	resp, rp, err := sendOffsetCommit(broker, req)
	broker.lock.Unlock()

	if err != nil {
		om.handleError(err)
		om.releaseCoordinator(broker)
		_ = broker.Close()
		return
	}

	err = handleResponsePromise(req, resp, rp, nil)
	if err != nil {
		om.handleError(err)
		om.releaseCoordinator(broker)
		_ = broker.Close()
		return
	}

	broker.handleThrottledResponse(resp)
	om.handleResponse(broker, req, resp)
}

func sendOffsetCommit(coordinator *Broker, req *OffsetCommitRequest) (*OffsetCommitResponse, *responsePromise, error) {
	resp := new(OffsetCommitResponse)
	responseHeaderVersion := resp.headerVersion()
	promise, err := coordinator.send(req, true, responseHeaderVersion)
	if err != nil {
		return nil, nil, err
	}
	return resp, promise, nil
}

func (om *offsetManager) constructRequest() *OffsetCommitRequest {
	r := &OffsetCommitRequest{
		Version:                 1,
		ConsumerGroup:           om.group,
		ConsumerID:              om.memberID,
		ConsumerGroupGeneration: om.generation,
	}
	// Version 1 adds timestamp and group membership information, as well as the commit timestamp.
	//
	// Version 2 adds retention time.  It removes the commit timestamp added in version 1.
	if om.conf.Version.IsAtLeast(V0_9_0_0) {
		r.Version = 2
	}
	// Version 3 and 4 are the same as version 2.
	if om.conf.Version.IsAtLeast(V0_11_0_0) {
		r.Version = 3
	}
	if om.conf.Version.IsAtLeast(V2_0_0_0) {
		r.Version = 4
	}
	// Version 5 removes the retention time, which is now controlled only by a broker configuration.
	//
	// Version 6 adds the leader epoch for fencing.
	if om.conf.Version.IsAtLeast(V2_1_0_0) {
		r.Version = 6
	}
	// version 7 adds a new field called groupInstanceId to indicate member identity across restarts.
	if om.conf.Version.IsAtLeast(V2_3_0_0) {
		r.Version = 7
		r.GroupInstanceId = om.groupInstanceId
	}

	// commit timestamp was only briefly supported in V1 where we set it to
	// ReceiveTime (-1) to tell the broker to set it to the time when the commit
	// request was received
	var commitTimestamp int64
	if r.Version == 1 {
		commitTimestamp = ReceiveTime
	}

	// request controlled retention was only supported from V2-V4 (it became
	// broker-only after that) so if the user has set the config options then
	// flow those through as retention time on the commit request.
	if r.Version >= 2 && r.Version < 5 {
		// Map Sarama's default of 0 to Kafka's default of -1
		r.RetentionTime = -1
		if om.conf.Consumer.Offsets.Retention > 0 {
			r.RetentionTime = int64(om.conf.Consumer.Offsets.Retention / time.Millisecond)
		}
	}

	om.pomsLock.RLock()
	defer om.pomsLock.RUnlock()

	for _, topicManagers := range om.poms {
		for _, pom := range topicManagers {
			pom.lock.Lock()
			if pom.dirty {
				r.AddBlockWithLeaderEpoch(pom.topic, pom.partition, pom.offset, pom.leaderEpoch, commitTimestamp, pom.metadata)
			}
			pom.lock.Unlock()
		}
	}

	if len(r.blocks) > 0 {
		return r
	}

	return nil
}

func (om *offsetManager) handleResponse(broker *Broker, req *OffsetCommitRequest, resp *OffsetCommitResponse) {
	om.pomsLock.RLock()
	defer om.pomsLock.RUnlock()

	for _, topicManagers := range om.poms {
		for _, pom := range topicManagers {
			if req.blocks[pom.topic] == nil || req.blocks[pom.topic][pom.partition] == nil {
				continue
			}

			var err KError
			var ok bool

			if resp.Errors[pom.topic] == nil {
				pom.handleError(ErrIncompleteResponse)
				continue
			}
			if err, ok = resp.Errors[pom.topic][pom.partition]; !ok {
				pom.handleError(ErrIncompleteResponse)
				continue
			}

			switch err {
			case ErrNoError:
				block := req.blocks[pom.topic][pom.partition]
				pom.updateCommitted(block.offset, block.metadata)
			case ErrNotLeaderForPartition, ErrLeaderNotAvailable,
				ErrConsumerCoordinatorNotAvailable, ErrNotCoordinatorForConsumer:
				// not a critical error, we just need to redispatch
				om.releaseCoordinator(broker)
			case ErrOffsetMetadataTooLarge, ErrInvalidCommitOffsetSize:
				// nothing we can do about this, just tell the user and carry on
				pom.handleError(err)
			case ErrOffsetsLoadInProgress:
				// nothing wrong but we didn't commit, we'll get it next time round
			case ErrFencedInstancedId:
				pom.handleError(err)
				// TODO close the whole consumer for instance fenced....
				om.tryCancelSession()
			case ErrUnknownTopicOrPartition:
				// let the user know *and* try redispatching - if topic-auto-create is
				// enabled, redispatching should trigger a metadata req and create the
				// topic; if not then re-dispatching won't help, but we've let the user
				// know and it shouldn't hurt either (see https://github.com/IBM/sarama/issues/706)
				fallthrough
			default:
				// dunno, tell the user and try redispatching
				pom.handleError(err)
				om.releaseCoordinator(broker)
			}
		}
	}
}

func (om *offsetManager) handleError(err error) {
	om.pomsLock.RLock()
	defer om.pomsLock.RUnlock()

	for _, topicManagers := range om.poms {
		for _, pom := range topicManagers {
			pom.handleError(err)
		}
	}
}

func (om *offsetManager) asyncClosePOMs() {
	om.pomsLock.RLock()
	defer om.pomsLock.RUnlock()

	for _, topicManagers := range om.poms {
		for _, pom := range topicManagers {
			pom.AsyncClose()
		}
	}
}

// Releases/removes closed POMs once they are clean (or when forced)
func (om *offsetManager) releasePOMs(force bool) (remaining int) {
	om.pomsLock.Lock()
	defer om.pomsLock.Unlock()

	for topic, topicManagers := range om.poms {
		for partition, pom := range topicManagers {
			pom.lock.Lock()
			releaseDue := pom.done && (force || !pom.dirty)
			pom.lock.Unlock()

			if releaseDue {
				pom.release()

				delete(om.poms[topic], partition)
				if len(om.poms[topic]) == 0 {
					delete(om.poms, topic)
				}
			}
		}
		remaining += len(om.poms[topic])
	}
	return
}

func (om *offsetManager) findPOM(topic string, partition int32) *partitionOffsetManager {
	om.pomsLock.RLock()
	defer om.pomsLock.RUnlock()

	if partitions, ok := om.poms[topic]; ok {
		if pom, ok := partitions[partition]; ok {
			return pom
		}
	}
	return nil
}

func (om *offsetManager) tryCancelSession() {
	if om.sessionCanceler != nil {
		om.sessionCanceler()
	}
}

// Partition Offset Manager

// PartitionOffsetManager uses Kafka to store and fetch consumed partition offsets. You MUST call Close()
// on a partition offset manager to avoid leaks, it will not be garbage-collected automatically when it passes
// out of scope.
type PartitionOffsetManager interface {
	// NextOffset returns the next offset that should be consumed for the managed
	// partition, accompanied by metadata which can be used to reconstruct the state
	// of the partition consumer when it resumes. NextOffset() will return
	// `config.Consumer.Offsets.Initial` and an empty metadata string if no offset
	// was committed for this partition yet.
	NextOffset() (int64, string)

	// MarkOffset marks the provided offset, alongside a metadata string
	// that represents the state of the partition consumer at that point in time. The
	// metadata string can be used by another consumer to restore that state, so it
	// can resume consumption.
	//
	// To follow upstream conventions, you are expected to mark the offset of the
	// next message to read, not the last message read. Thus, when calling `MarkOffset`
	// you should typically add one to the offset of the last consumed message.
	//
	// Note: calling MarkOffset does not necessarily commit the offset to the backend
	// store immediately for efficiency reasons, and it may never be committed if
	// your application crashes. This means that you may end up processing the same
	// message twice, and your processing should ideally be idempotent.
	MarkOffset(offset int64, metadata string)

	// ResetOffset resets to the provided offset, alongside a metadata string that
	// represents the state of the partition consumer at that point in time. Reset
	// acts as a counterpart to MarkOffset, the difference being that it allows to
	// reset an offset to an earlier or smaller value, where MarkOffset only
	// allows incrementing the offset. cf MarkOffset for more details.
	ResetOffset(offset int64, metadata string)

	// Errors returns a read channel of errors that occur during offset management, if
	// enabled. By default, errors are logged and not returned over this channel. If
	// you want to implement any custom error handling, set your config's
	// Consumer.Return.Errors setting to true, and read from this channel.
	Errors() <-chan *ConsumerError

	// AsyncClose initiates a shutdown of the PartitionOffsetManager. This method will
	// return immediately, after which you should wait until the 'errors' channel has
	// been drained and closed. It is required to call this function, or Close before
	// a consumer object passes out of scope, as it will otherwise leak memory. You
	// must call this before calling Close on the underlying client.
	AsyncClose()

	// Close stops the PartitionOffsetManager from managing offsets. It is required to
	// call this function (or AsyncClose) before a PartitionOffsetManager object
	// passes out of scope, as it will otherwise leak memory. You must call this
	// before calling Close on the underlying client.
	Close() error
}

type partitionOffsetManager struct {
	parent      *offsetManager
	topic       string
	partition   int32
	leaderEpoch int32

	lock     sync.Mutex
	offset   int64
	metadata string
	dirty    bool
	done     bool

	releaseOnce sync.Once
	errors      chan *ConsumerError
}

func (om *offsetManager) newPartitionOffsetManager(topic string, partition int32) (*partitionOffsetManager, error) {
	offset, leaderEpoch, metadata, err := om.fetchInitialOffset(topic, partition, om.conf.Metadata.Retry.Max)
	if err != nil {
		return nil, err
	}

	return &partitionOffsetManager{
		parent:      om,
		topic:       topic,
		partition:   partition,
		leaderEpoch: leaderEpoch,
		errors:      make(chan *ConsumerError, om.conf.ChannelBufferSize),
		offset:      offset,
		metadata:    metadata,
	}, nil
}

func (pom *partitionOffsetManager) Errors() <-chan *ConsumerError {
	return pom.errors
}

func (pom *partitionOffsetManager) MarkOffset(offset int64, metadata string) {
	pom.lock.Lock()
	defer pom.lock.Unlock()

	if offset > pom.offset {
		pom.offset = offset
		pom.metadata = metadata
		pom.dirty = true
	}
}

func (pom *partitionOffsetManager) ResetOffset(offset int64, metadata string) {
	pom.lock.Lock()
	defer pom.lock.Unlock()

	if offset <= pom.offset {
		pom.offset = offset
		pom.metadata = metadata
		pom.dirty = true
	}
}

func (pom *partitionOffsetManager) updateCommitted(offset int64, metadata string) {
	pom.lock.Lock()
	defer pom.lock.Unlock()

	if pom.offset == offset && pom.metadata == metadata {
		pom.dirty = false
	}
}

func (pom *partitionOffsetManager) NextOffset() (int64, string) {
	pom.lock.Lock()
	defer pom.lock.Unlock()

	if pom.offset >= 0 {
		return pom.offset, pom.metadata
	}

	return pom.parent.conf.Consumer.Offsets.Initial, ""
}

func (pom *partitionOffsetManager) AsyncClose() {
	pom.lock.Lock()
	pom.done = true
	pom.lock.Unlock()
}

func (pom *partitionOffsetManager) Close() error {
	pom.AsyncClose()

	var errors ConsumerErrors
	for err := range pom.errors {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return errors
	}
	return nil
}

func (pom *partitionOffsetManager) handleError(err error) {
	cErr := &ConsumerError{
		Topic:     pom.topic,
		Partition: pom.partition,
		Err:       err,
	}

	if pom.parent.conf.Consumer.Return.Errors {
		pom.errors <- cErr
	} else {
		Logger.Println(cErr)
	}
}

func (pom *partitionOffsetManager) release() {
	pom.releaseOnce.Do(func() {
		close(pom.errors)
	})
}
