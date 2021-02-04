package alertmanager

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/cortexproject/cortex/pkg/util/services"
)

const (
	// If a config sets the webhook URL to this, it will be rewritten to
	// a URL derived from Config.AutoWebhookRoot
	autoWebhookURL = "http://internal.monitor"

	// Reasons for (re)syncing alertmanager configurations from object storage.
	reasonPeriodic   = "periodic"
	reasonInitial    = "initial"
	reasonRingChange = "ring-change"

	// ringAutoForgetUnhealthyPeriods is how many consecutive timeout periods an unhealthy instance
	// in the ring will be automatically removed.
	ringAutoForgetUnhealthyPeriods = 5

	statusPage = `
<!doctype html>
<html>
	<head><title>Cortex Alertmanager Status</title></head>
	<body>
		<h1>Cortex Alertmanager Status</h1>
		<h2>Node</h2>
		<dl>
			<dt>Name</dt><dd>{{.self.Name}}</dd>
			<dt>Addr</dt><dd>{{.self.Addr}}</dd>
			<dt>Port</dt><dd>{{.self.Port}}</dd>
		</dl>
		<h3>Members</h3>
		{{ with .members }}
		<table>
		<tr><th>Name</th><th>Addr</th></tr>
		{{ range . }}
		<tr><td>{{ .Name }}</td><td>{{ .Addr }}</td></tr>
		{{ end }}
		</table>
		{{ else }}
		<p>No peers</p>
		{{ end }}
	</body>
</html>
`
)

var (
	statusTemplate *template.Template
)

func init() {
	statusTemplate = template.Must(template.New("statusPage").Funcs(map[string]interface{}{
		"state": func(enabled bool) string {
			if enabled {
				return "enabled"
			}
			return "disabled"
		},
	}).Parse(statusPage))
}

// MultitenantAlertmanagerConfig is the configuration for a multitenant Alertmanager.
type MultitenantAlertmanagerConfig struct {
	DataDir      string           `yaml:"data_dir"`
	Retention    time.Duration    `yaml:"retention"`
	ExternalURL  flagext.URLValue `yaml:"external_url"`
	PollInterval time.Duration    `yaml:"poll_interval"`

	DeprecatedClusterBindAddr      string              `yaml:"cluster_bind_address"`
	DeprecatedClusterAdvertiseAddr string              `yaml:"cluster_advertise_address"`
	DeprecatedPeers                flagext.StringSlice `yaml:"peers"`
	DeprecatedPeerTimeout          time.Duration       `yaml:"peer_timeout"`

	// Enable sharding for the Alertmanager
	ShardingEnabled bool       `yaml:"sharding_enabled"`
	ShardingRing    RingConfig `yaml:"sharding_ring"`

	FallbackConfigFile string `yaml:"fallback_config_file"`
	AutoWebhookRoot    string `yaml:"auto_webhook_root"`

	Store   AlertStoreConfig `yaml:"storage"`
	Cluster ClusterConfig    `yaml:"cluster"`

	EnableAPI bool `yaml:"enable_api"`
}

type ClusterConfig struct {
	ListenAddr       string                 `yaml:"listen_address"`
	AdvertiseAddr    string                 `yaml:"advertise_address"`
	Peers            flagext.StringSliceCSV `yaml:"peers"`
	PeerTimeout      time.Duration          `yaml:"peer_timeout"`
	GossipInterval   time.Duration          `yaml:"gossip_interval"`
	PushPullInterval time.Duration          `yaml:"push_pull_interval"`
}

const (
	defaultClusterAddr = "0.0.0.0:9094"
	defaultPeerTimeout = 15 * time.Second
)

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *MultitenantAlertmanagerConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.DataDir, "alertmanager.storage.path", "data/", "Base path for data storage.")
	f.DurationVar(&cfg.Retention, "alertmanager.storage.retention", 5*24*time.Hour, "How long to keep data for.")

	f.Var(&cfg.ExternalURL, "alertmanager.web.external-url", "The URL under which Alertmanager is externally reachable (for example, if Alertmanager is served via a reverse proxy). Used for generating relative and absolute links back to Alertmanager itself. If the URL has a path portion, it will be used to prefix all HTTP endpoints served by Alertmanager. If omitted, relevant URL components will be derived automatically.")

	f.StringVar(&cfg.FallbackConfigFile, "alertmanager.configs.fallback", "", "Filename of fallback config to use if none specified for instance.")
	f.StringVar(&cfg.AutoWebhookRoot, "alertmanager.configs.auto-webhook-root", "", "Root of URL to generate if config is "+autoWebhookURL)
	f.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll Cortex configs")

	// Flags prefixed with `cluster` are deprecated in favor of their `alertmanager` prefix equivalent.
	// TODO: New flags introduced in Cortex 1.7, remove old ones in Cortex 1.9
	f.StringVar(&cfg.DeprecatedClusterBindAddr, "cluster.listen-address", defaultClusterAddr, "Deprecated. Use -alertmanager.cluster.listen-address instead.")
	f.StringVar(&cfg.DeprecatedClusterAdvertiseAddr, "cluster.advertise-address", "", "Deprecated. Use -alertmanager.cluster.advertise-address instead.")
	f.Var(&cfg.DeprecatedPeers, "cluster.peer", "Deprecated. Use -alertmanager.cluster.peers instead.")
	f.DurationVar(&cfg.DeprecatedPeerTimeout, "cluster.peer-timeout", time.Second*15, "Deprecated. Use -alertmanager.cluster.peer-timeout instead.")

	f.BoolVar(&cfg.EnableAPI, "experimental.alertmanager.enable-api", false, "Enable the experimental alertmanager config api.")

	f.BoolVar(&cfg.ShardingEnabled, "alertmanager.sharding-enabled", false, "Shard tenants across multiple alertmanager instances.")

	cfg.ShardingRing.RegisterFlags(f)
	cfg.Store.RegisterFlags(f)
	cfg.Cluster.RegisterFlags(f)
}

func (cfg *ClusterConfig) RegisterFlags(f *flag.FlagSet) {
	prefix := "alertmanager.cluster."
	f.StringVar(&cfg.ListenAddr, prefix+"listen-address", defaultClusterAddr, "Listen address and port for the cluster. Not specifying this flag disables high-availability mode.")
	f.StringVar(&cfg.AdvertiseAddr, prefix+"advertise-address", "", "Explicit address or hostname to advertise in cluster.")
	f.Var(&cfg.Peers, prefix+"peers", "Comma-separated list of initial peers.")
	f.DurationVar(&cfg.PeerTimeout, prefix+"peer-timeout", defaultPeerTimeout, "Time to wait between peers to send notifications.")
	f.DurationVar(&cfg.GossipInterval, prefix+"gossip-interval", cluster.DefaultGossipInterval, "The interval between sending gossip messages. By lowering this value (more frequent) gossip messages are propagated across cluster more quickly at the expense of increased bandwidth usage.")
	f.DurationVar(&cfg.PushPullInterval, prefix+"push-pull-interval", cluster.DefaultPushPullInterval, "The interval between gossip state syncs. Setting this interval lower (more frequent) will increase convergence speeds across larger clusters at the expense of increased bandwidth usage.")
}

// SupportDeprecatedFlagset ensures we support the previous set of cluster flags that are now deprecated.
func (cfg *ClusterConfig) SupportDeprecatedFlagset(amCfg *MultitenantAlertmanagerConfig, logger log.Logger) {
	if amCfg.DeprecatedClusterBindAddr != defaultClusterAddr {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -cluster.listen-address, use -alertmanager.cluster.listen-address instead.")
		cfg.ListenAddr = amCfg.DeprecatedClusterBindAddr
	}

	if amCfg.DeprecatedClusterAdvertiseAddr != "" {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -cluster.advertise-address, use -alertmanager.cluster.advertise-address instead.")
		cfg.AdvertiseAddr = amCfg.DeprecatedClusterAdvertiseAddr
	}

	if len(amCfg.DeprecatedPeers) > 0 {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -cluster.peer, use -alertmanager.cluster.peers instead.")
		cfg.Peers = []string(amCfg.DeprecatedPeers)
	}

	if amCfg.DeprecatedPeerTimeout != defaultPeerTimeout {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -cluster.peer-timeout, use -alertmanager.cluster.peer-timeout instead.")
		cfg.PeerTimeout = amCfg.DeprecatedPeerTimeout
	}
}

// Validate config and returns error on failure
func (cfg *MultitenantAlertmanagerConfig) Validate() error {
	if err := cfg.Store.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	return nil
}

type multitenantAlertmanagerMetrics struct {
	lastReloadSuccessful          *prometheus.GaugeVec
	lastReloadSuccessfulTimestamp *prometheus.GaugeVec
}

func newMultitenantAlertmanagerMetrics(reg prometheus.Registerer) *multitenantAlertmanagerMetrics {
	m := &multitenantAlertmanagerMetrics{}

	m.lastReloadSuccessful = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "alertmanager_config_last_reload_successful",
		Help:      "Boolean set to 1 whenever the last configuration reload attempt was successful.",
	}, []string{"user"})

	m.lastReloadSuccessfulTimestamp = promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "alertmanager_config_last_reload_successful_seconds",
		Help:      "Timestamp of the last successful configuration reload.",
	}, []string{"user"})

	return m
}

// A MultitenantAlertmanager manages Alertmanager instances for multiple
// organizations.
type MultitenantAlertmanager struct {
	services.Service

	cfg *MultitenantAlertmanagerConfig

	// Ring used for sharding alertmanager instances.
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring

	// Subservices manager (ring, lifecycler)
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	store AlertStore

	// The fallback config is stored as a string and parsed every time it's needed
	// because we mutate the parsed results and don't want those changes to take
	// effect here.
	fallbackConfig string

	alertmanagersMtx sync.Mutex
	alertmanagers    map[string]*Alertmanager
	// Stores the current set of configurations we're running in each tenant's Alertmanager.
	// Used for comparing configurations as we synchronize them.
	cfgs map[string]alerts.AlertConfigDesc

	logger              log.Logger
	alertmanagerMetrics *alertmanagerMetrics
	multitenantMetrics  *multitenantAlertmanagerMetrics

	peer *cluster.Peer

	registry          prometheus.Registerer
	ringCheckErrors   prometheus.Counter
	tenantsOwned      prometheus.Gauge
	tenantsDiscovered prometheus.Gauge
	syncTotal         *prometheus.CounterVec
	syncFailures      *prometheus.CounterVec
}

// NewMultitenantAlertmanager creates a new MultitenantAlertmanager.
func NewMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, logger log.Logger, registerer prometheus.Registerer) (*MultitenantAlertmanager, error) {
	err := os.MkdirAll(cfg.DataDir, 0777)
	if err != nil {
		return nil, fmt.Errorf("unable to create Alertmanager data directory %q: %s", cfg.DataDir, err)
	}

	if cfg.ExternalURL.URL == nil {
		return nil, fmt.Errorf("unable to create Alertmanager because the external URL has not been configured")
	}

	var fallbackConfig []byte
	if cfg.FallbackConfigFile != "" {
		fallbackConfig, err = ioutil.ReadFile(cfg.FallbackConfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read fallback config %q: %s", cfg.FallbackConfigFile, err)
		}
		_, err = amconfig.LoadFile(cfg.FallbackConfigFile)
		if err != nil {
			return nil, fmt.Errorf("unable to load fallback config %q: %s", cfg.FallbackConfigFile, err)
		}
	}

	cfg.Cluster.SupportDeprecatedFlagset(cfg, logger)

	var peer *cluster.Peer
	if cfg.Cluster.ListenAddr != "" {
		peer, err = cluster.Create(
			log.With(logger, "component", "cluster"),
			registerer,
			cfg.Cluster.ListenAddr,
			cfg.Cluster.AdvertiseAddr,
			cfg.Cluster.Peers,
			true,
			cfg.Cluster.PushPullInterval,
			cfg.Cluster.GossipInterval,
			cluster.DefaultTcpTimeout,
			cluster.DefaultProbeTimeout,
			cluster.DefaultProbeInterval,
		)
		if err != nil {
			return nil, errors.Wrap(err, "unable to initialize gossip mesh")
		}
		err = peer.Join(cluster.DefaultReconnectInterval, cluster.DefaultReconnectTimeout)
		if err != nil {
			level.Warn(logger).Log("msg", "unable to join gossip mesh while initializing cluster for high availability mode", "err", err)
		}
		go peer.Settle(context.Background(), cluster.DefaultGossipInterval)
	}

	store, err := NewAlertStore(cfg.Store)
	if err != nil {
		return nil, err
	}

	var ringStore kv.Client
	if cfg.ShardingEnabled {
		ringStore, err = kv.NewClient(
			cfg.ShardingRing.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(registerer, "alertmanager"),
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}
	}

	return createMultitenantAlertmanager(cfg, fallbackConfig, peer, store, ringStore, logger, registerer)
}

func createMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, fallbackConfig []byte, peer *cluster.Peer, store AlertStore, ringStore kv.Client, logger log.Logger, registerer prometheus.Registerer) (*MultitenantAlertmanager, error) {
	am := &MultitenantAlertmanager{
		cfg:                 cfg,
		fallbackConfig:      string(fallbackConfig),
		cfgs:                map[string]alerts.AlertConfigDesc{},
		alertmanagers:       map[string]*Alertmanager{},
		alertmanagerMetrics: newAlertmanagerMetrics(),
		multitenantMetrics:  newMultitenantAlertmanagerMetrics(registerer),
		peer:                peer,
		store:               store,
		logger:              log.With(logger, "component", "MultiTenantAlertmanager"),
		registry:            registerer,
		ringCheckErrors: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_alertmanager_ring_check_errors_total",
			Help: "Number of errors that have occurred when checking the ring for ownership.",
		}),
		syncTotal: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_alertmanager_sync_configs_total",
			Help: "Total number of times the alertmanager sync operation triggered.",
		}, []string{"reason"}),
		syncFailures: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_alertmanager_sync_configs_failed_total",
			Help: "Total number of times the alertmanager sync operation failed.",
		}, []string{"reason"}),
		tenantsDiscovered: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_alertmanager_tenants_discovered",
			Help: "Number of tenants with an Alertmanager configuration discovered.",
		}),
		tenantsOwned: promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_alertmanager_tenants_owned",
			Help: "Current number of tenants owned by the Alertmanager instance.",
		}),
	}

	// Initialize the top-level metrics.
	for _, r := range []string{reasonInitial, reasonPeriodic, reasonRingChange} {
		am.syncTotal.WithLabelValues(r)
		am.syncFailures.WithLabelValues(r)
	}

	if cfg.ShardingEnabled {
		lifecyclerCfg, err := am.cfg.ShardingRing.ToLifecyclerConfig()
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize Alertmanager's lifecycler config")
		}

		// Define lifecycler delegates in reverse order (last to be called defined first because they're
		// chained via "next delegate").
		delegate := ring.BasicLifecyclerDelegate(am)
		delegate = ring.NewLeaveOnStoppingDelegate(delegate, am.logger)
		delegate = ring.NewAutoForgetDelegate(am.cfg.ShardingRing.HeartbeatTimeout*ringAutoForgetUnhealthyPeriods, delegate, am.logger)

		am.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, RingNameForServer, RingKey, ringStore, delegate, am.logger, am.registry)
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize Alertmanager's lifecycler")
		}

		am.ring, err = ring.NewWithStoreClientAndStrategy(am.cfg.ShardingRing.ToRingConfig(), RingNameForServer, RingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy())
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize Alertmanager's ring")
		}

		if am.registry != nil {
			am.registry.MustRegister(am.ring)
		}
	}

	if registerer != nil {
		registerer.MustRegister(am.alertmanagerMetrics)
	}

	am.Service = services.NewBasicService(am.starting, am.run, am.stopping)

	return am, nil
}

func (am *MultitenantAlertmanager) starting(ctx context.Context) (err error) {
	defer func() {
		if err == nil || am.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), am.subservices); stopErr != nil {
			level.Error(am.logger).Log("msg", "failed to gracefully stop alertmanager dependencies", "err", stopErr)
		}
	}()

	if am.cfg.ShardingEnabled {
		if am.subservices, err = services.NewManager(am.ringLifecycler, am.ring); err != nil {
			return errors.Wrap(err, "failed to start alertmanager's subservices")
		}

		if err = services.StartManagerAndAwaitHealthy(ctx, am.subservices); err != nil {
			return errors.Wrap(err, "failed to start alertmanager's subservices")
		}

		am.subservicesWatcher = services.NewFailureWatcher()
		am.subservicesWatcher.WatchManager(am.subservices)

		// We wait until the instance is in the JOINING state, once it does we know that tokens are assigned to this instance and we'll be ready to perform an initial sync of configs.
		level.Info(am.logger).Log("waiting until alertmanager is JOINING in the ring")
		if err = ring.WaitInstanceState(ctx, am.ring, am.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
			return err
		}
		level.Info(am.logger).Log("msg", "alertmanager is JOINING in the ring")
	}

	// At this point, if sharding is enabled, the instance is registered with some tokens
	// and we can run the initial iteration to sync configs. If no sharding is enabled we load _all_ the configs.
	if err := am.loadAndSyncConfigs(ctx, reasonInitial); err != nil {
		return err
	}

	if am.cfg.ShardingEnabled {
		// With the initial sync now completed, we should have loaded all assigned alertmanager configurations to this instance. We can switch it to ACTIVE and start serving requests.
		if err := am.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
			return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
		}

		// Wait until the ring client detected this instance in the ACTIVE state.
		level.Info(am.logger).Log("msg", "waiting until alertmanager is ACTIVE in the ring")
		if err := ring.WaitInstanceState(ctx, am.ring, am.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
			return err
		}
		level.Info(am.logger).Log("msg", "alertmanager is ACTIVE in the ring")
	}

	return nil
}

func (am *MultitenantAlertmanager) run(ctx context.Context) error {
	tick := time.NewTicker(am.cfg.PollInterval)
	defer tick.Stop()

	var ringTickerChan <-chan time.Time
	var ringLastState ring.ReplicationSet

	if am.cfg.ShardingEnabled {
		ringLastState, _ = am.ring.GetAllHealthy(RingOp)
		ringTicker := time.NewTicker(util.DurationWithJitter(am.cfg.ShardingRing.RingCheckPeriod, 0.2))
		defer ringTicker.Stop()
		ringTickerChan = ringTicker.C
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-am.subservicesWatcher.Chan():
			return errors.Wrap(err, "alertmanager subservices failed")
		case <-tick.C:
			// We don't want to halt execution here but instead just log what happened.
			if err := am.loadAndSyncConfigs(ctx, reasonPeriodic); err != nil {
				level.Warn(am.logger).Log("msg", "error while synchronizing alertmanager configs", "err", err)
			}
		case <-ringTickerChan:
			// We ignore the error because in case of error it will return an empty
			// replication set which we use to compare with the previous state.
			currRingState, _ := am.ring.GetAllHealthy(RingOp)

			if ring.HasReplicationSetChanged(ringLastState, currRingState) {
				ringLastState = currRingState
				if err := am.loadAndSyncConfigs(ctx, reasonRingChange); err != nil {
					level.Warn(am.logger).Log("msg", "error while synchronizing alertmanager configs", "err", err)
				}
			}
		}
	}
}

func (am *MultitenantAlertmanager) loadAndSyncConfigs(ctx context.Context, syncReason string) error {
	level.Info(am.logger).Log("msg", "synchronizing alertmanager configs for users")
	am.syncTotal.WithLabelValues(syncReason).Inc()

	cfgs, err := am.loadAlertmanagerConfigs(ctx)
	if err != nil {
		am.syncFailures.WithLabelValues(syncReason).Inc()
		return err
	}

	am.syncConfigs(cfgs)
	return nil
}

// stopping runs when MultitenantAlertmanager transitions to Stopping state.
func (am *MultitenantAlertmanager) stopping(_ error) error {
	am.alertmanagersMtx.Lock()
	for _, am := range am.alertmanagers {
		am.StopAndWait()
	}
	am.alertmanagersMtx.Unlock()
	if am.peer != nil { // Tests don't setup any peer.
		err := am.peer.Leave(am.cfg.Cluster.PeerTimeout)
		if err != nil {
			level.Warn(am.logger).Log("msg", "failed to leave the cluster", "err", err)
		}
	}

	if am.subservices != nil {
		// subservices manages ring and lifecycler, if sharding was enabled.
		_ = services.StopManagerAndAwaitStopped(context.Background(), am.subservices)
	}
	return nil
}

// loadAlertmanagerConfigs Loads (and filters) the alertmanagers configuration from object storage, taking into consideration the sharding strategy.
func (am *MultitenantAlertmanager) loadAlertmanagerConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error) {
	configs, err := am.store.ListAlertConfigs(ctx)
	if err != nil {
		return nil, err
	}

	// Without any sharding, we return _all_ the configs and there's nothing else for us to do.
	if !am.cfg.ShardingEnabled {
		am.tenantsDiscovered.Set(float64(len(configs)))
		am.tenantsOwned.Set(float64(len(configs)))
		return configs, nil
	}

	ownedConfigs := map[string]alerts.AlertConfigDesc{}
	for userID, cfg := range configs {
		owned, err := am.isConfigOwned(userID)
		if err != nil {
			am.ringCheckErrors.Inc()
			level.Error(am.logger).Log("msg", "failed to load alertmanager configuration for user", "user", userID, "err", err)
			continue
		}

		if owned {
			level.Debug(am.logger).Log("msg", "alertmanager configuration owned", "user", userID)
			ownedConfigs[userID] = cfg
		} else {
			level.Debug(am.logger).Log("msg", "alertmanager configuration not owned, ignoring", "user", userID)
		}
	}

	am.tenantsDiscovered.Set(float64(len(configs)))
	am.tenantsOwned.Set(float64(len(ownedConfigs)))
	return ownedConfigs, nil
}

func (am *MultitenantAlertmanager) isConfigOwned(userID string) (bool, error) {
	ringHasher := fnv.New32a()
	// Hasher never returns err.
	_, _ = ringHasher.Write([]byte(userID))

	alertmanagers, err := am.ring.Get(ringHasher.Sum32(), RingOp, nil, nil, nil)
	if err != nil {
		return false, errors.Wrap(err, "error reading ring to verify config ownership")
	}

	return alertmanagers.Includes(am.ringLifecycler.GetInstanceAddr()), nil
}

func (am *MultitenantAlertmanager) syncConfigs(cfgs map[string]alerts.AlertConfigDesc) {
	level.Debug(am.logger).Log("msg", "adding configurations", "num_configs", len(cfgs))
	for user, cfg := range cfgs {
		err := am.setConfig(cfg)
		if err != nil {
			am.multitenantMetrics.lastReloadSuccessful.WithLabelValues(user).Set(float64(0))
			level.Warn(am.logger).Log("msg", "error applying config", "err", err)
			continue
		}

		am.multitenantMetrics.lastReloadSuccessful.WithLabelValues(user).Set(float64(1))
		am.multitenantMetrics.lastReloadSuccessfulTimestamp.WithLabelValues(user).SetToCurrentTime()
	}

	am.alertmanagersMtx.Lock()
	defer am.alertmanagersMtx.Unlock()
	for userID, userAM := range am.alertmanagers {
		if _, exists := cfgs[userID]; !exists {
			level.Info(am.logger).Log("msg", "deactivating per-tenant alertmanager", "user", userID)
			userAM.Stop()
			delete(am.alertmanagers, userID)
			delete(am.cfgs, userID)
			am.multitenantMetrics.lastReloadSuccessful.DeleteLabelValues(userID)
			am.multitenantMetrics.lastReloadSuccessfulTimestamp.DeleteLabelValues(userID)
			am.alertmanagerMetrics.removeUserRegistry(userID)
			level.Info(am.logger).Log("msg", "deactivated per-tenant alertmanager", "user", userID)
		}
	}
}

// setConfig applies the given configuration to the alertmanager for `userID`,
// creating an alertmanager if it doesn't already exist.
func (am *MultitenantAlertmanager) setConfig(cfg alerts.AlertConfigDesc) error {
	var userAmConfig *amconfig.Config
	var err error
	var hasTemplateChanges bool

	for _, tmpl := range cfg.Templates {
		hasChanged, err := createTemplateFile(am.cfg.DataDir, cfg.User, tmpl.Filename, tmpl.Body)
		if err != nil {
			return err
		}

		if hasChanged {
			hasTemplateChanges = true
		}
	}

	level.Debug(am.logger).Log("msg", "setting config", "user", cfg.User)

	am.alertmanagersMtx.Lock()
	defer am.alertmanagersMtx.Unlock()
	existing, hasExisting := am.alertmanagers[cfg.User]

	rawCfg := cfg.RawConfig
	if cfg.RawConfig == "" {
		if am.fallbackConfig == "" {
			return fmt.Errorf("blank Alertmanager configuration for %v", cfg.User)
		}
		level.Debug(am.logger).Log("msg", "blank Alertmanager configuration; using fallback", "user", cfg.User)
		userAmConfig, err = amconfig.Load(am.fallbackConfig)
		if err != nil {
			return fmt.Errorf("unable to load fallback configuration for %v: %v", cfg.User, err)
		}
		rawCfg = am.fallbackConfig
	} else {
		userAmConfig, err = amconfig.Load(cfg.RawConfig)
		if err != nil && hasExisting {
			// This means that if a user has a working config and
			// they submit a broken one, the Manager will keep running the last known
			// working configuration.
			return fmt.Errorf("invalid Cortex configuration for %v: %v", cfg.User, err)
		}
	}

	// We can have an empty configuration here if:
	// 1) the user had a previous alertmanager
	// 2) then, submitted a non-working configuration (and we kept running the prev working config)
	// 3) finally, the cortex AM instance is restarted and the running version is no longer present
	if userAmConfig == nil {
		return fmt.Errorf("no usable Alertmanager configuration for %v", cfg.User)
	}

	// Transform webhook configs URLs to the per tenant monitor
	if am.cfg.AutoWebhookRoot != "" {
		for i, r := range userAmConfig.Receivers {
			for j, w := range r.WebhookConfigs {
				if w.URL.String() == autoWebhookURL {
					u, err := url.Parse(am.cfg.AutoWebhookRoot + "/" + cfg.User + "/monitor")
					if err != nil {
						return err
					}

					userAmConfig.Receivers[i].WebhookConfigs[j].URL = &amconfig.URL{URL: u}
				}
			}
		}
	}

	// If no Alertmanager instance exists for this user yet, start one.
	if !hasExisting {
		level.Debug(am.logger).Log("msg", "initializing new per-tenant alertmanager", "user", cfg.User)
		newAM, err := am.newAlertmanager(cfg.User, userAmConfig, rawCfg)
		if err != nil {
			return err
		}
		am.alertmanagers[cfg.User] = newAM
	} else if am.cfgs[cfg.User].RawConfig != cfg.RawConfig || hasTemplateChanges {
		level.Info(am.logger).Log("msg", "updating new per-tenant alertmanager", "user", cfg.User)
		// If the config changed, apply the new one.
		err := existing.ApplyConfig(cfg.User, userAmConfig, rawCfg)
		if err != nil {
			return fmt.Errorf("unable to apply Alertmanager config for user %v: %v", cfg.User, err)
		}
	}
	am.cfgs[cfg.User] = cfg
	return nil
}

func (am *MultitenantAlertmanager) newAlertmanager(userID string, amConfig *amconfig.Config, rawCfg string) (*Alertmanager, error) {
	reg := prometheus.NewRegistry()
	newAM, err := New(&Config{
		UserID:      userID,
		DataDir:     am.cfg.DataDir,
		Logger:      util_log.Logger,
		Peer:        am.peer,
		PeerTimeout: am.cfg.Cluster.PeerTimeout,
		Retention:   am.cfg.Retention,
		ExternalURL: am.cfg.ExternalURL.URL,
	}, reg)
	if err != nil {
		return nil, fmt.Errorf("unable to start Alertmanager for user %v: %v", userID, err)
	}

	if err := newAM.ApplyConfig(userID, amConfig, rawCfg); err != nil {
		return nil, fmt.Errorf("unable to apply initial config for user %v: %v", userID, err)
	}

	am.alertmanagerMetrics.addUserRegistry(userID, reg)
	return newAM, nil
}

// ServeHTTP serves the Alertmanager's web UI and API.
func (am *MultitenantAlertmanager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if am.State() != services.Running {
		http.Error(w, "Alertmanager not ready", http.StatusServiceUnavailable)
		return
	}

	userID, err := tenant.TenantID(req.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	am.alertmanagersMtx.Lock()
	userAM, ok := am.alertmanagers[userID]
	am.alertmanagersMtx.Unlock()

	if ok {

		userAM.mux.ServeHTTP(w, req)
		return
	}

	if am.fallbackConfig != "" {
		userAM, err = am.alertmanagerFromFallbackConfig(userID)
		if err != nil {
			level.Error(am.logger).Log("msg", "unable to initialize the Alertmanager with a fallback configuration", "user", userID, "err", err)
			http.Error(w, "Failed to initialize the Alertmanager", http.StatusInternalServerError)
			return
		}

		userAM.mux.ServeHTTP(w, req)
		return
	}

	level.Debug(am.logger).Log("msg", "the Alertmanager has no configuration and no fallback specified", "user", userID)
	http.Error(w, "the Alertmanager is not configured", http.StatusNotFound)
}

func (am *MultitenantAlertmanager) alertmanagerFromFallbackConfig(userID string) (*Alertmanager, error) {
	// Upload an empty config so that the Alertmanager is no de-activated in the next poll
	cfgDesc := alerts.ToProto("", nil, userID)
	err := am.store.SetAlertConfig(context.Background(), cfgDesc)
	if err != nil {
		return nil, err
	}

	// Calling setConfig with an empty configuration will use the fallback config.
	err = am.setConfig(cfgDesc)
	if err != nil {
		return nil, err
	}

	am.alertmanagersMtx.Lock()
	defer am.alertmanagersMtx.Unlock()
	return am.alertmanagers[userID], nil
}

// GetStatusHandler returns the status handler for this multi-tenant
// alertmanager.
func (am *MultitenantAlertmanager) GetStatusHandler() StatusHandler {
	return StatusHandler{
		am: am,
	}
}

// StatusHandler shows the status of the alertmanager.
type StatusHandler struct {
	am *MultitenantAlertmanager
}

// ServeHTTP serves the status of the alertmanager.
func (s StatusHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	err := statusTemplate.Execute(w, s.am.peer.Info())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func createTemplateFile(dataDir, userID, fn, content string) (bool, error) {
	if fn != filepath.Base(fn) {
		return false, fmt.Errorf("template file name '%s' is not not valid", fn)
	}

	dir := filepath.Join(dataDir, "templates", userID, filepath.Dir(fn))
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return false, fmt.Errorf("unable to create Alertmanager templates directory %q: %s", dir, err)
	}

	file := filepath.Join(dir, fn)
	// Check if the template file already exists and if it has changed
	if tmpl, err := ioutil.ReadFile(file); err == nil && string(tmpl) == content {
		return false, nil
	}

	if err := ioutil.WriteFile(file, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("unable to create Alertmanager template file %q: %s", file, err)
	}

	return true, nil
}
