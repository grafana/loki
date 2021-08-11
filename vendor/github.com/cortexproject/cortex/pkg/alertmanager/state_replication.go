package alertmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/cluster/clusterpb"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alertstore"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	defaultSettleReadTimeout = 15 * time.Second
	defaultStoreReadTimeout  = 15 * time.Second

	// Initial sync outcome label values.
	syncFromReplica  = "from-replica"
	syncFromStorage  = "from-storage"
	syncUserNotFound = "user-not-found"
	syncFailed       = "failed"
)

// state represents the Alertmanager silences and notification log internal state.
type state struct {
	services.Service

	userID string
	logger log.Logger
	reg    prometheus.Registerer

	settleReadTimeout time.Duration
	storeReadTimeout  time.Duration

	mtx    sync.Mutex
	states map[string]cluster.State

	replicationFactor int
	replicator        Replicator
	store             alertstore.AlertStore

	partialStateMergesTotal  *prometheus.CounterVec
	partialStateMergesFailed *prometheus.CounterVec
	stateReplicationTotal    *prometheus.CounterVec
	stateReplicationFailed   *prometheus.CounterVec
	fetchReplicaStateTotal   prometheus.Counter
	fetchReplicaStateFailed  prometheus.Counter
	initialSyncTotal         prometheus.Counter
	initialSyncCompleted     *prometheus.CounterVec
	initialSyncDuration      prometheus.Histogram

	msgc chan *clusterpb.Part
}

// newReplicatedStates creates a new state struct, which manages state to be replicated between alertmanagers.
func newReplicatedStates(userID string, rf int, re Replicator, st alertstore.AlertStore, l log.Logger, r prometheus.Registerer) *state {

	s := &state{
		logger:            l,
		userID:            userID,
		replicationFactor: rf,
		replicator:        re,
		store:             st,
		states:            make(map[string]cluster.State, 2), // we use two, one for the notifications and one for silences.
		msgc:              make(chan *clusterpb.Part),
		reg:               r,
		settleReadTimeout: defaultSettleReadTimeout,
		storeReadTimeout:  defaultStoreReadTimeout,
		partialStateMergesTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "alertmanager_partial_state_merges_total",
			Help: "Number of times we have received a partial state to merge for a key.",
		}, []string{"key"}),
		partialStateMergesFailed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "alertmanager_partial_state_merges_failed_total",
			Help: "Number of times we have failed to merge a partial state received for a key.",
		}, []string{"key"}),
		stateReplicationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "alertmanager_state_replication_total",
			Help: "Number of times we have tried to replicate a state to other alertmanagers.",
		}, []string{"key"}),
		stateReplicationFailed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "alertmanager_state_replication_failed_total",
			Help: "Number of times we have failed to replicate a state to other alertmanagers.",
		}, []string{"key"}),
		fetchReplicaStateTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "alertmanager_state_fetch_replica_state_total",
			Help: "Number of times we have tried to read and merge the full state from another replica.",
		}),
		fetchReplicaStateFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "alertmanager_state_fetch_replica_state_failed_total",
			Help: "Number of times we have failed to read and merge the full state from another replica.",
		}),
		initialSyncTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "alertmanager_state_initial_sync_total",
			Help: "Number of times we have tried to sync initial state from peers or remote storage.",
		}),
		initialSyncCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "alertmanager_state_initial_sync_completed_total",
			Help: "Number of times we have completed syncing initial state for each possible outcome.",
		}, []string{"outcome"}),
		initialSyncDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "alertmanager_state_initial_sync_duration_seconds",
			Help:    "Time spent syncing initial state from peers or remote storage.",
			Buckets: prometheus.ExponentialBuckets(0.008, 4, 7),
		}),
	}
	s.initialSyncCompleted.WithLabelValues(syncFromReplica)
	s.initialSyncCompleted.WithLabelValues(syncFromStorage)
	s.initialSyncCompleted.WithLabelValues(syncUserNotFound)
	s.initialSyncCompleted.WithLabelValues(syncFailed)

	s.Service = services.NewBasicService(s.starting, s.running, nil)

	return s
}

// AddState adds a new state that will be replicated using the ReplicationFunc. It returns a channel to which the client can broadcast messages of the state to be sent.
func (s *state) AddState(key string, cs cluster.State, _ prometheus.Registerer) cluster.ClusterChannel {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.states[key] = cs

	s.partialStateMergesTotal.WithLabelValues(key)
	s.partialStateMergesFailed.WithLabelValues(key)
	s.stateReplicationTotal.WithLabelValues(key)
	s.stateReplicationFailed.WithLabelValues(key)

	return &stateChannel{
		s:   s,
		key: key,
	}
}

// MergePartialState merges a received partial message with an internal state.
func (s *state) MergePartialState(p *clusterpb.Part) error {
	s.partialStateMergesTotal.WithLabelValues(p.Key).Inc()

	s.mtx.Lock()
	defer s.mtx.Unlock()
	st, ok := s.states[p.Key]
	if !ok {
		s.partialStateMergesFailed.WithLabelValues(p.Key).Inc()
		return fmt.Errorf("key not found while merging")
	}

	if err := st.Merge(p.Data); err != nil {
		s.partialStateMergesFailed.WithLabelValues(p.Key).Inc()
		return err
	}

	return nil
}

// Position helps in determining how long should we wait before sending a notification based on the number of replicas.
func (s *state) Position() int {
	return s.replicator.GetPositionForUser(s.userID)
}

// GetFullState returns the full internal state.
func (s *state) GetFullState() (*clusterpb.FullState, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	all := &clusterpb.FullState{
		Parts: make([]clusterpb.Part, 0, len(s.states)),
	}

	for key, s := range s.states {
		b, err := s.MarshalBinary()
		if err != nil {
			return nil, errors.Wrapf(err, "failed to encode state for key: %v", key)
		}
		all.Parts = append(all.Parts, clusterpb.Part{Key: key, Data: b})
	}

	return all, nil
}

// starting waits until the alertmanagers are ready (and sets the appropriate internal state when it is).
// The idea is that we don't want to start working" before we get a chance to know most of the notifications and/or silences.
func (s *state) starting(ctx context.Context) error {
	s.initialSyncTotal.Inc()
	timer := prometheus.NewTimer(s.initialSyncDuration)
	defer timer.ObserveDuration()

	level.Info(s.logger).Log("msg", "Waiting for notification and silences to settle...")

	// If the replication factor is <= 1, there is nowhere to obtain the state from.
	if s.replicationFactor <= 1 {
		level.Info(s.logger).Log("msg", "skipping settling (no replicas)")
		return nil
	}

	// We can check other alertmanager(s) and explicitly ask them to propagate their state to us if available.
	readCtx, cancel := context.WithTimeout(ctx, s.settleReadTimeout)
	defer cancel()

	s.fetchReplicaStateTotal.Inc()
	fullStates, err := s.replicator.ReadFullStateForUser(readCtx, s.userID)
	if err == nil {
		if err = s.mergeFullStates(fullStates); err == nil {
			level.Info(s.logger).Log("msg", "state settled; proceeding")
			s.initialSyncCompleted.WithLabelValues(syncFromReplica).Inc()
			return nil
		}
	}
	s.fetchReplicaStateFailed.Inc()

	level.Info(s.logger).Log("msg", "state not settled; trying to read from storage", "err", err)

	// Attempt to read the state from persistent storage instead.
	storeReadCtx, cancel := context.WithTimeout(ctx, s.storeReadTimeout)
	defer cancel()

	fullState, err := s.store.GetFullState(storeReadCtx, s.userID)
	if errors.Is(err, alertspb.ErrNotFound) {
		level.Info(s.logger).Log("msg", "no state for user in storage; proceeding", "user", s.userID)
		s.initialSyncCompleted.WithLabelValues(syncUserNotFound).Inc()
		return nil
	}
	if err == nil {
		if err = s.mergeFullStates([]*clusterpb.FullState{fullState.State}); err == nil {
			level.Info(s.logger).Log("msg", "state read from storage; proceeding")
			s.initialSyncCompleted.WithLabelValues(syncFromStorage).Inc()
			return nil
		}
	}

	level.Warn(s.logger).Log("msg", "failed to read state from storage; continuing anyway", "err", err)
	s.initialSyncCompleted.WithLabelValues(syncFailed).Inc()

	return nil
}

// WaitReady is needed for the pipeline builder to know whenever we've settled and the state is up to date.
func (s *state) WaitReady(ctx context.Context) error {
	return s.Service.AwaitRunning(ctx)
}

func (s *state) Ready() bool {
	return s.Service.State() == services.Running
}

// mergeFullStates attempts to merge all full states received from peers during settling.
func (s *state) mergeFullStates(fs []*clusterpb.FullState) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, f := range fs {
		for _, p := range f.Parts {
			level.Debug(s.logger).Log("msg", "merging full state", "user", s.userID, "key", p.Key, "bytes", len(p.Data))

			st, ok := s.states[p.Key]
			if !ok {
				level.Error(s.logger).Log("msg", "key not found while merging full state", "user", s.userID, "key", p.Key)
				continue
			}

			if err := st.Merge(p.Data); err != nil {
				return errors.Wrapf(err, "failed to merge part of full state for key: %v", p.Key)
			}
		}
	}

	return nil
}

func (s *state) running(ctx context.Context) error {
	for {
		select {
		case p := <-s.msgc:
			// If the replication factor is <= 1, we don't need to replicate any state anywhere else.
			if s.replicationFactor <= 1 {
				return nil
			}

			s.stateReplicationTotal.WithLabelValues(p.Key).Inc()
			if err := s.replicator.ReplicateStateForUser(ctx, s.userID, p); err != nil {
				s.stateReplicationFailed.WithLabelValues(p.Key).Inc()
				level.Error(s.logger).Log("msg", "failed to replicate state to other alertmanagers", "user", s.userID, "key", p.Key, "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *state) broadcast(key string, b []byte) {
	// We should ignore the Merges into the initial state during settling.
	if s.Ready() {
		s.msgc <- &clusterpb.Part{Key: key, Data: b}
	}
}

// stateChannel allows a state publisher to send messages that will be broadcasted to all other alertmanagers that a tenant
// belongs to.
type stateChannel struct {
	s   *state
	key string
}

// Broadcast receives a message to be replicated by the state.
func (c *stateChannel) Broadcast(b []byte) {
	c.s.broadcast(c.key, b)
}
