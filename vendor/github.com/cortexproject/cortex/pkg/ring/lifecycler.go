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

	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

var (
	consulHeartbeats = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_member_consul_heartbeats_total",
		Help: "The total number of heartbeats sent to consul.",
	}, []string{"name"})
	tokensOwned = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cortex_member_ring_tokens_owned",
		Help: "The number of tokens owned in the ring.",
	}, []string{"name"})
	tokensToOwn = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cortex_member_ring_tokens_to_own",
		Help: "The number of tokens to own in the ring.",
	}, []string{"name"})
	shutdownDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "cortex_shutdown_duration_seconds",
		Help:    "Duration (in seconds) of cortex shutdown procedure (ie transfer or flush).",
		Buckets: prometheus.ExponentialBuckets(10, 2, 8), // Biggest bucket is 10*2^(9-1) = 2560, or 42 mins.
	}, []string{"op", "status", "name"})
)

// LifecyclerConfig is the config to build a Lifecycler.
type LifecyclerConfig struct {
	RingConfig Config `yaml:"ring,omitempty"`

	// Config for the ingester lifecycle control
	ListenPort       *int
	NumTokens        int           `yaml:"num_tokens,omitempty"`
	HeartbeatPeriod  time.Duration `yaml:"heartbeat_period,omitempty"`
	ObservePeriod    time.Duration `yaml:"observe_period,omitempty"`
	JoinAfter        time.Duration `yaml:"join_after,omitempty"`
	MinReadyDuration time.Duration `yaml:"min_ready_duration,omitempty"`
	UnusedFlag       bool          `yaml:"claim_on_rollout,omitempty"` // DEPRECATED - left for backwards-compatibility
	NormaliseTokens  bool          `yaml:"normalise_tokens,omitempty"`
	InfNames         []string      `yaml:"interface_names"`
	FinalSleep       time.Duration `yaml:"final_sleep"`

	// For testing, you can override the address and ID of this ingester
	Addr           string `yaml:"address"`
	Port           int
	ID             string
	SkipUnregister bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *LifecyclerConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet.
func (cfg *LifecyclerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.RingConfig.RegisterFlagsWithPrefix(prefix, f)

	// In order to keep backwards compatibility all of these need to be prefixed
	// with "ingester."
	if prefix == "" {
		prefix = "ingester."
	}

	f.IntVar(&cfg.NumTokens, prefix+"num-tokens", 128, "Number of tokens for each ingester.")
	f.DurationVar(&cfg.HeartbeatPeriod, prefix+"heartbeat-period", 5*time.Second, "Period at which to heartbeat to consul.")
	f.DurationVar(&cfg.JoinAfter, prefix+"join-after", 0*time.Second, "Period to wait for a claim from another member; will join automatically after this.")
	f.DurationVar(&cfg.ObservePeriod, prefix+"observe-period", 0*time.Second, "Observe tokens after generating to resolve collisions. Useful when using gossiping ring.")
	f.DurationVar(&cfg.MinReadyDuration, prefix+"min-ready-duration", 1*time.Minute, "Minimum duration to wait before becoming ready. This is to work around race conditions with ingesters exiting and updating the ring.")
	flagext.DeprecatedFlag(f, prefix+"claim-on-rollout", "DEPRECATED. This feature is no longer optional.")
	f.BoolVar(&cfg.NormaliseTokens, prefix+"normalise-tokens", false, "Store tokens in a normalised fashion to reduce allocations.")
	f.DurationVar(&cfg.FinalSleep, prefix+"final-sleep", 30*time.Second, "Duration to sleep for before exiting, to ensure metrics are scraped.")

	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	cfg.InfNames = []string{"eth0", "en0"}
	f.Var((*flagext.Strings)(&cfg.InfNames), prefix+"lifecycler.interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.Addr, prefix+"lifecycler.addr", "", "IP address to advertise in consul.")
	f.IntVar(&cfg.Port, prefix+"lifecycler.port", 0, "port to advertise in consul (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.ID, prefix+"lifecycler.ID", hostname, "ID to register into consul.")
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
	KVStore         kv.Client

	// Controls the lifecycle of the ingester
	quit      chan struct{}
	done      sync.WaitGroup
	actorChan chan func()

	// These values are initialised at startup, and never change
	ID       string
	Addr     string
	RingName string

	// We need to remember the ingester state just in case consul goes away and comes
	// back empty.  And it changes during lifecycle of ingester.
	stateMtx sync.Mutex
	state    IngesterState
	tokens   []uint32

	// Controls the ready-reporting
	readyLock sync.Mutex
	startTime time.Time
	ready     bool

	// Keeps stats updated at every heartbeat period
	countersLock          sync.RWMutex
	healthyInstancesCount int
}

// NewLifecycler makes and starts a new Lifecycler.
func NewLifecycler(cfg LifecyclerConfig, flushTransferer FlushTransferer, name string) (*Lifecycler, error) {
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
	codec := GetCodec()
	store, err := kv.NewClient(cfg.RingConfig.KVStore, codec)
	if err != nil {
		return nil, err
	}

	l := &Lifecycler{
		cfg:             cfg,
		flushTransferer: flushTransferer,
		KVStore:         store,

		Addr:     fmt.Sprintf("%s:%d", addr, port),
		ID:       cfg.ID,
		RingName: name,

		quit:      make(chan struct{}),
		actorChan: make(chan func()),

		state:     PENDING,
		startTime: time.Now(),
	}

	tokensToOwn.WithLabelValues(l.RingName).Set(float64(cfg.NumTokens))

	return l, nil
}

// Start the lifecycler
func (i *Lifecycler) Start() {
	i.done.Add(1)
	go i.loop()
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

	desc, err := i.KVStore.Get(ctx, ConsulKey)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error talking to consul", "err", err)
		return fmt.Errorf("error talking to consul: %s", err)
	}

	if len(i.getTokens()) == 0 {
		return fmt.Errorf("this ingester owns no tokens")
	}

	ringDesc, ok := desc.(*Desc)
	if !ok || ringDesc == nil {
		return fmt.Errorf("no ring returned from consul")
	}

	if err := ringDesc.Ready(time.Now(), i.cfg.RingConfig.HeartbeatTimeout); err != nil {
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
	tokensOwned.WithLabelValues(i.RingName).Set(float64(len(tokens)))

	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	i.tokens = tokens
}

// ClaimTokensFor takes all the tokens for the supplied ingester and assigns them to this ingester.
//
// For this method to work correctly (especially when using gossiping), source ingester (specified by
// ingesterID) must be in the LEAVING state, otherwise ring's merge function may detect token conflict and
// assign token to the wrong ingester. While we could check for that state here, when this method is called,
// transfers have already finished -- it's better to check for this *before* transfers start.
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
			// update timestamp to give gossiping client a chance register ring change.
			ing := ringDesc.Ingesters[i.ID]
			ing.Timestamp = time.Now().Unix()
			ringDesc.Ingesters[i.ID] = ing
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

// HealthyInstancesCount returns the number of healthy instances in the ring, updated
// during the last heartbeat period
func (i *Lifecycler) HealthyInstancesCount() int {
	i.countersLock.RLock()
	defer i.countersLock.RUnlock()

	return i.healthyInstancesCount
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
		level.Info(util.Logger).Log("msg", "member.loop() exited gracefully")
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
	var observeChan <-chan time.Time = nil

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

				if i.cfg.ObservePeriod > 0 {
					// let's observe the ring. By using JOINING state, this ingester will be ignored by LEAVING
					// ingesters, but we also signal that it is not fully functional yet.
					if err := i.autoJoin(context.Background(), JOINING); err != nil {
						level.Error(util.Logger).Log("msg", "failed to pick tokens in consul", "err", err)
						os.Exit(1)
					}

					level.Info(util.Logger).Log("msg", "observing tokens before going ACTIVE")
					observeChan = time.After(i.cfg.ObservePeriod)
				} else {
					if err := i.autoJoin(context.Background(), ACTIVE); err != nil {
						level.Error(util.Logger).Log("msg", "failed to pick tokens in consul", "err", err)
						os.Exit(1)
					}
				}
			}

		case <-observeChan:
			// if observeChan is nil, this case is ignored. We keep updating observeChan while observing the ring.
			// When observing is done, observeChan is set to nil.

			observeChan = nil
			if s := i.GetState(); s != JOINING {
				level.Error(util.Logger).Log("msg", "unexpected state while observing tokens", "state", s)
			}

			if i.verifyTokens(context.Background()) {
				level.Info(util.Logger).Log("msg", "token verification successful")

				err := i.changeState(context.Background(), ACTIVE)
				if err != nil {
					level.Error(util.Logger).Log("msg", "failed to set state to ACTIVE", "err", err)
				}
			} else {
				level.Info(util.Logger).Log("msg", "token verification failed, observing")
				// keep observing
				observeChan = time.After(i.cfg.ObservePeriod)
			}

		case <-heartbeatTicker.C:
			consulHeartbeats.WithLabelValues(i.RingName).Inc()
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
			consulHeartbeats.WithLabelValues(i.RingName).Inc()
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

	i.KVStore.Stop()
}

// initRing is the first thing we do when we start. It:
// - add an ingester entry to the ring
// - copies out our state and tokens if they exist
func (i *Lifecycler) initRing(ctx context.Context) error {
	var ringDesc *Desc

	err := i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		ingesterDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// Either we are a new ingester, or consul must have restarted
			level.Info(util.Logger).Log("msg", "entry not found in ring, adding with no tokens")
			ringDesc.AddIngester(i.ID, i.Addr, []uint32{}, i.GetState(), i.cfg.NormaliseTokens)
			return ringDesc, true, nil
		}

		// We exist in the ring, so assume the ring is right and copy out tokens & state out of there.
		i.setState(ingesterDesc.State)
		tokens, _ := ringDesc.TokensFor(i.ID)
		i.setTokens(tokens)

		level.Info(util.Logger).Log("msg", "existing entry found in ring", "state", i.GetState(), "tokens", len(tokens))
		// we haven't modified the ring, don't try to store it.
		return nil, true, nil
	})

	// Update counters
	if err == nil {
		i.updateCounters(ringDesc)
	}

	return err
}

// Verifies that tokens that this ingester has registered to the ring still belong to it.
// Gossiping ring may change the ownership of tokens in case of conflicts.
// If ingester doesn't own its tokens anymore, this method generates new tokens and puts them to the ring.
func (i *Lifecycler) verifyTokens(ctx context.Context) bool {
	result := false

	err := i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		var ringDesc *Desc
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		// At this point, we should have the same tokens as we have registered before
		ringTokens, takenTokens := ringDesc.TokensFor(i.ID)

		if !i.compareTokens(ringTokens) {
			// uh, oh... our tokens are not our anymore. Let's try new ones.
			needTokens := i.cfg.NumTokens - len(ringTokens)

			level.Info(util.Logger).Log("msg", "generating new tokens", "count", needTokens)
			newTokens := GenerateTokens(needTokens, takenTokens)

			ringTokens = append(ringTokens, newTokens...)
			sort.Sort(sortableUint32(ringTokens))

			ringDesc.AddIngester(i.ID, i.Addr, ringTokens, i.GetState(), i.cfg.NormaliseTokens)

			i.setTokens(ringTokens)

			return ringDesc, true, nil
		}

		// all is good, this ingester owns its tokens
		result = true
		return nil, true, nil
	})

	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to verify tokens", "err", err)
		return false
	}

	return result
}

func (i *Lifecycler) compareTokens(fromRing []uint32) bool {
	sort.Sort(sortableUint32(fromRing))

	tokens := i.getTokens()
	sort.Sort(sortableUint32(tokens))

	if len(tokens) != len(fromRing) {
		return false
	}

	for i := 0; i < len(tokens); i++ {
		if tokens[i] != fromRing[i] {
			return false
		}
	}
	return true
}

// autoJoin selects random tokens & moves state to targetState
func (i *Lifecycler) autoJoin(ctx context.Context, targetState IngesterState) error {
	var ringDesc *Desc

	err := i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
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
		i.setState(targetState)
		ringDesc.AddIngester(i.ID, i.Addr, newTokens, i.GetState(), i.cfg.NormaliseTokens)

		tokens := append(myTokens, newTokens...)
		sort.Sort(sortableUint32(tokens))
		i.setTokens(tokens)

		return ringDesc, true, nil
	})

	// Update counters
	if err == nil {
		i.updateCounters(ringDesc)
	}

	return err
}

// updateConsul updates our entries in consul, heartbeating and dealing with
// consul restarts.
func (i *Lifecycler) updateConsul(ctx context.Context) error {
	var ringDesc *Desc

	err := i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		ingesterDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// consul must have restarted
			level.Info(util.Logger).Log("msg", "found empty ring, inserting tokens")
			ringDesc.AddIngester(i.ID, i.Addr, i.getTokens(), i.GetState(), i.cfg.NormaliseTokens)
		} else {
			ingesterDesc.Timestamp = time.Now().Unix()
			ingesterDesc.State = i.GetState()
			ingesterDesc.Addr = i.Addr
			ringDesc.Ingesters[i.ID] = ingesterDesc
		}

		return ringDesc, true, nil
	})

	// Update counters
	if err == nil {
		i.updateCounters(ringDesc)
	}

	return err
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

func (i *Lifecycler) updateCounters(ringDesc *Desc) {
	// Count the number of healthy instances for Write operation
	healthyInstancesCount := 0

	if ringDesc != nil {
		for _, ingester := range ringDesc.Ingesters {
			if ingester.IsHealthy(Write, i.cfg.RingConfig.HeartbeatTimeout) {
				healthyInstancesCount++
			}
		}
	}

	// Update counters
	i.countersLock.Lock()
	i.healthyInstancesCount = healthyInstancesCount
	i.countersLock.Unlock()
}

func (i *Lifecycler) processShutdown(ctx context.Context) {
	flushRequired := true
	transferStart := time.Now()
	if err := i.flushTransferer.TransferOut(ctx); err != nil {
		level.Error(util.Logger).Log("msg", "Failed to transfer chunks to another ingester", "err", err)
		shutdownDuration.WithLabelValues("transfer", "fail", i.RingName).Observe(time.Since(transferStart).Seconds())
	} else {
		flushRequired = false
		shutdownDuration.WithLabelValues("transfer", "success", i.RingName).Observe(time.Since(transferStart).Seconds())
	}

	if flushRequired {
		flushStart := time.Now()
		i.flushTransferer.Flush()
		shutdownDuration.WithLabelValues("flush", "success", i.RingName).Observe(time.Since(flushStart).Seconds())
	}

	// Sleep so the shutdownDuration metric can be collected.
	time.Sleep(i.cfg.FinalSleep)
}

// unregister removes our entry from consul.
func (i *Lifecycler) unregister(ctx context.Context) error {
	level.Debug(util.Logger).Log("msg", "unregistering member from ring")

	return i.KVStore.CAS(ctx, ConsulKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			return nil, false, fmt.Errorf("found empty ring when trying to unregister")
		}

		ringDesc := in.(*Desc)
		ringDesc.RemoveIngester(i.ID)
		return ringDesc, true, nil
	})
}
