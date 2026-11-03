package sarama

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
)

// ErrClosedConsumerGroup is the error returned when a method is called on a consumer group that has been closed.
var ErrClosedConsumerGroup = errors.New("kafka: tried to use a consumer group that was closed")

// ErrSessionPartitionCountChanged is set as the cancellation cause of a consumer group
// session context when the leader detects that the partition count for a subscribed topic
// has changed, requiring a new session.
var ErrSessionPartitionCountChanged = errors.New("kafka: partition count changed for subscribed topic")

// ErrSessionConsumeClaimExited is set as the cancellation cause of a consumer group session
// context when a ConsumeClaim goroutine exits, triggering the end of the session.
var ErrSessionConsumeClaimExited = errors.New("kafka: ConsumeClaim goroutine exited")

// ErrSessionHeartbeatFailed is set as the cancellation cause of a consumer group session
// context when the heartbeat loop exits due to an unrecoverable error (e.g. coordinator
// unreachable after retries).
var ErrSessionHeartbeatFailed = errors.New("kafka: heartbeat loop failed")

// ErrRebalanceTimedOut is set as the cancellation cause of a consumer group session
// context when a ConsumeClaim did not return within Consumer.Group.Rebalance.Timeout
// of the partition being revoked from it.
var ErrRebalanceTimedOut = errors.New("kafka: ConsumeClaim did not exit within Consumer.Group.Rebalance.Timeout of being revoked")

// errPartitionRevoked is set as the cancellation cause of a single claim's context
// when the partition has been revoked by a cooperative rebalance. It is internal:
// the session survives, and the handler is told by its Messages() channel closing.
var errPartitionRevoked = errors.New("kafka: partition revoked by rebalance")

// ConsumerGroup is responsible for dividing up processing of topics and partitions
// over a collection of processes (the members of the consumer group).
type ConsumerGroup interface {
	// Consume joins a cluster of consumers for a given list of topics and
	// starts a blocking ConsumerGroupSession through the ConsumerGroupHandler.
	//
	// A call to Consume follows this lifecycle:
	//
	// 1. The consumer joins the group and receives its assigned topic partitions,
	//    also known as claims.
	// 2. The handler's Setup method runs before claim processing starts.
	// 3. The handler's ConsumeClaim method runs in a separate goroutine for each
	//    claim, so the handler must be safe for concurrent use.
	// 4. When the session ends, Consume waits for the claims to return, calls the
	//    handler's Cleanup method, and commits marked offsets before releasing them.
	//
	// With an eager strategy, a rebalance ends the current session and this
	// Consume call. The caller should invoke Consume from a loop to create the
	// next session and receive the new claims.
	//
	// With a cooperative strategy, a rebalance updates the existing session:
	//
	//   - Setup runs on the first join and Cleanup runs when the member leaves.
	//   - Retained claims keep running.
	//   - A revoked claim has its Messages channel closed.
	//   - Consume and the session context remain active across rebalances.
	//
	// A revoked ConsumeClaim must return within
	// Config.Consumer.Group.Rebalance.Timeout. If it does not, Kafka may remove
	// the member from the group and subsequent offset commits may fail.
	Consume(ctx context.Context, topics []string, handler ConsumerGroupHandler) error

	// Errors returns a read channel of errors that occurred during the consumer life-cycle.
	// By default, errors are logged and not returned over this channel.
	// If you want to implement any custom error handling, set your config's
	// Consumer.Return.Errors setting to true, and read from this channel.
	Errors() <-chan error

	// Close stops the ConsumerGroup and detaches any running sessions. It is required to call
	// this function before the object passes out of scope, as it will otherwise leak memory.
	Close() error

	// Pause suspends fetching from the requested partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	Pause(partitions map[string][]int32)

	// Resume resumes specified partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	Resume(partitions map[string][]int32)

	// Pause suspends fetching from all partitions. Future calls to the broker will not return any
	// records from these partitions until they have been resumed using Resume()/ResumeAll().
	// Note that this method does not affect partition subscription.
	// In particular, it does not cause a group rebalance when automatic assignment is used.
	PauseAll()

	// Resume resumes all partitions which have been paused with Pause()/PauseAll().
	// New calls to the broker will return records from these partitions if there are any to be fetched.
	ResumeAll()
}

type consumerGroup struct {
	client Client

	config           *Config
	consumer         Consumer
	groupID          string
	groupInstanceId  *string
	memberID         string
	lastSessionCause error
	errors           chan error

	lock       sync.Mutex
	errorsLock sync.RWMutex
	closed     chan none
	closeOnce  sync.Once

	userData []byte

	// protocol is fixed at construction from the configured balance strategies
	protocol RebalanceProtocol

	metricRegistry metrics.Registry
}

// NewConsumerGroup creates a new consumer group the given broker addresses and configuration.
func NewConsumerGroup(addrs []string, groupID string, config *Config) (ConsumerGroup, error) {
	client, err := NewClient(addrs, config)
	if err != nil {
		return nil, err
	}

	c, err := newConsumerGroup(groupID, client)
	if err != nil {
		_ = client.Close()
	}
	return c, err
}

// NewConsumerGroupFromClient creates a new consumer group using the given client. It is still
// necessary to call Close() on the underlying client when shutting down this consumer.
// PLEASE NOTE: consumer groups can only re-use but not share clients.
func NewConsumerGroupFromClient(groupID string, client Client) (ConsumerGroup, error) {
	if client == nil {
		return nil, ConfigurationError("client must not be nil")
	}
	// For clients passed in by the client, ensure we don't
	// call Close() on it.
	cli := &nopCloserClient{client}
	return newConsumerGroup(groupID, cli)
}

func newConsumerGroup(groupID string, client Client) (ConsumerGroup, error) {
	config := client.Config()
	if !config.Version.IsAtLeast(V0_10_2_0) {
		return nil, ConfigurationError("consumer groups require Version to be >= V0_10_2_0")
	}

	protocol, err := selectRebalanceProtocol(config.groupStrategies())
	if err != nil {
		return nil, err
	}

	consumer, err := newConsumer(client)
	if err != nil {
		return nil, err
	}

	cg := &consumerGroup{
		client:         client,
		consumer:       consumer,
		config:         config,
		groupID:        groupID,
		errors:         make(chan error, config.ChannelBufferSize),
		closed:         make(chan none),
		userData:       config.Consumer.Group.Member.UserData,
		protocol:       protocol,
		metricRegistry: newCleanupRegistry(config.MetricRegistry),
	}
	if config.Consumer.Group.InstanceId != "" && config.Version.IsAtLeast(V2_3_0_0) {
		cg.groupInstanceId = &config.Consumer.Group.InstanceId
	}
	return cg, nil
}

// Errors implements ConsumerGroup.
func (c *consumerGroup) Errors() <-chan error { return c.errors }

// Close implements ConsumerGroup.
func (c *consumerGroup) Close() (err error) {
	c.closeOnce.Do(func() {
		close(c.closed)

		// leave group
		if e := c.leave(); e != nil {
			err = e
		}

		go func() {
			c.errorsLock.Lock()
			defer c.errorsLock.Unlock()
			close(c.errors)
		}()

		// drain errors
		for e := range c.errors {
			err = e
		}

		if e := c.client.Close(); e != nil {
			err = e
		}

		c.metricRegistry.UnregisterAll()
	})
	return
}

// Consume implements ConsumerGroup.
func (c *consumerGroup) Consume(ctx context.Context, topics []string, handler ConsumerGroupHandler) error {
	// Ensure group is not closed
	select {
	case <-c.closed:
		return ErrClosedConsumerGroup
	default:
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	// Close may have left the group between the check above and taking the
	// lock; joining now would add a member that nothing ever removes
	select {
	case <-c.closed:
		return ErrClosedConsumerGroup
	default:
	}

	// Quick exit when no topics are provided
	if len(topics) == 0 {
		return fmt.Errorf("no topics provided")
	}

	// Refresh metadata for requested topics
	if err := c.client.RefreshMetadata(topics...); err != nil {
		return err
	}

	// Init session
	sess, err := c.newSession(ctx, topics, handler, c.config.Consumer.Group.Rebalance.Retry.Max)
	if errors.Is(err, ErrClosedClient) {
		return ErrClosedConsumerGroup
	}
	if err != nil {
		return err
	}

	if c.protocol == RebalanceProtocolCooperative {
		return c.consumeCooperative(ctx, topics, sess)
	}

	// Wait for session exit signal or Close() call
	select {
	case <-c.closed:
	case <-sess.ctx.Done():
	}

	// Gracefully release session claims
	err = sess.release(true)

	c.recordSessionCause(sess)

	return err
}

// recordSessionCause keeps the reason for the next JoinGroup or LeaveGroup request
func (c *consumerGroup) recordSessionCause(sess *consumerGroupSession) {
	if cause := context.Cause(sess.ctx); !errors.Is(cause, context.Canceled) {
		c.lastSessionCause = cause
	} else {
		c.lastSessionCause = nil
	}
}

// Pause implements ConsumerGroup.
func (c *consumerGroup) Pause(partitions map[string][]int32) {
	c.consumer.Pause(partitions)
}

// Resume implements ConsumerGroup.
func (c *consumerGroup) Resume(partitions map[string][]int32) {
	c.consumer.Resume(partitions)
}

// PauseAll implements ConsumerGroup.
func (c *consumerGroup) PauseAll() {
	c.consumer.PauseAll()
}

// ResumeAll implements ConsumerGroup.
func (c *consumerGroup) ResumeAll() {
	c.consumer.ResumeAll()
}

func (c *consumerGroup) retryJoinSync(ctx context.Context, topics []string, held *heldAssignment, retries int, refreshCoordinator bool) (*rebalanceResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrClosedConsumerGroup
	case <-time.After(c.config.Consumer.Group.Rebalance.Retry.Backoff):
	}

	if refreshCoordinator {
		err := c.client.RefreshCoordinator(c.groupID)
		if err != nil {
			if retries <= 0 {
				return nil, err
			}
			return c.retryJoinSync(ctx, topics, held, retries-1, true)
		}
	}

	return c.joinSync(ctx, topics, held, retries-1)
}

type heldAssignment struct {
	claims       map[string][]int32
	generationID int32
}

type rebalanceResult struct {
	memberID     string
	generationID int32
	claims       map[string][]int32

	isLeader                     bool
	allSubscribedTopicPartitions map[string][]int32
	allSubscribedTopics          []string
}

// joinSync separates group negotiation from session lifetime so a session can survive a rejoin
func (c *consumerGroup) joinSync(ctx context.Context, topics []string, held *heldAssignment, retries int) (*rebalanceResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	coordinator, err := c.client.Coordinator(c.groupID)
	if err != nil {
		if retries <= 0 {
			return nil, err
		}

		return c.retryJoinSync(ctx, topics, held, retries, true)
	}

	var (
		metricRegistry          = c.metricRegistry
		consumerGroupJoinTotal  metrics.Counter
		consumerGroupJoinFailed metrics.Counter
		consumerGroupSyncTotal  metrics.Counter
		consumerGroupSyncFailed metrics.Counter
	)

	if metricRegistry != nil {
		consumerGroupJoinTotal = metrics.GetOrRegisterCounter(fmt.Sprintf("consumer-group-join-total-%s", c.groupID), metricRegistry)
		consumerGroupJoinFailed = metrics.GetOrRegisterCounter(fmt.Sprintf("consumer-group-join-failed-%s", c.groupID), metricRegistry)
		consumerGroupSyncTotal = metrics.GetOrRegisterCounter(fmt.Sprintf("consumer-group-sync-total-%s", c.groupID), metricRegistry)
		consumerGroupSyncFailed = metrics.GetOrRegisterCounter(fmt.Sprintf("consumer-group-sync-failed-%s", c.groupID), metricRegistry)
	}

	// Join consumer group
	join, err := c.joinGroupRequest(coordinator, topics, held)
	if consumerGroupJoinTotal != nil {
		consumerGroupJoinTotal.Inc(1)
	}
	if err != nil {
		_ = coordinator.Close()
		if consumerGroupJoinFailed != nil {
			consumerGroupJoinFailed.Inc(1)
		}
		return nil, err
	}
	if !errors.Is(join.Err, ErrNoError) {
		if consumerGroupJoinFailed != nil {
			consumerGroupJoinFailed.Inc(1)
		}
	}
	switch join.Err {
	case ErrNoError:
		c.memberID = join.MemberId
		// clear the session cause only once a join succeeds so the KIP-394
		// second request repeats the reason
		c.lastSessionCause = nil
	case ErrUnknownMemberId, ErrIllegalGeneration:
		// reset member ID and retry immediately
		c.memberID = ""
		if c.lostHeldAssignment(held, join.Err) {
			return nil, join.Err
		}
		return c.joinSync(ctx, topics, held, retries)
	case ErrNotCoordinatorForConsumer, ErrConsumerCoordinatorNotAvailable, ErrRebalanceInProgress, ErrOffsetsLoadInProgress:
		// retry after backoff
		if retries <= 0 {
			return nil, join.Err
		}
		return c.retryJoinSync(ctx, topics, held, retries, true)
	case ErrMemberIdRequired:
		// from JoinGroupRequest v4 onwards (due to KIP-394) if the client starts
		// with an empty member id, it needs to get the assigned id from the
		// response and send another join request with that id to actually join the
		// group
		c.memberID = join.MemberId
		return c.joinSync(ctx, topics, held, retries)
	case ErrFencedInstancedId:
		if c.groupInstanceId != nil {
			Logger.Printf("JoinGroup failed: group instance id %s has been fenced\n", *c.groupInstanceId)
		}
		return nil, join.Err
	default:
		return nil, join.Err
	}

	var strategy BalanceStrategy
	var ok bool
	if strategy = c.config.Consumer.Group.Rebalance.Strategy; strategy == nil {
		strategy, ok = c.findStrategy(join.GroupProtocol, c.config.Consumer.Group.Rebalance.GroupStrategies)
		if !ok {
			// this case shouldn't happen in practice, since the leader will choose the protocol
			// that all the members support
			return nil, fmt.Errorf("unable to find selected strategy: %s", join.GroupProtocol)
		}
	}

	// Prepare distribution plan if we joined as the leader
	var plan BalanceStrategyPlan
	var members map[string]ConsumerGroupMemberMetadata
	var allSubscribedTopicPartitions map[string][]int32
	var allSubscribedTopics []string
	if join.LeaderId == join.MemberId {
		members, err = join.GetMembers()
		if err != nil {
			return nil, err
		}

		allSubscribedTopicPartitions, allSubscribedTopics, plan, err = c.balance(strategy, members)
		if err != nil {
			return nil, err
		}
	}

	// Sync consumer group
	syncGroupResponse, err := c.syncGroupRequest(coordinator, members, plan, join.GenerationId, strategy)
	if consumerGroupSyncTotal != nil {
		consumerGroupSyncTotal.Inc(1)
	}
	if err != nil {
		_ = coordinator.Close()
		if consumerGroupSyncFailed != nil {
			consumerGroupSyncFailed.Inc(1)
		}
		return nil, err
	}
	if !errors.Is(syncGroupResponse.Err, ErrNoError) {
		if consumerGroupSyncFailed != nil {
			consumerGroupSyncFailed.Inc(1)
		}
	}

	switch syncGroupResponse.Err {
	case ErrNoError:
	case ErrUnknownMemberId, ErrIllegalGeneration:
		// reset member ID and retry immediately
		c.memberID = ""
		if c.lostHeldAssignment(held, syncGroupResponse.Err) {
			return nil, syncGroupResponse.Err
		}
		return c.joinSync(ctx, topics, held, retries)
	case ErrNotCoordinatorForConsumer, ErrConsumerCoordinatorNotAvailable, ErrRebalanceInProgress, ErrOffsetsLoadInProgress:
		// retry after backoff
		if retries <= 0 {
			return nil, syncGroupResponse.Err
		}
		if held != nil {
			// the member joined this generation still owning held.claims, so
			// report it on the retry; a Java 3.3 or earlier leader reassigns
			// the owned partitions of a member reporting an older generation
			// without waiting for them to be revoked
			held.generationID = join.GenerationId
		}
		return c.retryJoinSync(ctx, topics, held, retries, true)
	case ErrFencedInstancedId:
		if c.groupInstanceId != nil {
			Logger.Printf("JoinGroup failed: group instance id %s has been fenced\n", *c.groupInstanceId)
		}
		return nil, syncGroupResponse.Err
	default:
		return nil, syncGroupResponse.Err
	}

	// Retrieve and sort claims
	var claims map[string][]int32
	if len(syncGroupResponse.MemberAssignment) > 0 {
		members, err := syncGroupResponse.GetMemberAssignment()
		if err != nil {
			return nil, err
		}
		claims = members.Topics

		// in the case of stateful balance strategies, hold on to the returned
		// assignment metadata, otherwise, reset the statically defined consumer
		// group metadata
		if members.UserData != nil {
			c.userData = members.UserData
		} else {
			c.userData = c.config.Consumer.Group.Member.UserData
		}

		// Notify a stateful strategy of the assignment it just received, so it
		// can carry leader-computed state into its next subscription.
		if onAssign, ok := strategy.(OnAssignmentBalanceStrategy); ok {
			onAssign.OnAssignment(members, join.GenerationId)
		}

		for _, partitions := range claims {
			sort.Sort(int32Slice(partitions))
		}
	}

	return &rebalanceResult{
		memberID:                     join.MemberId,
		generationID:                 join.GenerationId,
		claims:                       claims,
		isLeader:                     join.LeaderId == join.MemberId,
		allSubscribedTopicPartitions: allSubscribedTopicPartitions,
		allSubscribedTopics:          allSubscribedTopics,
	}, nil
}

func (c *consumerGroup) lostHeldAssignment(held *heldAssignment, err error) bool {
	if held == nil || len(held.claims) == 0 {
		return false
	}
	Logger.Printf("consumergroup/%s: lost ownership of %v due to %v\n", c.groupID, held.claims, err)
	return true
}

func (c *consumerGroup) newSession(ctx context.Context, topics []string, handler ConsumerGroupHandler, retries int) (*consumerGroupSession, error) {
	res, err := c.joinSync(ctx, topics, nil, retries)
	if err != nil {
		return nil, err
	}

	session, err := newConsumerGroupSession(ctx, c, res.claims, res.memberID, res.generationID, handler)
	if err != nil {
		return nil, err
	}

	// only the leader needs to check whether there are newly-added partitions in order to trigger a rebalance
	session.watchPartitionNumbers(res)

	return session, nil
}

func (c *consumerGroup) joinGroupRequest(coordinator *Broker, topics []string, held *heldAssignment) (*JoinGroupResponse, error) {
	req := NewJoinGroupRequest(c.config.Version)
	req.GroupId = c.groupID
	req.MemberId = c.memberID
	req.SessionTimeout = int32(c.config.Consumer.Group.Session.Timeout / time.Millisecond)
	req.ProtocolType = "consumer"
	if req.Version >= 1 {
		req.RebalanceTimeout = int32(c.config.Consumer.Group.Rebalance.Timeout / time.Millisecond)
	}
	if req.Version >= 5 {
		req.GroupInstanceId = c.groupInstanceId
	}
	if req.Version >= 8 && c.lastSessionCause != nil {
		reason := sessionCauseToReason(c.lastSessionCause)
		req.Reason = &reason
	}

	var owned map[string][]int32
	generationID := int32(defaultGeneration)
	if held != nil {
		owned = held.claims
		generationID = held.generationID
	}

	for _, strategy := range c.config.groupStrategies() {
		meta := c.subscriptionMetadata(strategy, topics, owned, generationID)
		if err := req.AddGroupProtocolMetadata(strategy.Name(), meta); err != nil {
			return nil, err
		}
	}

	return coordinator.JoinGroup(req)
}

// subscriptionVersion is based on the broker version rather than the rebalance
// protocol so owned partitions are available during a rolling upgrade
func (c *consumerGroup) subscriptionVersion() int16 {
	switch {
	case c.config.Version.IsAtLeast(V3_2_0_0):
		return 2 // KIP-792: GenerationID
	case c.config.Version.IsAtLeast(V2_4_0_0):
		return 1 // KIP-429: OwnedPartitions
	default:
		return 0
	}
}

// subscriptionMetadata builds the ConsumerGroupMemberMetadata for a single
// strategy in a JoinGroup request. If the strategy implements
// SubscriptionUserDataBalanceStrategy, its SubscriptionUserData hook is invoked
// to obtain per-cycle UserData; on error the statically configured
// Consumer.Group.Member.UserData is used and the error is logged.
func (c *consumerGroup) subscriptionMetadata(strategy BalanceStrategy, topics []string, owned map[string][]int32, generationID int32) *ConsumerGroupMemberMetadata {
	meta := &ConsumerGroupMemberMetadata{
		Version:         c.subscriptionVersion(),
		Topics:          topics,
		UserData:        c.userData,
		OwnedPartitions: ownedPartitions(owned),
		GenerationID:    generationID,
	}

	p, ok := strategy.(SubscriptionUserDataBalanceStrategy)
	if !ok {
		return meta
	}

	// Hand the provider a throwaway copy so it cannot mutate the slice
	// we later attach to the JoinGroup request.
	userData, err := p.SubscriptionUserData(slices.Clone(topics))
	if err != nil {
		Logger.Printf(
			"consumergroup/%s: falling back to static user data for strategy %q due to %v\n",
			c.groupID, strategy.Name(), err,
		)
		return meta
	}
	meta.UserData = userData
	return meta
}

// findStrategy returns the BalanceStrategy with the specified protocolName
// from the slice provided.
func (c *consumerGroup) findStrategy(name string, groupStrategies []BalanceStrategy) (BalanceStrategy, bool) {
	for _, strategy := range groupStrategies {
		if strategy.Name() == name {
			return strategy, true
		}
	}
	return nil, false
}

func (c *consumerGroup) syncGroupRequest(
	coordinator *Broker,
	members map[string]ConsumerGroupMemberMetadata,
	plan BalanceStrategyPlan,
	generationID int32,
	strategy BalanceStrategy,
) (*SyncGroupResponse, error) {
	req := &SyncGroupRequest{
		GroupId:      c.groupID,
		MemberId:     c.memberID,
		GenerationId: generationID,
	}

	if c.config.Version.IsAtLeast(V2_5_0_0) {
		req.Version = 5
	} else if c.config.Version.IsAtLeast(V2_4_0_0) {
		req.Version = 4
	} else if c.config.Version.IsAtLeast(V2_3_0_0) {
		req.Version = 3
	} else if c.config.Version.IsAtLeast(V2_0_0_0) {
		req.Version = 2
	} else if c.config.Version.IsAtLeast(V0_11_0_0) {
		req.Version = 1
	}
	// Starting from version 3, we add a new field called groupInstanceId to indicate member identity across restarts.
	if req.Version >= 3 {
		req.GroupInstanceId = c.groupInstanceId
	}
	if req.Version >= 5 {
		protocolType := "consumer"
		protocolName := strategy.Name()
		req.ProtocolType = &protocolType
		req.ProtocolName = &protocolName
	}

	for memberID, topics := range plan {
		assignment := &ConsumerGroupMemberAssignment{Topics: topics}
		userDataBytes, err := strategy.AssignmentData(memberID, topics, generationID)
		if err != nil {
			return nil, err
		}
		assignment.UserData = userDataBytes
		if err := req.AddGroupAssignmentMember(memberID, assignment); err != nil {
			return nil, err
		}
		delete(members, memberID)
	}
	// add empty assignments for any remaining members
	for memberID := range members {
		if err := req.AddGroupAssignmentMember(memberID, &ConsumerGroupMemberAssignment{}); err != nil {
			return nil, err
		}
	}

	return coordinator.SyncGroup(req)
}

func (c *consumerGroup) heartbeatRequest(coordinator *Broker, memberID string, generationID int32) (*HeartbeatResponse, error) {
	req := &HeartbeatRequest{
		GroupId:      c.groupID,
		MemberId:     memberID,
		GenerationId: generationID,
	}

	// Version 1 and version 2 are the same as version 0.
	if c.config.Version.IsAtLeast(V0_11_0_0) {
		req.Version = 1
	}
	if c.config.Version.IsAtLeast(V2_0_0_0) {
		req.Version = 2
	}
	// Starting from version 3, we add a new field called groupInstanceId to indicate member identity across restarts.
	if c.config.Version.IsAtLeast(V2_3_0_0) {
		req.Version = 3
		req.GroupInstanceId = c.groupInstanceId
		// Version 4 is the first flexible version
		if c.config.Version.IsAtLeast(V2_4_0_0) {
			req.Version = 4
		}
	}

	return coordinator.Heartbeat(req)
}

func (c *consumerGroup) balance(strategy BalanceStrategy, members map[string]ConsumerGroupMemberMetadata) (map[string][]int32, []string, BalanceStrategyPlan, error) {
	topicPartitions := make(map[string][]int32)
	for _, meta := range members {
		for _, topic := range meta.Topics {
			topicPartitions[topic] = nil
		}
	}

	allSubscribedTopics := make([]string, 0, len(topicPartitions))
	for topic := range topicPartitions {
		allSubscribedTopics = append(allSubscribedTopics, topic)
	}

	// refresh metadata for all the subscribed topics in the consumer group
	// to avoid using stale metadata to assigning partitions
	err := c.client.RefreshMetadata(allSubscribedTopics...)
	if err != nil {
		return nil, nil, nil, err
	}

	for topic := range topicPartitions {
		partitions, err := c.client.Partitions(topic)
		if err != nil {
			return nil, nil, nil, err
		}
		topicPartitions[topic] = partitions
	}

	plan, err := strategy.Plan(members, topicPartitions)
	return topicPartitions, allSubscribedTopics, plan, err
}

// Leaves the cluster, called by Close.
func (c *consumerGroup) leave() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.memberID == "" {
		return nil
	}

	coordinator, err := c.client.Coordinator(c.groupID)
	if err != nil {
		return err
	}

	// as per KIP-345 if groupInstanceId is set, i.e. static membership is in action, then do not leave group when consumer closed, just clear memberID
	if c.groupInstanceId != nil {
		c.memberID = ""
		return nil
	}
	req := &LeaveGroupRequest{
		GroupId:  c.groupID,
		MemberId: c.memberID,
	}
	if c.config.Version.IsAtLeast(V3_2_0_0) {
		req.Version = 5
	} else if c.config.Version.IsAtLeast(V2_4_0_0) {
		req.Version = 4
	} else if c.config.Version.IsAtLeast(V2_0_0_0) {
		req.Version = 2
	} else if c.config.Version.IsAtLeast(V0_11_0_0) {
		req.Version = 1
	}
	if req.Version >= 3 {
		member := MemberIdentity{
			MemberId: c.memberID,
		}
		if req.Version >= 5 {
			reason := "the consumer is being closed"
			member.Reason = &reason
		}
		req.Members = append(req.Members, member)
	}

	resp, err := coordinator.LeaveGroup(req)
	if err != nil {
		_ = coordinator.Close()
		return err
	}

	// clear the memberID
	c.memberID = ""

	switch resp.Err {
	case ErrRebalanceInProgress, ErrUnknownMemberId, ErrNoError:
		return nil
	default:
		return resp.Err
	}
}

func (c *consumerGroup) handleError(err error, topic string, partition int32) {
	var consumerError *ConsumerError
	if ok := errors.As(err, &consumerError); !ok && topic != "" && partition > -1 {
		err = &ConsumerError{
			Topic:     topic,
			Partition: partition,
			Err:       err,
		}
	}

	if !c.config.Consumer.Return.Errors {
		Logger.Println(err)
		return
	}

	c.errorsLock.RLock()
	defer c.errorsLock.RUnlock()
	select {
	case <-c.closed:
		// consumer is closed
		return
	default:
	}

	select {
	case c.errors <- err:
	default:
		// no error listener
	}
}

// loopCheckPartitionNumbers triggers a rebalance when a subscribed topic grows
func (c *consumerGroup) loopCheckPartitionNumbers(ctx context.Context, allSubscribedTopicPartitions map[string][]int32, topics []string, session *consumerGroupSession) {
	if c.config.Metadata.RefreshFrequency == time.Duration(0) {
		return
	}

	oldTopicToPartitionNum := make(map[string]int, len(allSubscribedTopicPartitions))
	for topic, partitions := range allSubscribedTopicPartitions {
		oldTopicToPartitionNum[topic] = len(partitions)
	}

	pause := time.NewTicker(c.config.Metadata.RefreshFrequency)
	defer pause.Stop()
	for {
		if newTopicToPartitionNum, err := c.topicToPartitionNumbers(topics); err != nil {
			session.triggerRebalance(ErrSessionPartitionCountChanged)
			return
		} else {
			for topic, num := range oldTopicToPartitionNum {
				if newTopicToPartitionNum[topic] != num {
					Logger.Printf(
						"consumergroup/%s loop check partition number goroutine find partitions in topics %s changed from %d to %d\n",
						c.groupID, topics, num, newTopicToPartitionNum[topic])
					session.triggerRebalance(ErrSessionPartitionCountChanged)
					return
				}
			}
		}
		select {
		case <-pause.C:
		case <-ctx.Done():
			Logger.Printf(
				"consumergroup/%s loop check partition number goroutine will exit, topics %s\n",
				c.groupID, topics)
			return
		case <-c.closed:
			return
		}
	}
}

func (c *consumerGroup) topicToPartitionNumbers(topics []string) (map[string]int, error) {
	topicToPartitionNum := make(map[string]int, len(topics))
	for _, topic := range topics {
		if partitionNum, err := c.client.Partitions(topic); err != nil {
			Logger.Printf(
				"consumergroup/%s topic %s get partition number failed due to '%v'\n",
				c.groupID, topic, err)
			return nil, err
		} else {
			topicToPartitionNum[topic] = len(partitionNum)
		}
	}
	return topicToPartitionNum, nil
}
