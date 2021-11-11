package ring

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	perrors "github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/services"
	dstime "github.com/grafana/dskit/time"
)

// LifecyclerConfig is the config to build a Lifecycler.
type LifecyclerConfig struct {
	RingConfig Config `yaml:"ring"`

	// Config for the ingester lifecycle control
	NumTokens                int           `yaml:"num_tokens"`
	HeartbeatPeriod          time.Duration `yaml:"heartbeat_period"`
	ObservePeriod            time.Duration `yaml:"observe_period"`
	JoinAfter                time.Duration `yaml:"join_after"`
	MinReadyDuration         time.Duration `yaml:"min_ready_duration"`
	InfNames                 []string      `yaml:"interface_names"`
	FinalSleep               time.Duration `yaml:"final_sleep"`
	TokensFilePath           string        `yaml:"tokens_file_path"`
	Zone                     string        `yaml:"availability_zone"`
	UnregisterOnShutdown     bool          `yaml:"unregister_on_shutdown"`
	ReadinessCheckRingHealth bool          `yaml:"readiness_check_ring_health"`

	// For testing, you can override the address and ID of this ingester
	Addr string `yaml:"address" doc:"hidden"`
	Port int    `doc:"hidden"`
	ID   string `doc:"hidden"`

	// Injected internally
	ListenPort int `yaml:"-"`
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
	f.DurationVar(&cfg.HeartbeatPeriod, prefix+"heartbeat-period", 5*time.Second, "Period at which to heartbeat to consul. 0 = disabled.")
	f.DurationVar(&cfg.JoinAfter, prefix+"join-after", 0*time.Second, "Period to wait for a claim from another member; will join automatically after this.")
	f.DurationVar(&cfg.ObservePeriod, prefix+"observe-period", 0*time.Second, "Observe tokens after generating to resolve collisions. Useful when using gossiping ring.")
	f.DurationVar(&cfg.MinReadyDuration, prefix+"min-ready-duration", 15*time.Second, "Minimum duration to wait after the internal readiness checks have passed but before succeeding the readiness endpoint. This is used to slowdown deployment controllers (eg. Kubernetes) after an instance is ready and before they proceed with a rolling update, to give the rest of the cluster instances enough time to receive ring updates.")
	f.DurationVar(&cfg.FinalSleep, prefix+"final-sleep", 30*time.Second, "Duration to sleep for before exiting, to ensure metrics are scraped.")
	f.StringVar(&cfg.TokensFilePath, prefix+"tokens-file-path", "", "File path where tokens are stored. If empty, tokens are not stored at shutdown and restored at startup.")

	hostname, err := os.Hostname()
	if err != nil {
		panic(fmt.Errorf("failed to get hostname %s", err))
	}

	cfg.InfNames = []string{"eth0", "en0"}
	f.Var((*flagext.StringSlice)(&cfg.InfNames), prefix+"lifecycler.interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.Addr, prefix+"lifecycler.addr", "", "IP address to advertise in the ring.")
	f.IntVar(&cfg.Port, prefix+"lifecycler.port", 0, "port to advertise in consul (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.ID, prefix+"lifecycler.ID", hostname, "ID to register in the ring.")
	f.StringVar(&cfg.Zone, prefix+"availability-zone", "", "The availability zone where this instance is running.")
	f.BoolVar(&cfg.UnregisterOnShutdown, prefix+"unregister-on-shutdown", true, "Unregister from the ring upon clean shutdown. It can be useful to disable for rolling restarts with consistent naming in conjunction with -distributor.extend-writes=false.")
	f.BoolVar(&cfg.ReadinessCheckRingHealth, prefix+"readiness-check-ring-health", true, "When enabled the readiness probe succeeds only after all instances are ACTIVE and healthy in the ring, otherwise only the instance itself is checked. This option should be disabled if in your cluster multiple instances can be rolled out simultaneously, otherwise rolling updates may be slowed down.")
}

// Lifecycler is responsible for managing the lifecycle of entries in the ring.
type Lifecycler struct {
	*services.BasicService

	cfg             LifecyclerConfig
	flushTransferer FlushTransferer
	KVStore         kv.Client

	actorChan chan func()

	// These values are initialised at startup, and never change
	ID       string
	Addr     string
	RingName string
	RingKey  string
	Zone     string

	// Whether to flush if transfer fails on shutdown.
	flushOnShutdown      *atomic.Bool
	unregisterOnShutdown *atomic.Bool

	// We need to remember the ingester state, tokens and registered timestamp just in case the KV store
	// goes away and comes back empty. The state changes during lifecycle of instance.
	stateMtx     sync.RWMutex
	state        InstanceState
	tokens       Tokens
	registeredAt time.Time

	// Controls the ready-reporting
	readyLock  sync.Mutex
	ready      bool
	readySince time.Time

	// Keeps stats updated at every heartbeat period
	countersLock          sync.RWMutex
	healthyInstancesCount int
	zonesCount            int

	lifecyclerMetrics *LifecyclerMetrics
	logger            log.Logger
}

// NewLifecycler creates new Lifecycler. It must be started via StartAsync.
func NewLifecycler(cfg LifecyclerConfig, flushTransferer FlushTransferer, ringName, ringKey string, flushOnShutdown bool, logger log.Logger, reg prometheus.Registerer) (*Lifecycler, error) {
	addr, err := GetInstanceAddr(cfg.Addr, cfg.InfNames, logger)
	if err != nil {
		return nil, err
	}
	port := GetInstancePort(cfg.Port, cfg.ListenPort)
	codec := GetCodec()
	// Suffix all client names with "-lifecycler" to denote this kv client is used by the lifecycler
	store, err := kv.NewClient(
		cfg.RingConfig.KVStore,
		codec,
		kv.RegistererWithKVName(reg, ringName+"-lifecycler"),
		logger,
	)
	if err != nil {
		return nil, err
	}

	zone := cfg.Zone
	if zone != "" {
		level.Warn(logger).Log("msg", "experimental feature in use", "feature", "Zone aware replication")
	}

	// We do allow a nil FlushTransferer, but to keep the ring logic easier we assume
	// it's always set, so we use a noop FlushTransferer
	if flushTransferer == nil {
		flushTransferer = NewNoopFlushTransferer()
	}

	l := &Lifecycler{
		cfg:                  cfg,
		flushTransferer:      flushTransferer,
		KVStore:              store,
		Addr:                 fmt.Sprintf("%s:%d", addr, port),
		ID:                   cfg.ID,
		RingName:             ringName,
		RingKey:              ringKey,
		flushOnShutdown:      atomic.NewBool(flushOnShutdown),
		unregisterOnShutdown: atomic.NewBool(cfg.UnregisterOnShutdown),
		Zone:                 zone,
		actorChan:            make(chan func()),
		state:                PENDING,
		lifecyclerMetrics:    NewLifecyclerMetrics(ringName, reg),
		logger:               logger,
	}

	l.lifecyclerMetrics.tokensToOwn.Set(float64(cfg.NumTokens))

	l.BasicService = services.
		NewBasicService(nil, l.loop, l.stopping).
		WithName(fmt.Sprintf("%s ring lifecycler", ringName))

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

	if err := i.checkRingHealthForReadiness(ctx); err != nil {
		// Reset the min ready duration counter.
		i.readySince = time.Time{}

		return err
	}

	// Honor the min ready duration. The duration counter start after all readiness checks have
	// passed.
	if i.readySince.IsZero() {
		i.readySince = time.Now()
	}
	if time.Since(i.readySince) < i.cfg.MinReadyDuration {
		return fmt.Errorf("waiting for %v after being ready", i.cfg.MinReadyDuration)
	}

	i.ready = true
	return nil
}

func (i *Lifecycler) checkRingHealthForReadiness(ctx context.Context) error {
	// Ensure the instance holds some tokens.
	if len(i.getTokens()) == 0 {
		return fmt.Errorf("this instance owns no tokens")
	}

	// If ring health checking is enabled we make sure all instances in the ring are ACTIVE and healthy,
	// otherwise we just check this instance.
	desc, err := i.KVStore.Get(ctx, i.RingKey)
	if err != nil {
		level.Error(i.logger).Log("msg", "error talking to the KV store", "ring", i.RingName, "err", err)
		return fmt.Errorf("error talking to the KV store: %s", err)
	}

	ringDesc, ok := desc.(*Desc)
	if !ok || ringDesc == nil {
		return fmt.Errorf("no ring returned from the KV store")
	}

	if i.cfg.ReadinessCheckRingHealth {
		if err := ringDesc.IsReady(time.Now(), i.cfg.RingConfig.HeartbeatTimeout); err != nil {
			level.Warn(i.logger).Log("msg", "found an existing instance(s) with a problem in the ring, "+
				"this instance cannot become ready until this problem is resolved. "+
				"The /ring http endpoint on the distributor (or single binary) provides visibility into the ring.",
				"ring", i.RingName, "err", err)
			return err
		}
	} else {
		instance, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			return fmt.Errorf("instance %s not found in the ring", i.ID)
		}

		if err := instance.IsReady(time.Now(), i.cfg.RingConfig.HeartbeatTimeout); err != nil {
			return err
		}
	}

	return nil
}

// GetState returns the state of this ingester.
func (i *Lifecycler) GetState() InstanceState {
	i.stateMtx.RLock()
	defer i.stateMtx.RUnlock()
	return i.state
}

func (i *Lifecycler) setState(state InstanceState) {
	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	i.state = state
}

func (i *Lifecycler) sendToLifecyclerLoop(fn func()) error {
	sc := i.ServiceContext()
	if sc == nil {
		return errors.New("lifecycler not running")
	}

	select {
	case <-sc.Done():
		return errors.New("lifecycler not running")
	case i.actorChan <- fn:
		return nil
	}
}

// ChangeState of the ingester, for use off of the loop() goroutine.
func (i *Lifecycler) ChangeState(ctx context.Context, state InstanceState) error {
	errCh := make(chan error)
	fn := func() {
		errCh <- i.changeState(ctx, state)
	}

	if err := i.sendToLifecyclerLoop(fn); err != nil {
		return err
	}
	return <-errCh
}

func (i *Lifecycler) getTokens() Tokens {
	i.stateMtx.RLock()
	defer i.stateMtx.RUnlock()
	return i.tokens
}

func (i *Lifecycler) setTokens(tokens Tokens) {
	i.lifecyclerMetrics.tokensOwned.Set(float64(len(tokens)))

	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()

	i.tokens = tokens
	if i.cfg.TokensFilePath != "" {
		if err := i.tokens.StoreToFile(i.cfg.TokensFilePath); err != nil {
			level.Error(i.logger).Log("msg", "error storing tokens to disk", "path", i.cfg.TokensFilePath, "err", err)
		}
	}
}

func (i *Lifecycler) getRegisteredAt() time.Time {
	i.stateMtx.RLock()
	defer i.stateMtx.RUnlock()
	return i.registeredAt
}

func (i *Lifecycler) setRegisteredAt(registeredAt time.Time) {
	i.stateMtx.Lock()
	defer i.stateMtx.Unlock()
	i.registeredAt = registeredAt
}

// ClaimTokensFor takes all the tokens for the supplied ingester and assigns them to this ingester.
//
// For this method to work correctly (especially when using gossiping), source ingester (specified by
// ingesterID) must be in the LEAVING state, otherwise ring's merge function may detect token conflict and
// assign token to the wrong ingester. While we could check for that state here, when this method is called,
// transfers have already finished -- it's better to check for this *before* transfers start.
func (i *Lifecycler) ClaimTokensFor(ctx context.Context, ingesterID string) error {
	errCh := make(chan error)

	fn := func() {
		var tokens Tokens

		claimTokens := func(in interface{}) (out interface{}, retry bool, err error) {
			ringDesc, ok := in.(*Desc)
			if !ok || ringDesc == nil {
				return nil, false, fmt.Errorf("Cannot claim tokens in an empty ring")
			}

			tokens = ringDesc.ClaimTokens(ingesterID, i.ID)
			// update timestamp to give gossiping client a chance register ring change.
			ing := ringDesc.Ingesters[i.ID]
			ing.Timestamp = time.Now().Unix()

			// Tokens of the leaving ingester may have been generated by an older version which
			// doesn't guarantee sorted tokens, so we enforce sorting here.
			sort.Sort(tokens)
			ing.Tokens = tokens

			ringDesc.Ingesters[i.ID] = ing
			return ringDesc, true, nil
		}

		if err := i.KVStore.CAS(ctx, i.RingKey, claimTokens); err != nil {
			level.Error(i.logger).Log("msg", "Failed to write to the KV store", "ring", i.RingName, "err", err)
		}

		i.setTokens(tokens)
		errCh <- nil
	}

	if err := i.sendToLifecyclerLoop(fn); err != nil {
		return err
	}
	return <-errCh
}

// HealthyInstancesCount returns the number of healthy instances for the Write operation
// in the ring, updated during the last heartbeat period.
func (i *Lifecycler) HealthyInstancesCount() int {
	i.countersLock.RLock()
	defer i.countersLock.RUnlock()

	return i.healthyInstancesCount
}

// ZonesCount returns the number of zones for which there's at least 1 instance registered
// in the ring.
func (i *Lifecycler) ZonesCount() int {
	i.countersLock.RLock()
	defer i.countersLock.RUnlock()

	return i.zonesCount
}

func (i *Lifecycler) loop(ctx context.Context) error {
	// First, see if we exist in the cluster, update our state to match if we do,
	// and add ourselves (without tokens) if we don't.
	if err := i.initRing(context.Background()); err != nil {
		return perrors.Wrapf(err, "failed to join the ring %s", i.RingName)
	}

	// We do various period tasks
	autoJoinAfter := time.After(i.cfg.JoinAfter)
	var observeChan <-chan time.Time

	heartbeatTickerStop, heartbeatTickerChan := dstime.NewDisableableTicker(i.cfg.HeartbeatPeriod)
	defer heartbeatTickerStop()

	for {
		select {
		case <-autoJoinAfter:
			level.Debug(i.logger).Log("msg", "JoinAfter expired", "ring", i.RingName)
			// Will only fire once, after auto join timeout.  If we haven't entered "JOINING" state,
			// then pick some tokens and enter ACTIVE state.
			if i.GetState() == PENDING {
				level.Info(i.logger).Log("msg", "auto-joining cluster after timeout", "ring", i.RingName)

				if i.cfg.ObservePeriod > 0 {
					// let's observe the ring. By using JOINING state, this ingester will be ignored by LEAVING
					// ingesters, but we also signal that it is not fully functional yet.
					if err := i.autoJoin(context.Background(), JOINING); err != nil {
						return perrors.Wrapf(err, "failed to pick tokens in the KV store, ring: %s", i.RingName)
					}

					level.Info(i.logger).Log("msg", "observing tokens before going ACTIVE", "ring", i.RingName)
					observeChan = time.After(i.cfg.ObservePeriod)
				} else {
					if err := i.autoJoin(context.Background(), ACTIVE); err != nil {
						return perrors.Wrapf(err, "failed to pick tokens in the KV store, ring: %s", i.RingName)
					}
				}
			}

		case <-observeChan:
			// if observeChan is nil, this case is ignored. We keep updating observeChan while observing the ring.
			// When observing is done, observeChan is set to nil.

			observeChan = nil
			if s := i.GetState(); s != JOINING {
				level.Error(i.logger).Log("msg", "unexpected state while observing tokens", "state", s, "ring", i.RingName)
			}

			if i.verifyTokens(context.Background()) {
				level.Info(i.logger).Log("msg", "token verification successful", "ring", i.RingName)

				err := i.changeState(context.Background(), ACTIVE)
				if err != nil {
					level.Error(i.logger).Log("msg", "failed to set state to ACTIVE", "ring", i.RingName, "err", err)
				}
			} else {
				level.Info(i.logger).Log("msg", "token verification failed, observing", "ring", i.RingName)
				// keep observing
				observeChan = time.After(i.cfg.ObservePeriod)
			}

		case <-heartbeatTickerChan:
			i.lifecyclerMetrics.consulHeartbeats.Inc()
			if err := i.updateConsul(context.Background()); err != nil {
				level.Error(i.logger).Log("msg", "failed to write to the KV store, sleeping", "ring", i.RingName, "err", err)
			}

		case f := <-i.actorChan:
			f()

		case <-ctx.Done():
			level.Info(i.logger).Log("msg", "lifecycler loop() exited gracefully", "ring", i.RingName)
			return nil
		}
	}
}

// Shutdown the lifecycle.  It will:
// - send chunks to another ingester, if it can.
// - otherwise, flush chunks to the chunk store.
// - remove config from Consul.
func (i *Lifecycler) stopping(runningError error) error {
	if runningError != nil {
		// previously lifecycler just called os.Exit (from loop method)...
		// now it stops more gracefully, but also without doing any cleanup
		return nil
	}

	heartbeatTickerStop, heartbeatTickerChan := dstime.NewDisableableTicker(i.cfg.HeartbeatPeriod)
	defer heartbeatTickerStop()

	// Mark ourselved as Leaving so no more samples are send to us.
	err := i.changeState(context.Background(), LEAVING)
	if err != nil {
		level.Error(i.logger).Log("msg", "failed to set state to LEAVING", "ring", i.RingName, "err", err)
	}

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
		case <-heartbeatTickerChan:
			i.lifecyclerMetrics.consulHeartbeats.Inc()
			if err := i.updateConsul(context.Background()); err != nil {
				level.Error(i.logger).Log("msg", "failed to write to the KV store, sleeping", "ring", i.RingName, "err", err)
			}

		case <-done:
			break heartbeatLoop
		}
	}

	if i.ShouldUnregisterOnShutdown() {
		if err := i.unregister(context.Background()); err != nil {
			return perrors.Wrapf(err, "failed to unregister from the KV store, ring: %s", i.RingName)
		}
		level.Info(i.logger).Log("msg", "instance removed from the KV store", "ring", i.RingName)
	}

	return nil
}

// initRing is the first thing we do when we start. It:
// - add an ingester entry to the ring
// - copies out our state and tokens if they exist
func (i *Lifecycler) initRing(ctx context.Context) error {
	var (
		ringDesc       *Desc
		tokensFromFile Tokens
		err            error
	)

	if i.cfg.TokensFilePath != "" {
		tokensFromFile, err = LoadTokensFromFile(i.cfg.TokensFilePath)
		if err != nil && !os.IsNotExist(err) {
			level.Error(i.logger).Log("msg", "error loading tokens from file", "err", err)
		}
	} else {
		level.Info(i.logger).Log("msg", "not loading tokens from file, tokens file path is empty")
	}

	err = i.KVStore.CAS(ctx, i.RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		instanceDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// The instance doesn't exist in the ring, so it's safe to set the registered timestamp
			// as of now.
			registeredAt := time.Now()
			i.setRegisteredAt(registeredAt)

			// We use the tokens from the file only if it does not exist in the ring yet.
			if len(tokensFromFile) > 0 {
				level.Info(i.logger).Log("msg", "adding tokens from file", "num_tokens", len(tokensFromFile))
				if len(tokensFromFile) >= i.cfg.NumTokens {
					i.setState(ACTIVE)
				}
				ringDesc.AddIngester(i.ID, i.Addr, i.Zone, tokensFromFile, i.GetState(), registeredAt)
				i.setTokens(tokensFromFile)
				return ringDesc, true, nil
			}

			// Either we are a new ingester, or consul must have restarted
			level.Info(i.logger).Log("msg", "instance not found in ring, adding with no tokens", "ring", i.RingName)
			ringDesc.AddIngester(i.ID, i.Addr, i.Zone, []uint32{}, i.GetState(), registeredAt)
			return ringDesc, true, nil
		}

		// The instance already exists in the ring, so we can't change the registered timestamp (even if it's zero)
		// but we need to update the local state accordingly.
		i.setRegisteredAt(instanceDesc.GetRegisteredAt())

		// If the ingester is in the JOINING state this means it crashed due to
		// a failed token transfer or some other reason during startup. We want
		// to set it back to PENDING in order to start the lifecycle from the
		// beginning.
		if instanceDesc.State == JOINING {
			level.Warn(i.logger).Log("msg", "instance found in ring as JOINING, setting to PENDING",
				"ring", i.RingName)
			instanceDesc.State = PENDING
			return ringDesc, true, nil
		}

		// If the ingester failed to clean its ring entry up in can leave its state in LEAVING
		// OR unregister_on_shutdown=false
		// Move it into ACTIVE to ensure the ingester joins the ring.
		if instanceDesc.State == LEAVING && len(instanceDesc.Tokens) == i.cfg.NumTokens {
			instanceDesc.State = ACTIVE
		}

		// We exist in the ring, so assume the ring is right and copy out tokens & state out of there.
		i.setState(instanceDesc.State)
		tokens, _ := ringDesc.TokensFor(i.ID)
		i.setTokens(tokens)

		level.Info(i.logger).Log("msg", "existing entry found in ring", "state", i.GetState(), "tokens", len(tokens), "ring", i.RingName)

		// Update the ring if the instance has been changed and the heartbeat is disabled.
		// We dont need to update KV here when heartbeat is enabled as this info will eventually be update on KV
		// on the next heartbeat
		if i.cfg.HeartbeatPeriod == 0 && !instanceDesc.Equal(ringDesc.Ingesters[i.ID]) {
			// Update timestamp to give gossiping client a chance register ring change.
			instanceDesc.Timestamp = time.Now().Unix()
			ringDesc.Ingesters[i.ID] = instanceDesc
			return ringDesc, true, nil
		}

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

	err := i.KVStore.CAS(ctx, i.RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
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

			level.Info(i.logger).Log("msg", "generating new tokens", "count", needTokens, "ring", i.RingName)
			newTokens := GenerateTokens(needTokens, takenTokens)

			ringTokens = append(ringTokens, newTokens...)
			sort.Sort(ringTokens)

			ringDesc.AddIngester(i.ID, i.Addr, i.Zone, ringTokens, i.GetState(), i.getRegisteredAt())

			i.setTokens(ringTokens)

			return ringDesc, true, nil
		}

		// all is good, this ingester owns its tokens
		result = true
		return nil, true, nil
	})

	if err != nil {
		level.Error(i.logger).Log("msg", "failed to verify tokens", "ring", i.RingName, "err", err)
		return false
	}

	return result
}

func (i *Lifecycler) compareTokens(fromRing Tokens) bool {
	sort.Sort(fromRing)

	tokens := i.getTokens()
	sort.Sort(tokens)

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
func (i *Lifecycler) autoJoin(ctx context.Context, targetState InstanceState) error {
	var ringDesc *Desc

	err := i.KVStore.CAS(ctx, i.RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		// At this point, we should not have any tokens, and we should be in PENDING state.
		myTokens, takenTokens := ringDesc.TokensFor(i.ID)
		if len(myTokens) > 0 {
			level.Error(i.logger).Log("msg", "tokens already exist for this instance - wasn't expecting any!", "num_tokens", len(myTokens), "ring", i.RingName)
		}

		newTokens := GenerateTokens(i.cfg.NumTokens-len(myTokens), takenTokens)
		i.setState(targetState)

		myTokens = append(myTokens, newTokens...)
		sort.Sort(myTokens)
		i.setTokens(myTokens)

		ringDesc.AddIngester(i.ID, i.Addr, i.Zone, i.getTokens(), i.GetState(), i.getRegisteredAt())

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

	err := i.KVStore.CAS(ctx, i.RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			ringDesc = NewDesc()
		} else {
			ringDesc = in.(*Desc)
		}

		instanceDesc, ok := ringDesc.Ingesters[i.ID]
		if !ok {
			// consul must have restarted
			level.Info(i.logger).Log("msg", "found empty ring, inserting tokens", "ring", i.RingName)
			ringDesc.AddIngester(i.ID, i.Addr, i.Zone, i.getTokens(), i.GetState(), i.getRegisteredAt())
		} else {
			instanceDesc.Timestamp = time.Now().Unix()
			instanceDesc.State = i.GetState()
			instanceDesc.Addr = i.Addr
			instanceDesc.Zone = i.Zone
			instanceDesc.RegisteredTimestamp = i.getRegisteredAt().Unix()
			ringDesc.Ingesters[i.ID] = instanceDesc
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
func (i *Lifecycler) changeState(ctx context.Context, state InstanceState) error {
	currState := i.GetState()
	// Only the following state transitions can be triggered externally
	if !((currState == PENDING && state == JOINING) || // triggered by TransferChunks at the beginning
		(currState == JOINING && state == PENDING) || // triggered by TransferChunks on failure
		(currState == JOINING && state == ACTIVE) || // triggered by TransferChunks on success
		(currState == PENDING && state == ACTIVE) || // triggered by autoJoin
		(currState == ACTIVE && state == LEAVING)) { // triggered by shutdown
		return fmt.Errorf("Changing instance state from %v -> %v is disallowed", currState, state)
	}

	level.Info(i.logger).Log("msg", "changing instance state from", "old_state", currState, "new_state", state, "ring", i.RingName)
	i.setState(state)
	return i.updateConsul(ctx)
}

func (i *Lifecycler) updateCounters(ringDesc *Desc) {
	healthyInstancesCount := 0
	zones := map[string]struct{}{}

	if ringDesc != nil {
		now := time.Now()

		for _, ingester := range ringDesc.Ingesters {
			zones[ingester.Zone] = struct{}{}

			// Count the number of healthy instances for Write operation.
			if ingester.IsHealthy(Write, i.cfg.RingConfig.HeartbeatTimeout, now) {
				healthyInstancesCount++
			}
		}
	}

	// Update counters
	i.countersLock.Lock()
	i.healthyInstancesCount = healthyInstancesCount
	i.zonesCount = len(zones)
	i.countersLock.Unlock()
}

// FlushOnShutdown returns if flushing is enabled if transfer fails on a shutdown.
func (i *Lifecycler) FlushOnShutdown() bool {
	return i.flushOnShutdown.Load()
}

// SetFlushOnShutdown enables/disables flush on shutdown if transfer fails.
// Passing 'true' enables it, and 'false' disabled it.
func (i *Lifecycler) SetFlushOnShutdown(flushOnShutdown bool) {
	i.flushOnShutdown.Store(flushOnShutdown)
}

// ShouldUnregisterOnShutdown returns if unregistering should be skipped on shutdown.
func (i *Lifecycler) ShouldUnregisterOnShutdown() bool {
	return i.unregisterOnShutdown.Load()
}

// SetUnregisterOnShutdown enables/disables unregistering on shutdown.
func (i *Lifecycler) SetUnregisterOnShutdown(enabled bool) {
	i.unregisterOnShutdown.Store(enabled)
}

func (i *Lifecycler) processShutdown(ctx context.Context) {
	flushRequired := i.flushOnShutdown.Load()
	transferStart := time.Now()
	if err := i.flushTransferer.TransferOut(ctx); err != nil {
		if err == ErrTransferDisabled {
			level.Info(i.logger).Log("msg", "transfers are disabled")
		} else {
			level.Error(i.logger).Log("msg", "failed to transfer chunks to another instance", "ring", i.RingName, "err", err)
			i.lifecyclerMetrics.shutdownDuration.WithLabelValues("transfer", "fail").Observe(time.Since(transferStart).Seconds())
		}
	} else {
		flushRequired = false
		i.lifecyclerMetrics.shutdownDuration.WithLabelValues("transfer", "success").Observe(time.Since(transferStart).Seconds())
	}

	if flushRequired {
		flushStart := time.Now()
		i.flushTransferer.Flush()
		i.lifecyclerMetrics.shutdownDuration.WithLabelValues("flush", "success").Observe(time.Since(flushStart).Seconds())
	}

	// Sleep so the shutdownDuration metric can be collected.
	time.Sleep(i.cfg.FinalSleep)
}

// unregister removes our entry from consul.
func (i *Lifecycler) unregister(ctx context.Context) error {
	level.Debug(i.logger).Log("msg", "unregistering instance from ring", "ring", i.RingName)

	return i.KVStore.CAS(ctx, i.RingKey, func(in interface{}) (out interface{}, retry bool, err error) {
		if in == nil {
			return nil, false, fmt.Errorf("found empty ring when trying to unregister")
		}

		ringDesc := in.(*Desc)
		ringDesc.RemoveIngester(i.ID)
		return ringDesc, true, nil
	})
}
