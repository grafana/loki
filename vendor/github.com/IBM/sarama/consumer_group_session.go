package sarama

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ConsumerGroupSession represents a consumer group member session.
type ConsumerGroupSession interface {
	// Claims returns the currently claimed partitions by topic. The claims may
	// change across cooperative rebalances.
	Claims() map[string][]int32

	// MemberID returns the cluster member ID.
	MemberID() string

	// GenerationID returns the current generation ID. The generation changes
	// across cooperative rebalances.
	GenerationID() int32

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
	MarkOffset(topic string, partition int32, offset int64, metadata string)

	// Commit the offset to the backend
	//
	// Note: calling Commit performs a blocking synchronous operation.
	Commit()

	// ResetOffset resets to the provided offset, alongside a metadata string that
	// represents the state of the partition consumer at that point in time. Reset
	// acts as a counterpart to MarkOffset, the difference being that it allows to
	// reset an offset to an earlier or smaller value, where MarkOffset only
	// allows incrementing the offset. cf MarkOffset for more details.
	ResetOffset(topic string, partition int32, offset int64, metadata string)

	// MarkMessage marks a message as consumed.
	MarkMessage(msg *ConsumerMessage, metadata string)

	// Context returns the session context.
	Context() context.Context
}

type runningClaim struct {
	cancel context.CancelCauseFunc
	done   chan none
}

type consumerGroupSession struct {
	parent  *consumerGroup
	handler ConsumerGroupHandler

	// cooperative sessions replace these values on each generation
	memberID     atomic.Pointer[string]
	generationID atomic.Int32
	claims       atomic.Pointer[map[string][]int32]

	offsets *offsetManager
	ctx     context.Context
	cancel  context.CancelCauseFunc

	// only the Consume goroutine accesses running and partitionWatchCancel
	running              map[topicPartitionAssignment]*runningClaim
	partitionWatchCancel context.CancelFunc

	// the buffered channel coalesces concurrent rebalance notifications
	rejoin chan error

	// serializes joining a new generation with heartbeat requests
	generationMu sync.Mutex

	waitGroup       sync.WaitGroup
	releaseOnce     sync.Once
	hbDying, hbDead chan none
}

func newConsumerGroupSession(ctx context.Context, parent *consumerGroup, claims map[string][]int32, memberID string, generationID int32, handler ConsumerGroupHandler) (*consumerGroupSession, error) {
	// init context
	ctx, cancel := context.WithCancelCause(ctx)

	// init offset manager
	offsets, err := newOffsetManagerFromClient(parent.groupID, memberID, generationID, parent.client, cancel)
	if err != nil {
		return nil, err
	}

	// init session
	sess := &consumerGroupSession{
		parent:  parent,
		handler: handler,
		offsets: offsets,
		ctx:     ctx,
		cancel:  cancel,
		running: make(map[topicPartitionAssignment]*runningClaim),
		rejoin:  make(chan error, 1),
		hbDying: make(chan none),
		hbDead:  make(chan none),
	}
	sess.memberID.Store(&memberID)
	sess.generationID.Store(generationID)
	sess.claims.Store(&claims)

	// start heartbeat loop
	go sess.heartbeatLoop()

	// create a POM for each claim
	if err := sess.manageClaims(claims); err != nil {
		_ = sess.release(false)
		return nil, err
	}

	// perform setup
	if err := handler.Setup(sess); err != nil {
		_ = sess.release(true)
		return nil, err
	}

	// start consuming each topic partition in its own goroutine
	sess.startClaims(claims)
	return sess, nil
}

// watchPartitionNumbers replaces the leader-only watcher for each generation
func (s *consumerGroupSession) watchPartitionNumbers(res *rebalanceResult) {
	if s.partitionWatchCancel != nil {
		s.partitionWatchCancel()
		s.partitionWatchCancel = nil
	}
	if !res.isLeader {
		return
	}

	var ctx context.Context
	ctx, s.partitionWatchCancel = context.WithCancel(s.ctx)
	go s.parent.loopCheckPartitionNumbers(ctx, res.allSubscribedTopicPartitions, res.allSubscribedTopics, s)
}

func (s *consumerGroupSession) managePartition(topic string, partition int32) error {
	pom, err := s.offsets.ManagePartition(topic, partition)
	if err != nil {
		return err
	}

	// handle POM errors
	go func() {
		for err := range pom.Errors() {
			s.parent.handleError(err, topic, partition)
		}
	}()
	return nil
}

func (s *consumerGroupSession) manageClaims(claims map[string][]int32) error {
	for topic, partitions := range claims {
		for _, partition := range partitions {
			if err := s.managePartition(topic, partition); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *consumerGroupSession) startClaims(claims map[string][]int32) {
	for topic, partitions := range claims {
		for _, partition := range partitions {
			s.startClaim(topic, partition)
		}
	}
}

// startClaim gives each partition a context that can be canceled independently
func (s *consumerGroupSession) startClaim(topic string, partition int32) {
	claimCtx, cancel := context.WithCancelCause(s.ctx)
	claim := &runningClaim{
		cancel: cancel,
		done:   make(chan none),
	}

	s.running[topicPartitionAssignment{Topic: topic, Partition: partition}] = claim

	s.waitGroup.Go(func() {
		defer close(claim.done)
		// a handler returning without revocation still ends the session
		defer func() {
			if !errors.Is(context.Cause(claimCtx), errPartitionRevoked) {
				s.cancel(ErrSessionConsumeClaimExited)
			}
		}()

		// if partition not currently readable, wait for it to become readable
		if s.parent.client.PartitionNotReadable(topic, partition) {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()

			for s.parent.client.PartitionNotReadable(topic, partition) {
				select {
				case <-claimCtx.Done():
					return
				case <-s.parent.closed:
					return
				case <-timer.C:
					timer.Reset(5 * time.Second)
				}
			}
		}

		// consume a single topic/partition, blocking
		s.consume(claimCtx, topic, partition)
	})
}

func (s *consumerGroupSession) Claims() map[string][]int32 { return *s.claims.Load() }
func (s *consumerGroupSession) MemberID() string           { return *s.memberID.Load() }
func (s *consumerGroupSession) GenerationID() int32        { return s.generationID.Load() }

func (s *consumerGroupSession) MarkOffset(topic string, partition int32, offset int64, metadata string) {
	if pom := s.offsets.findPOM(topic, partition); pom != nil {
		pom.MarkOffset(offset, metadata)
	}
}

func (s *consumerGroupSession) Commit() {
	s.offsets.Commit()
}

func (s *consumerGroupSession) ResetOffset(topic string, partition int32, offset int64, metadata string) {
	if pom := s.offsets.findPOM(topic, partition); pom != nil {
		pom.ResetOffset(offset, metadata)
	}
}

func (s *consumerGroupSession) MarkMessage(msg *ConsumerMessage, metadata string) {
	s.MarkOffset(msg.Topic, msg.Partition, msg.Offset+1, metadata)
}

func (s *consumerGroupSession) Context() context.Context {
	return s.ctx
}

// newClaimWithRetry calls newConsumerGroupClaim, retrying transient errors so
// that brief leader/metadata desync around a rebalance doesn't leave a
// partition permanently unclaimed for the lifetime of this session
func (s *consumerGroupSession) newClaimWithRetry(ctx context.Context, topic string, partition int32, offset int64) (*consumerGroupClaim, error) {
	retries := s.parent.config.Metadata.Retry.Max
	for {
		claim, err := newConsumerGroupClaim(s, topic, partition, offset)
		if err == nil {
			return claim, nil
		}
		if retries <= 0 || !isRetriableClaimError(err) {
			return nil, err
		}
		retries--

		backoff := computeMetadataBackoff(s.parent.config, retries)
		Logger.Printf(
			"consumer-group/claim %s/%d retrying after %dms... (%d attempts remaining): %v\n",
			topic, partition, backoff/time.Millisecond, retries, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.parent.closed:
			return nil, err
		case <-time.After(backoff):
		}

		// refresh leader/broker info before retrying
		_ = s.parent.client.RefreshMetadata(topic)
	}
}

// isRetriableClaimError reports whether err from newConsumerGroupClaim is
// a transient condition worth retrying after a metadata refresh
func isRetriableClaimError(err error) bool {
	return errors.Is(err, ErrNotConnected) ||
		errors.Is(err, ErrLeaderNotAvailable) ||
		errors.Is(err, ErrNotLeaderForPartition) ||
		errors.Is(err, ErrFencedLeaderEpoch) ||
		errors.Is(err, ErrUnknownLeaderEpoch) ||
		errors.Is(err, ErrReplicaNotAvailable)
}

func (s *consumerGroupSession) consume(ctx context.Context, topic string, partition int32) {
	// quick exit if rebalance is due
	select {
	case <-ctx.Done():
		return
	case <-s.parent.closed:
		return
	default:
	}

	// get next offset
	offset := s.parent.config.Consumer.Offsets.Initial
	if pom := s.offsets.findPOM(topic, partition); pom != nil {
		offset, _ = pom.NextOffset()
	}

	// create new claim
	claim, err := s.newClaimWithRetry(ctx, topic, partition, offset)
	if err != nil {
		if ctx.Err() == nil {
			s.parent.handleError(err, topic, partition)
		}
		return
	}

	// trigger close when the claim is revoked or the session is done; closing
	// Messages() is how a handler is told to let the partition go
	go func() {
		select {
		case <-ctx.Done():
		case <-s.parent.closed:
		}
		claim.AsyncClose()
	}()

	// start processing
	if err := s.handler.ConsumeClaim(s, claim); err != nil {
		s.parent.handleError(err, topic, partition)
	}

	// ensure consumer is closed & drained
	claim.AsyncClose()
	for _, err := range claim.waitClosed() {
		s.parent.handleError(err, topic, partition)
	}
}

func (s *consumerGroupSession) release(withCleanup bool) (err error) {
	// signal release, stop heartbeat
	s.cancel(nil)

	// wait for consumers to exit
	s.waitGroup.Wait()

	// perform release
	s.releaseOnce.Do(func() {
		if withCleanup {
			if e := s.handler.Cleanup(s); e != nil {
				s.parent.handleError(e, "", -1)
				err = e
			}
		}

		if e := s.offsets.Close(); e != nil {
			err = e
		}

		close(s.hbDying)
		<-s.hbDead
	})

	Logger.Printf(
		"consumergroup/session/%s/%d released\n",
		s.MemberID(), s.GenerationID())

	return
}

func (s *consumerGroupSession) sendHeartbeat(coordinator *Broker) (*HeartbeatResponse, error) {
	s.generationMu.Lock()
	defer s.generationMu.Unlock()
	resp, err := s.parent.heartbeatRequest(coordinator, s.MemberID(), s.GenerationID())
	if err != nil {
		return nil, err
	}

	if errors.Is(resp.Err, ErrRebalanceInProgress) {
		// Signal while still holding generationMu, so the signal cannot arrive
		// after a cooperative rejoin has already joined this rebalance.
		s.triggerRebalance(resp.Err)
	}
	return resp, nil
}

func (s *consumerGroupSession) heartbeatLoop() {
	defer close(s.hbDead)
	defer s.cancel(ErrSessionHeartbeatFailed) // trigger the end of the session on exit
	defer func() {
		Logger.Printf(
			"consumergroup/session/%s/%d heartbeat loop stopped\n",
			s.MemberID(), s.GenerationID())
	}()

	pause := time.NewTicker(s.parent.config.Consumer.Group.Heartbeat.Interval)
	defer pause.Stop()

	retryBackoff := time.NewTimer(s.parent.config.Metadata.Retry.Backoff)
	defer retryBackoff.Stop()

	retries := s.parent.config.Metadata.Retry.Max
	for {
		coordinator, err := s.parent.client.Coordinator(s.parent.groupID)
		if err != nil {
			if retries <= 0 {
				s.parent.handleError(err, "", -1)
				s.cancel(err)
				return
			}
			retryBackoff.Reset(s.parent.config.Metadata.Retry.Backoff)
			select {
			case <-s.hbDying:
				return
			case <-retryBackoff.C:
				retries--
			}
			continue
		}

		resp, err := s.sendHeartbeat(coordinator)
		if err != nil {
			_ = coordinator.Close()

			if retries <= 0 {
				s.parent.handleError(err, "", -1)
				s.cancel(err)
				return
			}

			retries--
			continue
		}

		switch err := resp.Err; err {
		case ErrNoError:
			retries = s.parent.config.Metadata.Retry.Max
		case ErrRebalanceInProgress:
			retries = s.parent.config.Metadata.Retry.Max
		case ErrNotCoordinatorForConsumer, ErrConsumerCoordinatorNotAvailable:
			// the group has moved to another coordinator, which keeps the
			// generation, so find it and carry on heartbeating
			if retries <= 0 {
				s.parent.handleError(err, "", -1)
				s.cancel(err)
				return
			}
			_ = s.parent.client.RefreshCoordinator(s.parent.groupID)
			retryBackoff.Reset(s.parent.config.Metadata.Retry.Backoff)
			select {
			case <-s.hbDying:
				return
			case <-retryBackoff.C:
				retries--
			}
			continue
		case ErrUnknownMemberId, ErrIllegalGeneration:
			s.cancel(err)
			return
		case ErrFencedInstancedId:
			if s.parent.groupInstanceId != nil {
				Logger.Printf("JoinGroup failed: group instance id %s has been fenced\n", *s.parent.groupInstanceId)
			}
			s.parent.handleError(err, "", -1)
			s.cancel(err)
			return
		default:
			s.parent.handleError(err, "", -1)
			s.cancel(err)
			return
		}

		select {
		case <-pause.C:
		case <-s.hbDying:
			return
		}
	}
}

func sessionCauseToReason(cause error) string {
	switch {
	case errors.Is(cause, ErrRebalanceInProgress):
		return "group is rebalancing"
	case errors.Is(cause, ErrUnknownMemberId):
		return "member id is not known by the group coordinator"
	case errors.Is(cause, ErrIllegalGeneration):
		return "generation id is not current"
	case errors.Is(cause, ErrFencedInstancedId):
		return "group instance id has been fenced"
	case errors.Is(cause, ErrSessionPartitionCountChanged):
		return "partitions were added to a subscribed topic"
	case errors.Is(cause, ErrSessionConsumeClaimExited):
		return "a ConsumeClaim handler has exited"
	case errors.Is(cause, ErrSessionHeartbeatFailed):
		return "the heartbeat goroutine has stopped"
	default:
		return cause.Error()
	}
}

// --------------------------------------------------------------------

// ConsumerGroupHandler instances are used to handle individual topic/partition claims.
// It also provides hooks for your consumer group session life-cycle and allow you to
// trigger logic before or after the consume loop(s).
//
// PLEASE NOTE that handlers are likely be called from several goroutines concurrently,
// ensure that all state is safely protected against race conditions.
type ConsumerGroupHandler interface {
	// Setup is run at the beginning of a new session, before ConsumeClaim.
	Setup(ConsumerGroupSession) error

	// Cleanup is run at the end of a session, once all ConsumeClaim goroutines have exited
	// but before the offsets are committed for the very last time.
	Cleanup(ConsumerGroupSession) error

	// ConsumeClaim must start a consumer loop of ConsumerGroupClaim's Messages().
	// Once the Messages() channel is closed, the Handler must finish its processing
	// loop and exit. Handlers should also return when ConsumerGroupSession.Context()
	// is done; Messages() alone can block while the partition consumer is retrying
	// (e.g. after a broker disconnect). See examples/consumergroup.
	ConsumeClaim(ConsumerGroupSession, ConsumerGroupClaim) error
}

// ConsumerGroupClaim processes Kafka messages from a given topic and partition within a consumer group.
type ConsumerGroupClaim interface {
	// Topic returns the consumed topic name.
	Topic() string

	// Partition returns the consumed partition.
	Partition() int32

	// InitialOffset returns the initial offset that was used as a starting point for this claim.
	InitialOffset() int64

	// HighWaterMarkOffset returns the high watermark offset of the partition,
	// i.e. the offset that will be used for the next message that will be produced.
	// You can use this to determine how far behind the processing is.
	HighWaterMarkOffset() int64

	// Messages returns messages from the broker. The channel closes when the
	// claim is released. You must finish processing and mark offsets within
	// Config.Consumer.Group.Rebalance.Timeout after a claim is revoked.
	Messages() <-chan *ConsumerMessage
}

type consumerGroupClaim struct {
	topic     string
	partition int32
	offset    int64
	PartitionConsumer
}

func newConsumerGroupClaim(sess *consumerGroupSession, topic string, partition int32, offset int64) (*consumerGroupClaim, error) {
	pcm, err := sess.parent.consumer.ConsumePartition(topic, partition, offset)

	if errors.Is(err, ErrOffsetOutOfRange) && sess.parent.config.Consumer.Group.ResetInvalidOffsets {
		offset = sess.parent.config.Consumer.Offsets.Initial
		pcm, err = sess.parent.consumer.ConsumePartition(topic, partition, offset)
	}
	if err != nil {
		return nil, err
	}

	go func() {
		for err := range pcm.Errors() {
			sess.parent.handleError(err, topic, partition)
		}
	}()

	return &consumerGroupClaim{
		topic:             topic,
		partition:         partition,
		offset:            offset,
		PartitionConsumer: pcm,
	}, nil
}

func (c *consumerGroupClaim) Topic() string        { return c.topic }
func (c *consumerGroupClaim) Partition() int32     { return c.partition }
func (c *consumerGroupClaim) InitialOffset() int64 { return c.offset }

// Drains messages and errors, ensures the claim is fully closed.
func (c *consumerGroupClaim) waitClosed() (errs ConsumerErrors) {
	go func() {
		for range c.Messages() {
		}
	}()

	for err := range c.Errors() {
		errs = append(errs, err)
	}
	return
}
