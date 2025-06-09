package sarama

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

// Client is a generic Kafka client. It manages connections to one or more Kafka brokers.
// You MUST call Close() on a client to avoid leaks, it will not be garbage-collected
// automatically when it passes out of scope. It is safe to share a client amongst many
// users, however Kafka will process requests from a single client strictly in serial,
// so it is generally more efficient to use the default one client per producer/consumer.
type Client interface {
	// Config returns the Config struct of the client. This struct should not be
	// altered after it has been created.
	Config() *Config

	// Controller returns the cluster controller broker. It will return a
	// locally cached value if it's available. You can call RefreshController
	// to update the cached value. Requires Kafka 0.10 or higher.
	Controller() (*Broker, error)

	// RefreshController retrieves the cluster controller from fresh metadata
	// and stores it in the local cache. Requires Kafka 0.10 or higher.
	RefreshController() (*Broker, error)

	// Brokers returns the current set of active brokers as retrieved from cluster metadata.
	Brokers() []*Broker

	// Broker returns the active Broker if available for the broker ID.
	Broker(brokerID int32) (*Broker, error)

	// Topics returns the set of available topics as retrieved from cluster metadata.
	Topics() ([]string, error)

	// Partitions returns the sorted list of all partition IDs for the given topic.
	Partitions(topic string) ([]int32, error)

	// WritablePartitions returns the sorted list of all writable partition IDs for
	// the given topic, where "writable" means "having a valid leader accepting
	// writes".
	WritablePartitions(topic string) ([]int32, error)

	// Leader returns the broker object that is the leader of the current
	// topic/partition, as determined by querying the cluster metadata.
	Leader(topic string, partitionID int32) (*Broker, error)

	// LeaderAndEpoch returns the leader and its epoch for the current
	// topic/partition, as determined by querying the cluster metadata.
	LeaderAndEpoch(topic string, partitionID int32) (*Broker, int32, error)

	// Replicas returns the set of all replica IDs for the given partition.
	Replicas(topic string, partitionID int32) ([]int32, error)

	// InSyncReplicas returns the set of all in-sync replica IDs for the given
	// partition. In-sync replicas are replicas which are fully caught up with
	// the partition leader.
	InSyncReplicas(topic string, partitionID int32) ([]int32, error)

	// OfflineReplicas returns the set of all offline replica IDs for the given
	// partition. Offline replicas are replicas which are offline
	OfflineReplicas(topic string, partitionID int32) ([]int32, error)

	// RefreshBrokers takes a list of addresses to be used as seed brokers.
	// Existing broker connections are closed and the updated list of seed brokers
	// will be used for the next metadata fetch.
	RefreshBrokers(addrs []string) error

	// RefreshMetadata takes a list of topics and queries the cluster to refresh the
	// available metadata for those topics. If no topics are provided, it will refresh
	// metadata for all topics.
	RefreshMetadata(topics ...string) error

	// GetOffset queries the cluster to get the most recent available offset at the
	// given time (in milliseconds) on the topic/partition combination.
	// Time should be OffsetOldest for the earliest available offset,
	// OffsetNewest for the offset of the message that will be produced next, or a time.
	GetOffset(topic string, partitionID int32, time int64) (int64, error)

	// Coordinator returns the coordinating broker for a consumer group. It will
	// return a locally cached value if it's available. You can call
	// RefreshCoordinator to update the cached value. This function only works on
	// Kafka 0.8.2 and higher.
	Coordinator(consumerGroup string) (*Broker, error)

	// RefreshCoordinator retrieves the coordinator for a consumer group and stores it
	// in local cache. This function only works on Kafka 0.8.2 and higher.
	RefreshCoordinator(consumerGroup string) error

	// Coordinator returns the coordinating broker for a transaction id. It will
	// return a locally cached value if it's available. You can call
	// RefreshCoordinator to update the cached value. This function only works on
	// Kafka 0.11.0.0 and higher.
	TransactionCoordinator(transactionID string) (*Broker, error)

	// RefreshCoordinator retrieves the coordinator for a transaction id and stores it
	// in local cache. This function only works on Kafka 0.11.0.0 and higher.
	RefreshTransactionCoordinator(transactionID string) error

	// InitProducerID retrieves information required for Idempotent Producer
	InitProducerID() (*InitProducerIDResponse, error)

	// LeastLoadedBroker retrieves broker that has the least responses pending
	LeastLoadedBroker() *Broker

	// check if partition is readable
	PartitionNotReadable(topic string, partition int32) bool

	// Close shuts down all broker connections managed by this client. It is required
	// to call this function before a client object passes out of scope, as it will
	// otherwise leak memory. You must close any Producers or Consumers using a client
	// before you close the client.
	Close() error

	// Closed returns true if the client has already had Close called on it
	Closed() bool
}

const (
	// OffsetNewest stands for the log head offset, i.e. the offset that will be
	// assigned to the next message that will be produced to the partition. You
	// can send this to a client's GetOffset method to get this offset, or when
	// calling ConsumePartition to start consuming new messages.
	OffsetNewest int64 = -1
	// OffsetOldest stands for the oldest offset available on the broker for a
	// partition. You can send this to a client's GetOffset method to get this
	// offset, or when calling ConsumePartition to start consuming from the
	// oldest offset that is still available on the broker.
	OffsetOldest int64 = -2
)

type client struct {
	// updateMetadataMs stores the time at which metadata was lasted updated.
	// Note: this accessed atomically so must be the first word in the struct
	// as per golang/go#41970
	updateMetadataMs int64

	conf           *Config
	closer, closed chan none // for shutting down background metadata updater

	// the broker addresses given to us through the constructor are not guaranteed to be returned in
	// the cluster metadata (I *think* it only returns brokers who are currently leading partitions?)
	// so we store them separately
	seedBrokers []*Broker
	deadSeeds   []*Broker

	controllerID            int32                                   // cluster controller broker id
	brokers                 map[int32]*Broker                       // maps broker ids to brokers
	metadata                map[string]map[int32]*PartitionMetadata // maps topics to partition ids to metadata
	metadataTopics          map[string]none                         // topics that need to collect metadata
	coordinators            map[string]int32                        // Maps consumer group names to coordinating broker IDs
	transactionCoordinators map[string]int32                        // Maps transaction ids to coordinating broker IDs

	// If the number of partitions is large, we can get some churn calling cachedPartitions,
	// so the result is cached.  It is important to update this value whenever metadata is changed
	cachedPartitionsResults map[string][maxPartitionIndex][]int32

	lock sync.RWMutex // protects access to the maps that hold cluster state.
}

// NewClient creates a new Client. It connects to one of the given broker addresses
// and uses that broker to automatically fetch metadata on the rest of the kafka cluster. If metadata cannot
// be retrieved from any of the given broker addresses, the client is not created.
func NewClient(addrs []string, conf *Config) (Client, error) {
	DebugLogger.Println("Initializing new client")

	if conf == nil {
		conf = NewConfig()
	}

	if err := conf.Validate(); err != nil {
		return nil, err
	}

	if len(addrs) < 1 {
		return nil, ConfigurationError("You must provide at least one broker address")
	}

	if strings.Contains(addrs[0], ".servicebus.windows.net") {
		if conf.Version.IsAtLeast(V1_1_0_0) || !conf.Version.IsAtLeast(V0_11_0_0) {
			Logger.Println("Connecting to Azure Event Hubs, forcing version to V1_0_0_0 for compatibility")
			conf.Version = V1_0_0_0
		}
	}

	client := &client{
		conf:                    conf,
		closer:                  make(chan none),
		closed:                  make(chan none),
		brokers:                 make(map[int32]*Broker),
		metadata:                make(map[string]map[int32]*PartitionMetadata),
		metadataTopics:          make(map[string]none),
		cachedPartitionsResults: make(map[string][maxPartitionIndex][]int32),
		coordinators:            make(map[string]int32),
		transactionCoordinators: make(map[string]int32),
	}

	if conf.Net.ResolveCanonicalBootstrapServers {
		var err error
		addrs, err = client.resolveCanonicalNames(addrs)
		if err != nil {
			return nil, err
		}
	}

	client.randomizeSeedBrokers(addrs)

	if conf.Metadata.Full {
		// do an initial fetch of all cluster metadata by specifying an empty list of topics
		err := client.RefreshMetadata()
		if err == nil {
		} else if errors.Is(err, ErrLeaderNotAvailable) || errors.Is(err, ErrReplicaNotAvailable) || errors.Is(err, ErrTopicAuthorizationFailed) || errors.Is(err, ErrClusterAuthorizationFailed) {
			// indicates that maybe part of the cluster is down, but is not fatal to creating the client
			Logger.Println(err)
		} else {
			close(client.closed) // we haven't started the background updater yet, so we have to do this manually
			_ = client.Close()
			return nil, err
		}
	}
	go withRecover(client.backgroundMetadataUpdater)

	DebugLogger.Println("Successfully initialized new client")

	return client, nil
}

func (client *client) Config() *Config {
	return client.conf
}

func (client *client) Brokers() []*Broker {
	client.lock.RLock()
	defer client.lock.RUnlock()
	brokers := make([]*Broker, 0, len(client.brokers))
	for _, broker := range client.brokers {
		brokers = append(brokers, broker)
	}
	return brokers
}

func (client *client) Broker(brokerID int32) (*Broker, error) {
	client.lock.RLock()
	defer client.lock.RUnlock()
	broker, ok := client.brokers[brokerID]
	if !ok {
		return nil, ErrBrokerNotFound
	}
	_ = broker.Open(client.conf)
	return broker, nil
}

func (client *client) InitProducerID() (*InitProducerIDResponse, error) {
	// FIXME: this InitProducerID seems to only be called from client_test.go (TestInitProducerIDConnectionRefused) and has been superceded by transaction_manager.go?
	brokerErrors := make([]error, 0)
	for broker := client.LeastLoadedBroker(); broker != nil; broker = client.LeastLoadedBroker() {
		request := &InitProducerIDRequest{}

		if client.conf.Version.IsAtLeast(V2_7_0_0) {
			// Version 4 adds the support for new error code PRODUCER_FENCED.
			request.Version = 4
		} else if client.conf.Version.IsAtLeast(V2_5_0_0) {
			// Version 3 adds ProducerId and ProducerEpoch, allowing producers to try to resume after an INVALID_PRODUCER_EPOCH error
			request.Version = 3
		} else if client.conf.Version.IsAtLeast(V2_4_0_0) {
			// Version 2 is the first flexible version.
			request.Version = 2
		} else if client.conf.Version.IsAtLeast(V2_0_0_0) {
			// Version 1 is the same as version 0.
			request.Version = 1
		}

		response, err := broker.InitProducerID(request)
		if err == nil {
			return response, nil
		} else {
			// some error, remove that broker and try again
			Logger.Printf("Client got error from broker %d when issuing InitProducerID : %v\n", broker.ID(), err)
			_ = broker.Close()
			brokerErrors = append(brokerErrors, err)
			client.deregisterBroker(broker)
		}
	}

	return nil, Wrap(ErrOutOfBrokers, brokerErrors...)
}

func (client *client) Close() error {
	if client.Closed() {
		// Chances are this is being called from a defer() and the error will go unobserved
		// so we go ahead and log the event in this case.
		Logger.Printf("Close() called on already closed client")
		return ErrClosedClient
	}

	// shutdown and wait for the background thread before we take the lock, to avoid races
	close(client.closer)
	<-client.closed

	client.lock.Lock()
	defer client.lock.Unlock()
	DebugLogger.Println("Closing Client")

	for _, broker := range client.brokers {
		safeAsyncClose(broker)
	}

	for _, broker := range client.seedBrokers {
		safeAsyncClose(broker)
	}

	client.brokers = nil
	client.metadata = nil
	client.metadataTopics = nil

	return nil
}

func (client *client) Closed() bool {
	client.lock.RLock()
	defer client.lock.RUnlock()

	return client.brokers == nil
}

func (client *client) Topics() ([]string, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	client.lock.RLock()
	defer client.lock.RUnlock()

	ret := make([]string, 0, len(client.metadata))
	for topic := range client.metadata {
		ret = append(ret, topic)
	}

	return ret, nil
}

func (client *client) MetadataTopics() ([]string, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	client.lock.RLock()
	defer client.lock.RUnlock()

	ret := make([]string, 0, len(client.metadataTopics))
	for topic := range client.metadataTopics {
		ret = append(ret, topic)
	}

	return ret, nil
}

func (client *client) Partitions(topic string) ([]int32, error) {
	return client.getPartitions(topic, allPartitions)
}

func (client *client) WritablePartitions(topic string) ([]int32, error) {
	return client.getPartitions(topic, writablePartitions)
}

func (client *client) getPartitions(topic string, pt partitionType) ([]int32, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	partitions := client.cachedPartitions(topic, pt)

	// len==0 catches when it's nil (no such topic) and the odd case when every single
	// partition is undergoing leader election simultaneously. Callers have to be able to handle
	// this function returning an empty slice (which is a valid return value) but catching it
	// here the first time (note we *don't* catch it below where we return ErrUnknownTopicOrPartition) triggers
	// a metadata refresh as a nicety so callers can just try again and don't have to manually
	// trigger a refresh (otherwise they'd just keep getting a stale cached copy).
	if len(partitions) == 0 {
		err := client.RefreshMetadata(topic)
		if err != nil {
			return nil, err
		}
		partitions = client.cachedPartitions(topic, pt)
	}

	if partitions == nil {
		return nil, ErrUnknownTopicOrPartition
	}

	return partitions, nil
}

func (client *client) Replicas(topic string, partitionID int32) ([]int32, error) {
	return client.getReplicas(topic, partitionID, func(metadata *PartitionMetadata) []int32 {
		return metadata.Replicas
	})
}

func (client *client) InSyncReplicas(topic string, partitionID int32) ([]int32, error) {
	return client.getReplicas(topic, partitionID, func(metadata *PartitionMetadata) []int32 {
		return metadata.Isr
	})
}

func (client *client) OfflineReplicas(topic string, partitionID int32) ([]int32, error) {
	return client.getReplicas(topic, partitionID, func(metadata *PartitionMetadata) []int32 {
		return metadata.OfflineReplicas
	})
}

func (client *client) getReplicas(topic string, partitionID int32, extractor func(metadata *PartitionMetadata) []int32) ([]int32, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	metadata := client.cachedMetadata(topic, partitionID)

	if metadata == nil {
		err := client.RefreshMetadata(topic)
		if err != nil {
			return nil, err
		}
		metadata = client.cachedMetadata(topic, partitionID)
	}

	if metadata == nil {
		return nil, ErrUnknownTopicOrPartition
	}

	replicas := extractor(metadata)
	if errors.Is(metadata.Err, ErrReplicaNotAvailable) {
		return dupInt32Slice(replicas), metadata.Err
	}
	return dupInt32Slice(replicas), nil
}

func (client *client) Leader(topic string, partitionID int32) (*Broker, error) {
	leader, _, err := client.LeaderAndEpoch(topic, partitionID)
	return leader, err
}

func (client *client) LeaderAndEpoch(topic string, partitionID int32) (*Broker, int32, error) {
	if client.Closed() {
		return nil, -1, ErrClosedClient
	}

	leader, epoch, err := client.cachedLeader(topic, partitionID)
	if leader == nil {
		err = client.RefreshMetadata(topic)
		if err != nil {
			return nil, -1, err
		}
		leader, epoch, err = client.cachedLeader(topic, partitionID)
	}

	return leader, epoch, err
}

func (client *client) RefreshBrokers(addrs []string) error {
	if client.Closed() {
		return ErrClosedClient
	}

	client.lock.Lock()
	defer client.lock.Unlock()

	for _, broker := range client.brokers {
		safeAsyncClose(broker)
	}
	client.brokers = make(map[int32]*Broker)

	for _, broker := range client.seedBrokers {
		safeAsyncClose(broker)
	}

	for _, broker := range client.deadSeeds {
		safeAsyncClose(broker)
	}

	client.seedBrokers = nil
	client.deadSeeds = nil

	client.randomizeSeedBrokers(addrs)

	return nil
}

func (client *client) RefreshMetadata(topics ...string) error {
	if client.Closed() {
		return ErrClosedClient
	}

	// Prior to 0.8.2, Kafka will throw exceptions on an empty topic and not return a proper
	// error. This handles the case by returning an error instead of sending it
	// off to Kafka. See: https://github.com/IBM/sarama/pull/38#issuecomment-26362310
	for _, topic := range topics {
		if topic == "" {
			return ErrInvalidTopic // this is the error that 0.8.2 and later correctly return
		}
	}

	deadline := time.Time{}
	if client.conf.Metadata.Timeout > 0 {
		deadline = time.Now().Add(client.conf.Metadata.Timeout)
	}
	return client.tryRefreshMetadata(topics, client.conf.Metadata.Retry.Max, deadline)
}

func (client *client) GetOffset(topic string, partitionID int32, timestamp int64) (int64, error) {
	if client.Closed() {
		return -1, ErrClosedClient
	}

	offset, err := client.getOffset(topic, partitionID, timestamp)
	if err != nil {
		if err := client.RefreshMetadata(topic); err != nil {
			return -1, err
		}
		return client.getOffset(topic, partitionID, timestamp)
	}

	return offset, err
}

func (client *client) Controller() (*Broker, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	if !client.conf.Version.IsAtLeast(V0_10_0_0) {
		return nil, ErrUnsupportedVersion
	}

	controller := client.cachedController()
	if controller == nil {
		if err := client.refreshMetadata(); err != nil {
			return nil, err
		}
		controller = client.cachedController()
	}

	if controller == nil {
		return nil, ErrControllerNotAvailable
	}

	_ = controller.Open(client.conf)
	return controller, nil
}

// deregisterController removes the cached controllerID
func (client *client) deregisterController() {
	client.lock.Lock()
	defer client.lock.Unlock()
	if controller, ok := client.brokers[client.controllerID]; ok {
		_ = controller.Close()
		delete(client.brokers, client.controllerID)
	}
}

// RefreshController retrieves the cluster controller from fresh metadata
// and stores it in the local cache. Requires Kafka 0.10 or higher.
func (client *client) RefreshController() (*Broker, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	client.deregisterController()

	if err := client.refreshMetadata(); err != nil {
		return nil, err
	}

	controller := client.cachedController()
	if controller == nil {
		return nil, ErrControllerNotAvailable
	}

	_ = controller.Open(client.conf)
	return controller, nil
}

func (client *client) Coordinator(consumerGroup string) (*Broker, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	coordinator := client.cachedCoordinator(consumerGroup)

	if coordinator == nil {
		if err := client.RefreshCoordinator(consumerGroup); err != nil {
			return nil, err
		}
		coordinator = client.cachedCoordinator(consumerGroup)
	}

	if coordinator == nil {
		return nil, ErrConsumerCoordinatorNotAvailable
	}

	_ = coordinator.Open(client.conf)
	return coordinator, nil
}

func (client *client) RefreshCoordinator(consumerGroup string) error {
	if client.Closed() {
		return ErrClosedClient
	}

	response, err := client.findCoordinator(consumerGroup, CoordinatorGroup, client.conf.Metadata.Retry.Max)
	if err != nil {
		return err
	}

	client.lock.Lock()
	defer client.lock.Unlock()
	client.registerBroker(response.Coordinator)
	client.coordinators[consumerGroup] = response.Coordinator.ID()
	return nil
}

func (client *client) TransactionCoordinator(transactionID string) (*Broker, error) {
	if client.Closed() {
		return nil, ErrClosedClient
	}

	coordinator := client.cachedTransactionCoordinator(transactionID)

	if coordinator == nil {
		if err := client.RefreshTransactionCoordinator(transactionID); err != nil {
			return nil, err
		}
		coordinator = client.cachedTransactionCoordinator(transactionID)
	}

	if coordinator == nil {
		return nil, ErrConsumerCoordinatorNotAvailable
	}

	_ = coordinator.Open(client.conf)
	return coordinator, nil
}

func (client *client) RefreshTransactionCoordinator(transactionID string) error {
	if client.Closed() {
		return ErrClosedClient
	}

	response, err := client.findCoordinator(transactionID, CoordinatorTransaction, client.conf.Metadata.Retry.Max)
	if err != nil {
		return err
	}

	client.lock.Lock()
	defer client.lock.Unlock()
	client.registerBroker(response.Coordinator)
	client.transactionCoordinators[transactionID] = response.Coordinator.ID()
	return nil
}

// private broker management helpers

func (client *client) randomizeSeedBrokers(addrs []string) {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	for _, index := range random.Perm(len(addrs)) {
		client.seedBrokers = append(client.seedBrokers, NewBroker(addrs[index]))
	}
}

func (client *client) updateBroker(brokers []*Broker) {
	currentBroker := make(map[int32]*Broker, len(brokers))

	for _, broker := range brokers {
		currentBroker[broker.ID()] = broker
		if client.brokers[broker.ID()] == nil { // add new broker
			client.brokers[broker.ID()] = broker
			DebugLogger.Printf("client/brokers registered new broker #%d at %s", broker.ID(), broker.Addr())
		} else if broker.Addr() != client.brokers[broker.ID()].Addr() { // replace broker with new address
			safeAsyncClose(client.brokers[broker.ID()])
			client.brokers[broker.ID()] = broker
			Logger.Printf("client/brokers replaced registered broker #%d with %s", broker.ID(), broker.Addr())
		}
	}

	for id, broker := range client.brokers {
		if _, exist := currentBroker[id]; !exist { // remove old broker
			safeAsyncClose(broker)
			delete(client.brokers, id)
			Logger.Printf("client/broker remove invalid broker #%d with %s", broker.ID(), broker.Addr())
		}
	}
}

// registerBroker makes sure a broker received by a Metadata or Coordinator request is registered
// in the brokers map. It returns the broker that is registered, which may be the provided broker,
// or a previously registered Broker instance. You must hold the write lock before calling this function.
func (client *client) registerBroker(broker *Broker) {
	if client.brokers == nil {
		Logger.Printf("cannot register broker #%d at %s, client already closed", broker.ID(), broker.Addr())
		return
	}

	if client.brokers[broker.ID()] == nil {
		client.brokers[broker.ID()] = broker
		DebugLogger.Printf("client/brokers registered new broker #%d at %s", broker.ID(), broker.Addr())
	} else if broker.Addr() != client.brokers[broker.ID()].Addr() {
		safeAsyncClose(client.brokers[broker.ID()])
		client.brokers[broker.ID()] = broker
		Logger.Printf("client/brokers replaced registered broker #%d with %s", broker.ID(), broker.Addr())
	}
}

// deregisterBroker removes a broker from the broker list, and if it's
// not in the broker list, removes it from seedBrokers.
func (client *client) deregisterBroker(broker *Broker) {
	client.lock.Lock()
	defer client.lock.Unlock()

	_, ok := client.brokers[broker.ID()]
	if ok {
		Logger.Printf("client/brokers deregistered broker #%d at %s", broker.ID(), broker.Addr())
		delete(client.brokers, broker.ID())
		return
	}
	if len(client.seedBrokers) > 0 && broker == client.seedBrokers[0] {
		client.deadSeeds = append(client.deadSeeds, broker)
		client.seedBrokers = client.seedBrokers[1:]
	}
}

func (client *client) resurrectDeadBrokers() {
	client.lock.Lock()
	defer client.lock.Unlock()

	Logger.Printf("client/brokers resurrecting %d dead seed brokers", len(client.deadSeeds))
	client.seedBrokers = append(client.seedBrokers, client.deadSeeds...)
	client.deadSeeds = nil
}

// LeastLoadedBroker returns the broker with the least pending requests.
// Firstly, choose the broker from cached broker list. If the broker list is empty, choose from seed brokers.
func (client *client) LeastLoadedBroker() *Broker {
	client.lock.RLock()
	defer client.lock.RUnlock()

	var leastLoadedBroker *Broker
	pendingRequests := math.MaxInt
	for _, broker := range client.brokers {
		if pendingRequests > broker.ResponseSize() {
			pendingRequests = broker.ResponseSize()
			leastLoadedBroker = broker
		}
	}
	if leastLoadedBroker != nil {
		_ = leastLoadedBroker.Open(client.conf)
		return leastLoadedBroker
	}

	if len(client.seedBrokers) > 0 {
		_ = client.seedBrokers[0].Open(client.conf)
		return client.seedBrokers[0]
	}

	return leastLoadedBroker
}

// private caching/lazy metadata helpers

type partitionType int

const (
	allPartitions partitionType = iota
	writablePartitions
	// If you add any more types, update the partition cache in update()

	// Ensure this is the last partition type value
	maxPartitionIndex
)

func (client *client) cachedMetadata(topic string, partitionID int32) *PartitionMetadata {
	client.lock.RLock()
	defer client.lock.RUnlock()

	partitions := client.metadata[topic]
	if partitions != nil {
		return partitions[partitionID]
	}

	return nil
}

func (client *client) cachedPartitions(topic string, partitionSet partitionType) []int32 {
	client.lock.RLock()
	defer client.lock.RUnlock()

	partitions, exists := client.cachedPartitionsResults[topic]

	if !exists {
		return nil
	}
	return partitions[partitionSet]
}

func (client *client) setPartitionCache(topic string, partitionSet partitionType) []int32 {
	partitions := client.metadata[topic]

	if partitions == nil {
		return nil
	}

	ret := make([]int32, 0, len(partitions))
	for _, partition := range partitions {
		if partitionSet == writablePartitions && errors.Is(partition.Err, ErrLeaderNotAvailable) {
			continue
		}
		ret = append(ret, partition.ID)
	}

	sort.Sort(int32Slice(ret))
	return ret
}

func (client *client) cachedLeader(topic string, partitionID int32) (*Broker, int32, error) {
	client.lock.RLock()
	defer client.lock.RUnlock()

	partitions := client.metadata[topic]
	if partitions != nil {
		metadata, ok := partitions[partitionID]
		if ok {
			if errors.Is(metadata.Err, ErrLeaderNotAvailable) {
				return nil, -1, ErrLeaderNotAvailable
			}
			b := client.brokers[metadata.Leader]
			if b == nil {
				return nil, -1, ErrLeaderNotAvailable
			}
			_ = b.Open(client.conf)
			return b, metadata.LeaderEpoch, nil
		}
	}

	return nil, -1, ErrUnknownTopicOrPartition
}

func (client *client) getOffset(topic string, partitionID int32, timestamp int64) (int64, error) {
	broker, err := client.Leader(topic, partitionID)
	if err != nil {
		return -1, err
	}

	request := &OffsetRequest{}
	if client.conf.Version.IsAtLeast(V2_1_0_0) {
		// Version 4 adds the current leader epoch, which is used for fencing.
		request.Version = 4
	} else if client.conf.Version.IsAtLeast(V2_0_0_0) {
		// Version 3 is the same as version 2.
		request.Version = 3
	} else if client.conf.Version.IsAtLeast(V0_11_0_0) {
		// Version 2 adds the isolation level, which is used for transactional reads.
		request.Version = 2
	} else if client.conf.Version.IsAtLeast(V0_10_1_0) {
		// Version 1 removes MaxNumOffsets.  From this version forward, only a single
		// offset can be returned.
		request.Version = 1
	}

	request.AddBlock(topic, partitionID, timestamp, 1)

	response, err := broker.GetAvailableOffsets(request)
	if err != nil {
		_ = broker.Close()
		return -1, err
	}

	block := response.GetBlock(topic, partitionID)
	if block == nil {
		_ = broker.Close()
		return -1, ErrIncompleteResponse
	}
	if !errors.Is(block.Err, ErrNoError) {
		return -1, block.Err
	}
	if len(block.Offsets) != 1 {
		return -1, ErrOffsetOutOfRange
	}

	return block.Offsets[0], nil
}

// core metadata update logic

func (client *client) backgroundMetadataUpdater() {
	defer close(client.closed)

	if client.conf.Metadata.RefreshFrequency == time.Duration(0) {
		return
	}

	ticker := time.NewTicker(client.conf.Metadata.RefreshFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := client.refreshMetadata(); err != nil {
				Logger.Println("Client background metadata update:", err)
			}
		case <-client.closer:
			return
		}
	}
}

func (client *client) refreshMetadata() error {
	var topics []string

	if !client.conf.Metadata.Full {
		if specificTopics, err := client.MetadataTopics(); err != nil {
			return err
		} else if len(specificTopics) == 0 {
			return ErrNoTopicsToUpdateMetadata
		} else {
			topics = specificTopics
		}
	}

	if err := client.RefreshMetadata(topics...); err != nil {
		return err
	}

	return nil
}

func (client *client) tryRefreshMetadata(topics []string, attemptsRemaining int, deadline time.Time) error {
	pastDeadline := func(backoff time.Duration) bool {
		if !deadline.IsZero() && time.Now().Add(backoff).After(deadline) {
			// we are past the deadline
			return true
		}
		return false
	}
	retry := func(err error) error {
		if attemptsRemaining > 0 {
			backoff := client.computeBackoff(attemptsRemaining)
			if pastDeadline(backoff) {
				Logger.Println("client/metadata skipping last retries as we would go past the metadata timeout")
				return err
			}
			if backoff > 0 {
				time.Sleep(backoff)
			}

			t := atomic.LoadInt64(&client.updateMetadataMs)
			if time.Since(time.UnixMilli(t)) < backoff {
				return err
			}
			attemptsRemaining--
			Logger.Printf("client/metadata retrying after %dms... (%d attempts remaining)\n", backoff/time.Millisecond, attemptsRemaining)

			return client.tryRefreshMetadata(topics, attemptsRemaining, deadline)
		}
		return err
	}

	broker := client.LeastLoadedBroker()
	brokerErrors := make([]error, 0)
	for ; broker != nil && !pastDeadline(0); broker = client.LeastLoadedBroker() {
		allowAutoTopicCreation := client.conf.Metadata.AllowAutoTopicCreation
		if len(topics) > 0 {
			DebugLogger.Printf("client/metadata fetching metadata for %v from broker %s\n", topics, broker.addr)
		} else {
			allowAutoTopicCreation = false
			DebugLogger.Printf("client/metadata fetching metadata for all topics from broker %s\n", broker.addr)
		}

		req := NewMetadataRequest(client.conf.Version, topics)
		req.AllowAutoTopicCreation = allowAutoTopicCreation
		atomic.StoreInt64(&client.updateMetadataMs, time.Now().UnixMilli())

		response, err := broker.GetMetadata(req)
		var kerror KError
		var packetEncodingError PacketEncodingError
		if err == nil {
			// When talking to the startup phase of a broker, it is possible to receive an empty metadata set. We should remove that broker and try next broker (https://issues.apache.org/jira/browse/KAFKA-7924).
			if len(response.Brokers) == 0 {
				Logger.Println("client/metadata receiving empty brokers from the metadata response when requesting the broker #%d at %s", broker.ID(), broker.addr)
				_ = broker.Close()
				client.deregisterBroker(broker)
				continue
			}
			allKnownMetaData := len(topics) == 0
			// valid response, use it
			shouldRetry, err := client.updateMetadata(response, allKnownMetaData)
			if shouldRetry {
				Logger.Println("client/metadata found some partitions to be leaderless")
				return retry(err) // note: err can be nil
			}
			return err
		} else if errors.As(err, &packetEncodingError) {
			// didn't even send, return the error
			return err
		} else if errors.As(err, &kerror) {
			// if SASL auth error return as this _should_ be a non retryable err for all brokers
			if errors.Is(err, ErrSASLAuthenticationFailed) {
				Logger.Println("client/metadata failed SASL authentication")
				return err
			}

			if errors.Is(err, ErrTopicAuthorizationFailed) {
				Logger.Println("client is not authorized to access this topic. The topics were: ", topics)
				return err
			}
			// else remove that broker and try again
			Logger.Printf("client/metadata got error from broker %d while fetching metadata: %v\n", broker.ID(), err)
			_ = broker.Close()
			client.deregisterBroker(broker)
		} else {
			// some other error, remove that broker and try again
			Logger.Printf("client/metadata got error from broker %d while fetching metadata: %v\n", broker.ID(), err)
			brokerErrors = append(brokerErrors, err)
			_ = broker.Close()
			client.deregisterBroker(broker)
		}
	}

	error := Wrap(ErrOutOfBrokers, brokerErrors...)
	if broker != nil {
		Logger.Printf("client/metadata not fetching metadata from broker %s as we would go past the metadata timeout\n", broker.addr)
		return retry(error)
	}

	Logger.Println("client/metadata no available broker to send metadata request to")
	client.resurrectDeadBrokers()
	return retry(error)
}

// if no fatal error, returns a list of topics that need retrying due to ErrLeaderNotAvailable
func (client *client) updateMetadata(data *MetadataResponse, allKnownMetaData bool) (retry bool, err error) {
	if client.Closed() {
		return
	}

	client.lock.Lock()
	defer client.lock.Unlock()

	// For all the brokers we received:
	// - if it is a new ID, save it
	// - if it is an existing ID, but the address we have is stale, discard the old one and save it
	// - if some brokers is not exist in it, remove old broker
	// - otherwise ignore it, replacing our existing one would just bounce the connection
	client.updateBroker(data.Brokers)

	client.controllerID = data.ControllerID

	if allKnownMetaData {
		client.metadata = make(map[string]map[int32]*PartitionMetadata)
		client.metadataTopics = make(map[string]none)
		client.cachedPartitionsResults = make(map[string][maxPartitionIndex][]int32)
	}
	for _, topic := range data.Topics {
		// topics must be added firstly to `metadataTopics` to guarantee that all
		// requested topics must be recorded to keep them trackable for periodically
		// metadata refresh.
		if _, exists := client.metadataTopics[topic.Name]; !exists {
			client.metadataTopics[topic.Name] = none{}
		}
		delete(client.metadata, topic.Name)
		delete(client.cachedPartitionsResults, topic.Name)

		switch topic.Err {
		case ErrNoError:
			// no-op
		case ErrInvalidTopic, ErrTopicAuthorizationFailed: // don't retry, don't store partial results
			err = topic.Err
			continue
		case ErrUnknownTopicOrPartition: // retry, do not store partial partition results
			err = topic.Err
			retry = true
			continue
		case ErrLeaderNotAvailable: // retry, but store partial partition results
			retry = true
		default: // don't retry, don't store partial results
			Logger.Printf("Unexpected topic-level metadata error: %s", topic.Err)
			err = topic.Err
			continue
		}

		client.metadata[topic.Name] = make(map[int32]*PartitionMetadata, len(topic.Partitions))
		for _, partition := range topic.Partitions {
			client.metadata[topic.Name][partition.ID] = partition
			if errors.Is(partition.Err, ErrLeaderNotAvailable) {
				retry = true
			}
		}

		var partitionCache [maxPartitionIndex][]int32
		partitionCache[allPartitions] = client.setPartitionCache(topic.Name, allPartitions)
		partitionCache[writablePartitions] = client.setPartitionCache(topic.Name, writablePartitions)
		client.cachedPartitionsResults[topic.Name] = partitionCache
	}

	return
}

func (client *client) cachedCoordinator(consumerGroup string) *Broker {
	client.lock.RLock()
	defer client.lock.RUnlock()
	if coordinatorID, ok := client.coordinators[consumerGroup]; ok {
		return client.brokers[coordinatorID]
	}
	return nil
}

func (client *client) cachedTransactionCoordinator(transactionID string) *Broker {
	client.lock.RLock()
	defer client.lock.RUnlock()
	if coordinatorID, ok := client.transactionCoordinators[transactionID]; ok {
		return client.brokers[coordinatorID]
	}
	return nil
}

func (client *client) cachedController() *Broker {
	client.lock.RLock()
	defer client.lock.RUnlock()

	return client.brokers[client.controllerID]
}

func (client *client) computeBackoff(attemptsRemaining int) time.Duration {
	if client.conf.Metadata.Retry.BackoffFunc != nil {
		maxRetries := client.conf.Metadata.Retry.Max
		retries := maxRetries - attemptsRemaining
		return client.conf.Metadata.Retry.BackoffFunc(retries, maxRetries)
	}
	return client.conf.Metadata.Retry.Backoff
}

func (client *client) findCoordinator(coordinatorKey string, coordinatorType CoordinatorType, attemptsRemaining int) (*FindCoordinatorResponse, error) {
	retry := func(err error) (*FindCoordinatorResponse, error) {
		if attemptsRemaining > 0 {
			backoff := client.computeBackoff(attemptsRemaining)
			attemptsRemaining--
			Logger.Printf("client/coordinator retrying after %dms... (%d attempts remaining)\n", backoff/time.Millisecond, attemptsRemaining)
			time.Sleep(backoff)
			return client.findCoordinator(coordinatorKey, coordinatorType, attemptsRemaining)
		}
		return nil, err
	}

	brokerErrors := make([]error, 0)
	for broker := client.LeastLoadedBroker(); broker != nil; broker = client.LeastLoadedBroker() {
		DebugLogger.Printf("client/coordinator requesting coordinator for %s from %s\n", coordinatorKey, broker.Addr())

		request := new(FindCoordinatorRequest)
		request.CoordinatorKey = coordinatorKey
		request.CoordinatorType = coordinatorType

		// Version 1 adds KeyType.
		if client.conf.Version.IsAtLeast(V0_11_0_0) {
			request.Version = 1
		}
		// Version 2 is the same as version 1.
		if client.conf.Version.IsAtLeast(V2_0_0_0) {
			request.Version = 2
		}

		response, err := broker.FindCoordinator(request)
		if err != nil {
			Logger.Printf("client/coordinator request to broker %s failed: %s\n", broker.Addr(), err)

			var packetEncodingError PacketEncodingError
			if errors.As(err, &packetEncodingError) {
				return nil, err
			} else {
				_ = broker.Close()
				brokerErrors = append(brokerErrors, err)
				client.deregisterBroker(broker)
				continue
			}
		}

		if errors.Is(response.Err, ErrNoError) {
			DebugLogger.Printf("client/coordinator coordinator for %s is #%d (%s)\n", coordinatorKey, response.Coordinator.ID(), response.Coordinator.Addr())
			return response, nil
		} else if errors.Is(response.Err, ErrConsumerCoordinatorNotAvailable) {
			Logger.Printf("client/coordinator coordinator for %s is not available\n", coordinatorKey)

			// This is very ugly, but this scenario will only happen once per cluster.
			// The __consumer_offsets topic only has to be created one time.
			// The number of partitions not configurable, but partition 0 should always exist.
			if _, err := client.Leader("__consumer_offsets", 0); err != nil {
				Logger.Printf("client/coordinator the __consumer_offsets topic is not initialized completely yet. Waiting 2 seconds...\n")
				time.Sleep(2 * time.Second)
			}
			if coordinatorType == CoordinatorTransaction {
				if _, err := client.Leader("__transaction_state", 0); err != nil {
					Logger.Printf("client/coordinator the __transaction_state topic is not initialized completely yet. Waiting 2 seconds...\n")
					time.Sleep(2 * time.Second)
				}
			}

			return retry(ErrConsumerCoordinatorNotAvailable)
		} else if errors.Is(response.Err, ErrGroupAuthorizationFailed) {
			Logger.Printf("client was not authorized to access group %s while attempting to find coordinator", coordinatorKey)
			return retry(ErrGroupAuthorizationFailed)
		} else {
			return nil, response.Err
		}
	}

	Logger.Println("client/coordinator no available broker to send consumer metadata request to")
	client.resurrectDeadBrokers()
	return retry(Wrap(ErrOutOfBrokers, brokerErrors...))
}

func (client *client) resolveCanonicalNames(addrs []string) ([]string, error) {
	ctx := context.Background()

	dialer := client.Config().getDialer()
	resolver := net.Resolver{
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			// dial func should only be called once, so switching within is acceptable
			switch d := dialer.(type) {
			case proxy.ContextDialer:
				return d.DialContext(ctx, network, address)
			default:
				// we have no choice but to ignore the context
				return d.Dial(network, address)
			}
		},
	}

	canonicalAddrs := make(map[string]struct{}, len(addrs)) // dedupe as we go
	for _, addr := range addrs {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err // message includes addr
		}

		ips, err := resolver.LookupHost(ctx, host)
		if err != nil {
			return nil, err // message includes host
		}
		for _, ip := range ips {
			ptrs, err := resolver.LookupAddr(ctx, ip)
			if err != nil {
				return nil, err // message includes ip
			}

			// unlike the Java client, we do not further check that PTRs resolve
			ptr := strings.TrimSuffix(ptrs[0], ".") // trailing dot breaks GSSAPI
			canonicalAddrs[net.JoinHostPort(ptr, port)] = struct{}{}
		}
	}

	addrs = make([]string, 0, len(canonicalAddrs))
	for addr := range canonicalAddrs {
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// nopCloserClient embeds an existing Client, but disables
// the Close method (yet all other methods pass
// through unchanged). This is for use in larger structs
// where it is undesirable to close the client that was
// passed in by the caller.
type nopCloserClient struct {
	Client
}

// Close intercepts and purposely does not call the underlying
// client's Close() method.
func (ncc *nopCloserClient) Close() error {
	return nil
}

func (client *client) PartitionNotReadable(topic string, partition int32) bool {
	client.lock.RLock()
	defer client.lock.RUnlock()

	pm := client.metadata[topic][partition]
	if pm == nil {
		return true
	}
	return pm.Leader == -1
}
