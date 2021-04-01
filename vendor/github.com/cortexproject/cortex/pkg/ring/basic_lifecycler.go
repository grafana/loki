package ring

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ring/kv"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

type BasicLifecyclerDelegate interface {
	// OnRingInstanceRegister is called while the lifecycler is registering the
	// instance within the ring and should return the state and set of tokens to
	// use for the instance itself.
	OnRingInstanceRegister(lifecycler *BasicLifecycler, ringDesc Desc, instanceExists bool, instanceID string, instanceDesc InstanceDesc) (InstanceState, Tokens)

	// OnRingInstanceTokens is called once the instance tokens are set and are
	// stable within the ring (honoring the observe period, if set).
	OnRingInstanceTokens(lifecycler *BasicLifecycler, tokens Tokens)

	// OnRingInstanceStopping is called while the lifecycler is stopping. The lifecycler
	// will continue to hearbeat the ring the this function is executing and will proceed
	// to unregister the instance from the ring only after this function has returned.
	OnRingInstanceStopping(lifecycler *BasicLifecycler)

	// OnRingInstanceHeartbeat is called while the instance is updating its heartbeat
	// in the ring.
	OnRingInstanceHeartbeat(lifecycler *BasicLifecycler, ringDesc *Desc, instanceDesc *InstanceDesc)
}

type BasicLifecyclerConfig struct {
	// ID is the instance unique ID.
	ID string

	// Addr is the instance address, in the form "address:port".
	Addr string

	// Zone is the instance availability zone. Can be an empty string
	// if zone awareness is unused.
	Zone string

	HeartbeatPeriod     time.Duration
	TokensObservePeriod time.Duration
	NumTokens           int
}

// BasicLifecycler is a basic ring lifecycler which allows to hook custom
// logic at different stages of the lifecycle. This lifecycler should be
// used to build higher level lifecyclers.
//
// This lifecycler never change the instance state. It's the delegate
// responsibility to ChangeState().
type BasicLifecycler struct {
	*services.BasicService

	cfg      BasicLifecyclerConfig
	logger   log.Logger
	store    kv.Client
	delegate BasicLifecyclerDelegate
	metrics  *BasicLifecyclerMetrics

	// Channel used to execute logic within the lifecycler loop.
	actorChan chan func()

	// These values are initialised at startup, and never change
	ringName string
	ringKey  string

	// The current instance state.
	currState        sync.RWMutex
	currInstanceDesc *InstanceDesc
}

// NewBasicLifecycler makes a new BasicLifecycler.
func NewBasicLifecycler(cfg BasicLifecyclerConfig, ringName, ringKey string, store kv.Client, delegate BasicLifecyclerDelegate, logger log.Logger, reg prometheus.Registerer) (*BasicLifecycler, error) {
	l := &BasicLifecycler{
		cfg:       cfg,
		ringName:  ringName,
		ringKey:   ringKey,
		logger:    logger,
		store:     store,
		delegate:  delegate,
		metrics:   NewBasicLifecyclerMetrics(ringName, reg),
		actorChan: make(chan func()),
	}

	l.metrics.tokensToOwn.Set(float64(cfg.NumTokens))
	l.BasicService = services.NewBasicService(l.starting, l.running, l.stopping)

	return l, nil
}

func (l *BasicLifecycler) GetInstanceID() string {
	return l.cfg.ID
}

func (l *BasicLifecycler) GetInstanceAddr() string {
	return l.cfg.Addr
}

func (l *BasicLifecycler) GetInstanceZone() string {
	return l.cfg.Zone
}

func (l *BasicLifecycler) GetState() InstanceState {
	l.currState.RLock()
	defer l.currState.RUnlock()

	if l.currInstanceDesc == nil {
		return PENDING
	}

	return l.currInstanceDesc.GetState()
}

func (l *BasicLifecycler) GetTokens() Tokens {
	l.currState.RLock()
	defer l.currState.RUnlock()

	if l.currInstanceDesc == nil {
		return Tokens{}
	}

	return l.currInstanceDesc.GetTokens()
}

// GetRegisteredAt returns the timestamp when the instance has been registered to the ring
// or a zero value if the lifecycler hasn't been started yet or was already registered and its
// timestamp is unknown.
func (l *BasicLifecycler) GetRegisteredAt() time.Time {
	l.currState.RLock()
	defer l.currState.RUnlock()

	return l.currInstanceDesc.GetRegisteredAt()
}

// IsRegistered returns whether the instance is currently registered within the ring.
func (l *BasicLifecycler) IsRegistered() bool {
	l.currState.RLock()
	defer l.currState.RUnlock()

	return l.currInstanceDesc != nil
}

func (l *BasicLifecycler) ChangeState(ctx context.Context, state InstanceState) error {
	return l.run(func() error {
		return l.changeState(ctx, state)
	})
}

func (l *BasicLifecycler) starting(ctx context.Context) error {
	if err := l.registerInstance(ctx); err != nil {
		return errors.Wrap(err, "register instance in the ring")
	}

	// If we have registered an instance with some tokens and
	// an observe period has been configured, we should now wait
	// until tokens are "stable" within the ring.
	if len(l.GetTokens()) > 0 && l.cfg.TokensObservePeriod > 0 {
		if err := l.waitStableTokens(ctx, l.cfg.TokensObservePeriod); err != nil {
			return errors.Wrap(err, "wait stable tokens in the ring")
		}
	}

	// At this point, if some tokens have been set they're stable and we
	// can notify the delegate.
	if tokens := l.GetTokens(); len(tokens) > 0 {
		l.metrics.tokensOwned.Set(float64(len(tokens)))
		l.delegate.OnRingInstanceTokens(l, tokens)
	}

	return nil
}

func (l *BasicLifecycler) running(ctx context.Context) error {
	heartbeatTicker := time.NewTicker(l.cfg.HeartbeatPeriod)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-heartbeatTicker.C:
			l.heartbeat(ctx)

		case f := <-l.actorChan:
			f()

		case <-ctx.Done():
			level.Info(util_log.Logger).Log("msg", "ring lifecycler is shutting down", "ring", l.ringName)
			return nil
		}
	}
}

func (l *BasicLifecycler) stopping(runningError error) error {
	if runningError != nil {
		return nil
	}

	// Let the delegate change the instance state (ie. to LEAVING) and handling any
	// state transferring / flushing while we continue to heartbeat.
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.delegate.OnRingInstanceStopping(l)
	}()

	// Heartbeat while the stopping delegate function is running.
	heartbeatTicker := time.NewTicker(l.cfg.HeartbeatPeriod)
	defer heartbeatTicker.Stop()

heartbeatLoop:
	for {
		select {
		case <-heartbeatTicker.C:
			l.heartbeat(context.Background())
		case <-done:
			break heartbeatLoop
		}
	}

	// Remove the instance from the ring.
	if err := l.unregisterInstance(context.Background()); err != nil {
		return errors.Wrapf(err, "failed to unregister instance from the ring (ring: %s)", l.ringName)
	}
	level.Info(l.logger).Log("msg", "instance removed from the ring", "ring", l.ringName)

	return nil
}

// registerInstance registers the instance in the ring. The initial state and set of tokens
// depends on the OnRingInstanceRegister() delegate function.
func (l *BasicLifecycler) registerInstance(ctx context.Context) error {
	var instanceDesc InstanceDesc

	err := l.store.CAS(ctx, l.ringKey, func(in interface{}) (out interface{}, retry bool, err error) {
		ringDesc := GetOrCreateRingDesc(in)

		var exists bool
		instanceDesc, exists = ringDesc.Ingesters[l.cfg.ID]
		if exists {
			level.Info(l.logger).Log("msg", "instance found in the ring", "instance", l.cfg.ID, "ring", l.ringName, "state", instanceDesc.GetState(), "tokens", len(instanceDesc.GetTokens()), "registered_at", instanceDesc.GetRegisteredAt().String())
		} else {
			level.Info(l.logger).Log("msg", "instance not found in the ring", "instance", l.cfg.ID, "ring", l.ringName)
		}

		// We call the delegate to get the desired state right after the initialization.
		state, tokens := l.delegate.OnRingInstanceRegister(l, *ringDesc, exists, l.cfg.ID, instanceDesc)

		// Ensure tokens are sorted.
		sort.Sort(tokens)

		// If the instance didn't already exist, then we can safely set the registered timestamp to "now",
		// otherwise we have to honor the previous value (even if it was zero, because means it was unknown
		// but it's definitely not "now").
		var registeredAt time.Time
		if exists {
			registeredAt = instanceDesc.GetRegisteredAt()
		} else {
			registeredAt = time.Now()
		}

		if !exists {
			instanceDesc = ringDesc.AddIngester(l.cfg.ID, l.cfg.Addr, l.cfg.Zone, tokens, state, registeredAt)
			return ringDesc, true, nil
		}

		// Always overwrite the instance in the ring (even if already exists) because some properties
		// may have changed (stated, tokens, zone, address) and even if they didn't the heartbeat at
		// least did.
		instanceDesc = ringDesc.AddIngester(l.cfg.ID, l.cfg.Addr, l.cfg.Zone, tokens, state, registeredAt)
		return ringDesc, true, nil
	})

	if err != nil {
		return err
	}

	l.currState.Lock()
	l.currInstanceDesc = &instanceDesc
	l.currState.Unlock()

	return nil
}

func (l *BasicLifecycler) waitStableTokens(ctx context.Context, period time.Duration) error {
	heartbeatTicker := time.NewTicker(l.cfg.HeartbeatPeriod)
	defer heartbeatTicker.Stop()

	// The first observation will occur after the specified period.
	level.Info(l.logger).Log("msg", "waiting stable tokens", "ring", l.ringName)
	observeChan := time.After(period)

	for {
		select {
		case <-observeChan:
			if !l.verifyTokens(ctx) {
				// The verification has failed
				level.Info(l.logger).Log("msg", "tokens verification failed, keep observing", "ring", l.ringName)
				observeChan = time.After(period)
				break
			}

			level.Info(l.logger).Log("msg", "tokens verification succeeded", "ring", l.ringName)
			return nil

		case <-heartbeatTicker.C:
			l.heartbeat(ctx)

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Verifies that tokens that this instance has registered to the ring still belong to it.
// Gossiping ring may change the ownership of tokens in case of conflicts.
// If instance doesn't own its tokens anymore, this method generates new tokens and stores them to the ring.
func (l *BasicLifecycler) verifyTokens(ctx context.Context) bool {
	result := false

	err := l.updateInstance(ctx, func(r *Desc, i *InstanceDesc) bool {
		// At this point, we should have the same tokens as we have registered before.
		actualTokens, takenTokens := r.TokensFor(l.cfg.ID)

		if actualTokens.Equals(l.GetTokens()) {
			// Tokens have been verified. No need to change them.
			result = true
			return false
		}

		// uh, oh... our tokens are not our anymore. Let's try new ones.
		needTokens := l.cfg.NumTokens - len(actualTokens)

		level.Info(l.logger).Log("msg", "generating new tokens", "count", needTokens, "ring", l.ringName)
		newTokens := GenerateTokens(needTokens, takenTokens)

		actualTokens = append(actualTokens, newTokens...)
		sort.Sort(actualTokens)

		i.Tokens = actualTokens
		return true
	})

	if err != nil {
		level.Error(l.logger).Log("msg", "failed to verify tokens", "ring", l.ringName, "err", err)
		return false
	}

	return result
}

// unregister removes our entry from the store.
func (l *BasicLifecycler) unregisterInstance(ctx context.Context) error {
	level.Info(l.logger).Log("msg", "unregistering instance from ring", "ring", l.ringName)

	err := l.store.CAS(ctx, l.ringKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			return nil, false, fmt.Errorf("found empty ring when trying to unregister")
		}

		ringDesc := in.(*Desc)
		ringDesc.RemoveIngester(l.cfg.ID)
		return ringDesc, true, nil
	})

	if err != nil {
		return err
	}

	l.currState.Lock()
	l.currInstanceDesc = nil
	l.currState.Unlock()

	l.metrics.tokensToOwn.Set(0)
	l.metrics.tokensOwned.Set(0)
	return nil
}

func (l *BasicLifecycler) updateInstance(ctx context.Context, update func(*Desc, *InstanceDesc) bool) error {
	var instanceDesc InstanceDesc

	err := l.store.CAS(ctx, l.ringKey, func(in interface{}) (out interface{}, retry bool, err error) {
		ringDesc := GetOrCreateRingDesc(in)

		var ok bool
		instanceDesc, ok = ringDesc.Ingesters[l.cfg.ID]

		// This could happen if the backend store restarted (and content deleted)
		// or the instance has been forgotten. In this case, we do re-insert it.
		if !ok {
			level.Warn(l.logger).Log("msg", "instance missing in the ring, adding it back", "ring", l.ringName)
			instanceDesc = ringDesc.AddIngester(l.cfg.ID, l.cfg.Addr, l.cfg.Zone, l.GetTokens(), l.GetState(), l.GetRegisteredAt())
		}

		prevTimestamp := instanceDesc.Timestamp
		changed := update(ringDesc, &instanceDesc)
		if ok && !changed {
			return nil, false, nil
		}

		// Memberlist requires that the timestamp always change, so we do update it unless
		// was updated in the callback function.
		if instanceDesc.Timestamp == prevTimestamp {
			instanceDesc.Timestamp = time.Now().Unix()
		}

		ringDesc.Ingesters[l.cfg.ID] = instanceDesc
		return ringDesc, true, nil
	})

	if err != nil {
		return err
	}

	l.currState.Lock()
	l.currInstanceDesc = &instanceDesc
	l.currState.Unlock()

	return nil
}

// heartbeat updates the instance timestamp within the ring. This function is guaranteed
// to be called within the lifecycler main goroutine.
func (l *BasicLifecycler) heartbeat(ctx context.Context) {
	err := l.updateInstance(ctx, func(r *Desc, i *InstanceDesc) bool {
		l.delegate.OnRingInstanceHeartbeat(l, r, i)
		i.Timestamp = time.Now().Unix()
		return true
	})

	if err != nil {
		level.Warn(l.logger).Log("msg", "failed to heartbeat instance in the ring", "ring", l.ringName, "err", err)
		return
	}

	l.metrics.heartbeats.Inc()
}

// changeState of the instance within the ring. This function is guaranteed
// to be called within the lifecycler main goroutine.
func (l *BasicLifecycler) changeState(ctx context.Context, state InstanceState) error {
	err := l.updateInstance(ctx, func(_ *Desc, i *InstanceDesc) bool {
		// No-op if the state hasn't changed.
		if i.State == state {
			return false
		}

		i.State = state
		return true
	})

	if err != nil {
		level.Warn(l.logger).Log("msg", "failed to change instance state in the ring", "from", l.GetState(), "to", state, "err", err)
	}

	return err
}

// run a function within the lifecycler service goroutine.
func (l *BasicLifecycler) run(fn func() error) error {
	sc := l.ServiceContext()
	if sc == nil {
		return errors.New("lifecycler not running")
	}

	errCh := make(chan error)
	wrappedFn := func() {
		errCh <- fn()
	}

	select {
	case <-sc.Done():
		return errors.New("lifecycler not running")
	case l.actorChan <- wrappedFn:
		return <-errCh
	}
}
