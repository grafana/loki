package alertmanager

import (
	"context"
	"flag"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster/clusterpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alertstore"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	defaultPersistTimeout = 30 * time.Second
)

var (
	errInvalidPersistInterval = errors.New("invalid alertmanager persist interval, must be greater than zero")
)

type PersisterConfig struct {
	Interval time.Duration `yaml:"persist_interval"`
}

func (cfg *PersisterConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Interval, prefix+".persist-interval", 15*time.Minute, "The interval between persisting the current alertmanager state (notification log and silences) to object storage. This is only used when sharding is enabled. This state is read when all replicas for a shard can not be contacted. In this scenario, having persisted the state more frequently will result in potentially fewer lost silences, and fewer duplicate notifications.")
}

func (cfg *PersisterConfig) Validate() error {
	if cfg.Interval <= 0 {
		return errInvalidPersistInterval
	}
	return nil
}

type PersistableState interface {
	State
	GetFullState() (*clusterpb.FullState, error)
}

// statePersister periodically writes the alertmanager state to persistent storage.
type statePersister struct {
	services.Service

	state  PersistableState
	store  alertstore.AlertStore
	userID string
	logger log.Logger

	timeout time.Duration

	persistTotal  prometheus.Counter
	persistFailed prometheus.Counter
}

// newStatePersister creates a new state persister.
func newStatePersister(cfg PersisterConfig, userID string, state PersistableState, store alertstore.AlertStore, l log.Logger, r prometheus.Registerer) *statePersister {

	s := &statePersister{
		state:   state,
		store:   store,
		userID:  userID,
		logger:  l,
		timeout: defaultPersistTimeout,
		persistTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "alertmanager_state_persist_total",
			Help: "Number of times we have tried to persist the running state to remote storage.",
		}),
		persistFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "alertmanager_state_persist_failed_total",
			Help: "Number of times we have failed to persist the running state to remote storage.",
		}),
	}

	s.Service = services.NewTimerService(cfg.Interval, s.starting, s.iteration, nil)

	return s
}

func (s *statePersister) starting(ctx context.Context) error {
	// Waits until the state replicator is settled, so that state is not
	// persisted before obtaining some initial state.
	return s.state.WaitReady(ctx)
}

func (s *statePersister) iteration(ctx context.Context) error {
	if err := s.persist(ctx); err != nil {
		level.Error(s.logger).Log("msg", "failed to persist state", "user", s.userID, "err", err)
	}
	return nil
}

func (s *statePersister) persist(ctx context.Context) (err error) {
	// Only the replica at position zero should write the state.
	if s.state.Position() != 0 {
		return nil
	}

	s.persistTotal.Inc()
	defer func() {
		if err != nil {
			s.persistFailed.Inc()
		}
	}()

	level.Debug(s.logger).Log("msg", "persisting state", "user", s.userID)

	var fs *clusterpb.FullState
	fs, err = s.state.GetFullState()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	desc := alertspb.FullStateDesc{State: fs}
	if err = s.store.SetFullState(ctx, s.userID, desc); err != nil {
		return err
	}

	return nil
}
