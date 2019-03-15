package ring

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

var (
	consulHeartbeats = promauto.NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_consul_heartbeats_total",
		Help: "The total number of heartbeats sent to consul.",
	})
	tokensOwned = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ingester_ring_tokens_owned",
		Help: "The number of tokens owned in the ring.",
	})
	tokensToOwn = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ingester_ring_tokens_to_own",
		Help: "The number of tokens to own in the ring.",
	})
)

// LifecyclerConfig is the config to build a Lifecycler.
type LifecyclerConfig struct {
	RingConfig Config `yaml:"ring,omitempty"`

	// Config for the ingester lifecycle control
	ListenPort       *int
	NumTokens        int           `yaml:"num_tokens,omitempty"`
	HeartbeatPeriod  time.Duration `yaml:"heartbeat_period,omitempty"`
	JoinAfter        time.Duration `yaml:"join_after,omitempty"`
	MinReadyDuration time.Duration `yaml:"min_ready_duration,omitempty"`
	ClaimOnRollout   bool          `yaml:"claim_on_rollout,omitempty"`
	NormaliseTokens  bool          `yaml:"normalise_tokens,omitempty"`
	InfNames         []string      `yaml:"interface_names"`

	// For testing, you can override the address and ID of this ingester
	Addr           string `yaml:"address"`
	Port           int
	ID             string
	SkipUnregister bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *LifecyclerConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RingConfig.RegisterFlags(f)

	f.IntVar(&cfg.NumTokens, "ingester.num-tokens", 128, "Number of tokens for each ingester.")
	f.DurationVar(&cfg.HeartbeatPeriod, "ingester.heartbeat-period", 5*time.Second, "Period at which to heartbeat to consul.")
	f.DurationVar(&cfg.JoinAfter, "ingester.join-after", 0*time.Second, "Period to wait for a claim from another ingester; will join automatically after this.")
	f.DurationVar(&cfg.MinReadyDuration, "ingester.min-ready-duration", 1*time.Minute, "Minimum duration to wait before becoming ready. This is to work around race conditions with ingesters exiting and updating the ring.")
	f.BoolVar(&cfg.ClaimOnRollout, "ingester.claim-on-rollout", false, "Send chunks to PENDING ingesters on exit.")
	f.BoolVar(&cfg.NormaliseTokens, "ingester.normalise-tokens", false, "Store tokens in a normalised fashion to reduce allocations.")

	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	cfg.InfNames = []string{"eth0", "en0"}
	f.Var((*flagext.Strings)(&cfg.InfNames), "ingester.interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.Addr, "ingester.addr", "", "IP address to advertise in consul.")
	f.IntVar(&cfg.Port, "ingester.port", 0, "port to advertise in consul (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.ID, "ingester.ID", hostname, "ID to register into consul.")
}

// FlushTransferer controls the shutdown of an ingester.
type FlushTransferer interface {
	StopIncomingRequests()
	Flush()
	TransferOut(ctx context.Context) error
}

// Lifecycler is responsible for managing the lifecycle of entries in the ring.
type Lifecycler struct {
	cfg             LifecyclerConfig
	flushTransferer FlushTransferer
	KVStore         KVClient

	// Controls the lifecycle of the ingester
	quit      chan struct{}
	done      sync.WaitGroup
	actorChan chan func()

	// These values are initialised at startup, and never change
	ID   string
	addr string

	// We need to remember the ingester state just in case consul goes away and comes
	// back empty.  And it changes during lifecycle of ingester.
	stateMtx sync.Mutex
	state    IngesterState
	tokens   []uint32

	// Controls the ready-reporting
	readyLock sync.Mutex
	startTime time.Time
	ready     bool
}

// NewLifecycler makes and starts a new Lifecycler.
func NewLifecycler(cfg LifecyclerConfig, flushTransferer FlushTransferer) (*Lifecycler, error) {
	addr := cfg.Addr
	if addr == "" {
		var err error
		addr, err = util.GetFirstAddressOf(cfg.InfNames)
		if err != nil {
			return nil, err
		}
	}
	port := cfg.Port
	if port == 0 {
		port = *cfg.ListenPort
	}

	store, err := newKVStore(cfg.RingConfig)
	if err != nil {
		return nil, err
	}

	l := &Lifecycler{
		cfg:             cfg,
		flushTransferer: flushTransferer,
		KVStore:         store,

		addr: fmt.Sprintf("%s:%d", addr, port),
		ID:   cfg.ID,

		quit:      make(chan struct{}),
		actorChan: make(chan func()),

		state:     PENDING,
		startTime: time.Now(),
	}

	tokensToOwn.Set(float64(cfg.NumTokens))

	l.done.Add(1)
	go l.loop()
	return l, nil
}

// CheckReady is used to rate limit the number of ingesters that can be coming or
// going at any one time, by only returning true if all ingesters are active.
// The state latches: once we have gone ready we don't go un-ready
func (i *Lifecycler) CheckReady(ctx context.Context) error {
	i.readyLock.Lock()
	defer i.readyLock.Unlock()

	if i.ready {
		return nil
	}

	// Ingester always take at least minReadyDuration to become ready to work
	// around race conditions with ingesters exiting and updating the ring
	if time.Now().Sub(i.startTime) < i.cfg.MinReadyDuration {
		return fmt.Errorf("waiting for %v after startup", i.cfg.MinReadyDuration)
	}

	ringDesc, err := i.KVStore.Get(ctx, ConsulKey)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error talking to consul", "err", err)
		return fmt.Errorf("error talking to consul: %s", err)
	}

	if len(i.getTokens()) == 0 {
		return fmt.Errorf("this ingester owns no tokens")
	}
	if err := ringDesc.(*Desc).Ready(i.cfg.RingConfig.HeartbeatTimeout); err != nil {
		return err
	}

	i.ready = true
	return nil
}

// GetState returns the state of this ingester.
func (i *Lifecycler) GetState() IngesterState {
	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	return i.state
}

func (i *Lifecycler) setState(state IngesterState) {
	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	i.state = state
}

// ChangeState of the ingester, for use off of the loop() goroutine.
func (i *Lifecycler) ChangeState(ctx context.Context, state IngesterState) error {
	err := make(chan error)
	i.actorChan <- func() {
		err <- i.changeState(ctx, state)
	}
	return <-err
}

func (i *Lifecycler) getTokens() []uint32 {
	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	return i.tokens
}

func (i *Lifecycler) setTokens(tokens []uint32) {
	tokensOwned.Set(float64(len(tokens)))

	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	i.tokens = tokens
}

// ClaimTokensFor takes all the tokens for the supplied ingester and assigns them to this ingester.
func (i *Lifecycler) ClaimTokensFor(ctx context.Context, ingesterID string) error {
	err := make(chan error)

	i.actorChan <- func() {
		var tokens []uint32

		claimTokens := func(in interface{}) (out interface{}, retry bool, err error) {
			ringDesc, ok := in.(*Desc)
			if !ok || ringDesc == nil {
				return nil, false, fmt.Errorf("Cannot claim tokens in an empty ring")
			}

			tokens = ringDesc.ClaimTokens(ingesterID, i.ID, i.cfg.NormaliseTokens)
			return ringDesc, true, nil
		}

		if err := i.KVStore.CAS(ctx, ConsulKey, claimTokens); err != nil {
			level.Error(util.Logger).Log("msg", "Failed to write to consul", "err", err)
		}

		i.setTokens(tokens)
		err <- nil
	}

	return <-err
}

// Shutdown the lifecycle.  It will:
// - send chunks to another ingester, if it can.
// - otherwise, flush chunks to the chunk store.
// - remove config from Consul.
// - block until we've successfully shutdown.
func (i *Lifecycler) Shutdown() {
	// This will prevent us accepting any more samples
	i.flushTransferer.StopIncomingRequests()

	// closing i.quit triggers loop() to exit, which in turn will trigger
	// the removal of our tokens etc
	close(i.quit)
	i.done.Wait()
}

func (i *Lifecycler) loop() {
	defer func() {
		level.Info(util.Logger).Log("msg", "Ingester.loop() exited gracefully")
		i.done.Done()
	}()

	// First, see if we exist in the cluster, update our state to match if we do,
	// and add ourselves (without tokens) if we don't.
	if err := i.initRing(context.Background()); err != nil {
		level.Error(util.Logger).Log("msg", "failed to join consul", "err", err)
		os.Exit(1)
	}

	// We do various period tasks
	autoJoinAfter := time.After(i.cfg.JoinAfter)

	heartbeatTicker := time.NewTicker(i.cfg.HeartbeatPeriod)
	defer heartbeatTicker.Stop()

loop:
	for {
		select {
		case <-autoJoinAfter:
			level.Debug(util.Logger).Log("msg", "JoinAfter expired")
			// Will only fire once, after auto join timeout.  If we haven't entered "JOINING" state,
			// then pick some tokens and enter ACTIVE state.
			if i.GetState() == PENDING {
				level.Info(util.Logger).Log("msg", "auto-joining cluster after timeout")
				if err := i.autoJoin(context.Background()); err != nil {
					level.Error(util.Logger).Log("msg", "failed to pick tokens in consul", "err", err)
					os.Exit(1)
				}
			}

		case <-heartbeatTicker.C:
			consulHeartbeats.Inc()
			if err := i.updateConsul(context.Background()); err != nil {
				level.Error(util.Logger).Log("msg", "failed to write to consul, sleeping", "err", err)
			}

		case f := <-i.actorChan:
			f()

		case <-i.quit:
			break loop
		}
	}

	// Mark ourselved as Leaving so no more samples are send to us.
	i.changeState(context.Background(), LEAVING)

	// Do the transferring / flushing on a background goroutine so we can continue
	// to heartbeat to consul.
	done := make(chan struct{})
	go func() {
		i.processShutdown(context.Background())
		close(done)
	}()

heartbeatLoop:
	for {
		select {
		case <-heartbeatTicker.C:
			consulHeartbeats.Inc()
			if err := i.updateConsul(context.Background()); err != nil {
				level.Error(util.Logger).Log("msg", "failed to write to consul, sleeping", "err", err)
			}

		case <-done:
			break heartbeatLoop
		}
	}

	if !i.cfg.SkipUnregister {
		if err := i.unregister(context.Background()); err != nil {
			level.Error(util.Logger).Log("msg", "Failed to unregister from consul", "err", err)
			os.Exit(1)
		}
		level.Info(util.Logger).Log("msg", "ingester removed from consul")
	}
}

// initRing is the first thing we do when we start. It:
// - add an ingester entry to the ring
// - copies out our state and tokens if they exist
func (i *Lifecycler) initRing(ctx context.Context) error {
	return i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		var ringDesc *Desc
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		ingesterDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// Either we are a new ingester, or consul must have restarted
			level.Info(util.Logger).Log("msg", "entry not found in ring, adding with no tokens")
			ringDesc.AddIngester(i.ID, i.addr, []uint32{}, i.GetState(), i.cfg.NormaliseTokens)
			return ringDesc, true, nil
		}

		// We exist in the ring, so assume the ring is right and copy out tokens & state out of there.
		i.setState(ingesterDesc.State)
		tokens, _ := ringDesc.TokensFor(i.ID)
		i.setTokens(tokens)

		level.Info(util.Logger).Log("msg", "existing entry found in ring", "state", i.GetState(), "tokens", len(tokens))
		return ringDesc, true, nil
	})
}

// autoJoin selects random tokens & moves state to ACTIVE
func (i *Lifecycler) autoJoin(ctx context.Context) error {
	return i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		var ringDesc *Desc
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		// At this point, we should not have any tokens, and we should be in PENDING state.
		myTokens, takenTokens := ringDesc.TokensFor(i.ID)
		if len(myTokens) > 0 {
			level.Error(util.Logger).Log("msg", "tokens already exist for this ingester - wasn't expecting any!", "num_tokens", len(myTokens))
		}

		newTokens := GenerateTokens(i.cfg.NumTokens-len(myTokens), takenTokens)
		i.setState(ACTIVE)
		ringDesc.AddIngester(i.ID, i.addr, newTokens, i.GetState(), i.cfg.NormaliseTokens)

		tokens := append(myTokens, newTokens...)
		sort.Sort(sortableUint32(tokens))
		i.setTokens(tokens)

		return ringDesc, true, nil
	})
}

// updateConsul updates our entries in consul, heartbeating and dealing with
// consul restarts.
func (i *Lifecycler) updateConsul(ctx context.Context) error {
	return i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		var ringDesc *Desc
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		ingesterDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// consul must have restarted
			level.Info(util.Logger).Log("msg", "found empty ring, inserting tokens")
			ringDesc.AddIngester(i.ID, i.addr, i.getTokens(), i.GetState(), i.cfg.NormaliseTokens)
		} else {
			ingesterDesc.Timestamp = time.Now().Unix()
			ingesterDesc.State = i.GetState()
			ingesterDesc.Addr = i.addr
			ringDesc.Ingesters[i.ID] = ingesterDesc
		}

		return ringDesc, true, nil
	})
}

// changeState updates consul with state transitions for us.  NB this must be
// called from loop()!  Use ChangeState for calls from outside of loop().
func (i *Lifecycler) changeState(ctx context.Context, state IngesterState) error {
	currState := i.GetState()
	// Only the following state transitions can be triggered externally
	if !((currState == PENDING && state == JOINING) || // triggered by TransferChunks at the beginning
		(currState == JOINING && state == PENDING) || // triggered by TransferChunks on failure
		(currState == JOINING && state == ACTIVE) || // triggered by TransferChunks on success
		(currState == PENDING && state == ACTIVE) || // triggered by autoJoin
		(currState == ACTIVE && state == LEAVING)) { // triggered by shutdown
		return fmt.Errorf("Changing ingester state from %v -> %v is disallowed", currState, state)
	}

	level.Info(util.Logger).Log("msg", "changing ingester state from", "old_state", currState, "new_state", state)
	i.setState(state)
	return i.updateConsul(ctx)
}

func (i *Lifecycler) processShutdown(ctx context.Context) {
	flushRequired := true
	if i.cfg.ClaimOnRollout {
		if err := i.flushTransferer.TransferOut(ctx); err != nil {
			level.Error(util.Logger).Log("msg", "Failed to transfer chunks to another ingester", "err", err)
		} else {
			flushRequired = false
		}
	}

	if flushRequired {
		i.flushTransferer.Flush()
	}
}

// unregister removes our entry from consul.
func (i *Lifecycler) unregister(ctx context.Context) error {
	return i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			return nil, false, fmt.Errorf("found empty ring when trying to unregister")
		}

		ringDesc := in.(*Desc)
		ringDesc.RemoveIngester(i.ID)
		return ringDesc, true, nil
	})
}
