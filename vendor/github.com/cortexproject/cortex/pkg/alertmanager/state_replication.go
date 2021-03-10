package alertmanager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/cluster/clusterpb"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util/services"
)

// state represents the Alertmanager silences and notification log internal state.
type state struct {
	services.Service

	userID string
	logger log.Logger
	reg    prometheus.Registerer

	mtx    sync.Mutex
	states map[string]cluster.State

	replicationFactor  int
	replicateStateFunc func(context.Context, string, *clusterpb.Part) error
	positionFunc       func(string) int

	partialStateMergesTotal  *prometheus.CounterVec
	partialStateMergesFailed *prometheus.CounterVec
	stateReplicationTotal    *prometheus.CounterVec
	stateReplicationFailed   *prometheus.CounterVec

	msgc   chan *clusterpb.Part
	readyc chan struct{}
}

// newReplicatedStates creates a new state struct, which manages state to be replicated between alertmanagers.
func newReplicatedStates(userID string, rf int, f func(context.Context, string, *clusterpb.Part) error, pf func(string) int, l log.Logger, r prometheus.Registerer) *state {

	s := &state{
		logger:             l,
		userID:             userID,
		replicateStateFunc: f,
		replicationFactor:  rf,
		positionFunc:       pf,
		states:             make(map[string]cluster.State, 2), // we use two, one for the notifications and one for silences.
		msgc:               make(chan *clusterpb.Part),
		readyc:             make(chan struct{}),
		reg:                r,
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
	}

	s.Service = services.NewBasicService(nil, s.running, nil)

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
		msgc: s.msgc,
		key:  key,
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
func (s *state) Position() int { return s.positionFunc(s.userID) }

// Settle waits until the alertmanagers are ready (and sets the appropriate internal state when it is).
// The idea is that we don't want to start working" before we get a chance to know most of the notifications and/or silences.
func (s *state) Settle(ctx context.Context, _ time.Duration) {
	level.Info(s.logger).Log("msg", "Waiting for notification and silences to settle...")

	// TODO: Make sure that the state is fully synchronised at this point.
	// We can check other alertmanager(s) and explicitly ask them to propagate their state to us if available.
	close(s.readyc)
}

// WaitReady is needed for the pipeline builder to know whenever we've settled and the state is up to date.
func (s *state) WaitReady() {
	//TODO: At the moment, we settle in a separate go-routine (see multitenant.go as we create the Peer) we should
	// mimic that behaviour here once we have full state replication.
	s.Settle(context.Background(), time.Second)
	<-s.readyc
}

func (s *state) Ready() bool {
	select {
	case <-s.readyc:
		return true
	default:
	}
	return false
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
			if err := s.replicateStateFunc(ctx, s.userID, p); err != nil {
				s.stateReplicationFailed.WithLabelValues(p.Key).Inc()
				level.Error(s.logger).Log("msg", "failed to replicate state to other alertmanagers", "user", s.userID, "key", p.Key, "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// stateChannel allows a state publisher to send messages that will be broadcasted to all other alertmanagers that a tenant
// belongs to.
type stateChannel struct {
	msgc chan *clusterpb.Part
	key  string
}

// Broadcast receives a message to be replicated by the state.
func (c *stateChannel) Broadcast(b []byte) {
	c.msgc <- &clusterpb.Part{Key: c.key, Data: b}
}
