package alertmanager

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/alertmanager/cluster"
	"github.com/prometheus/alertmanager/cluster/clusterpb"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/user"
	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/alertmanager/alertmanagerpb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alertspb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alertstore"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
	"github.com/cortexproject/cortex/pkg/util/flagext"
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

	errInvalidExternalURL                  = errors.New("the configured external URL is invalid: should not end with /")
	errShardingLegacyStorage               = errors.New("deprecated -alertmanager.storage.* not supported with -alertmanager.sharding-enabled, use -alertmanager-storage.*")
	errShardingUnsupportedStorage          = errors.New("the configured alertmanager storage backend is not supported when sharding is enabled")
	errZoneAwarenessEnabledWithoutZoneInfo = errors.New("the configured alertmanager has zone awareness enabled but zone is not set")
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
	DataDir        string           `yaml:"data_dir"`
	Retention      time.Duration    `yaml:"retention"`
	ExternalURL    flagext.URLValue `yaml:"external_url"`
	PollInterval   time.Duration    `yaml:"poll_interval"`
	MaxRecvMsgSize int64            `yaml:"max_recv_msg_size"`

	// Enable sharding for the Alertmanager
	ShardingEnabled bool       `yaml:"sharding_enabled"`
	ShardingRing    RingConfig `yaml:"sharding_ring"`

	FallbackConfigFile string `yaml:"fallback_config_file"`
	AutoWebhookRoot    string `yaml:"auto_webhook_root"`

	Store   alertstore.LegacyConfig `yaml:"storage" doc:"description=Deprecated. Use -alertmanager-storage.* CLI flags and their respective YAML config options instead."`
	Cluster ClusterConfig           `yaml:"cluster"`

	EnableAPI bool `yaml:"enable_api"`

	// For distributor.
	AlertmanagerClient ClientConfig `yaml:"alertmanager_client"`

	// For the state persister.
	Persister PersisterConfig `yaml:",inline"`
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
	f.Int64Var(&cfg.MaxRecvMsgSize, "alertmanager.max-recv-msg-size", 16<<20, "Maximum size (bytes) of an accepted HTTP request body.")

	f.Var(&cfg.ExternalURL, "alertmanager.web.external-url", "The URL under which Alertmanager is externally reachable (for example, if Alertmanager is served via a reverse proxy). Used for generating relative and absolute links back to Alertmanager itself. If the URL has a path portion, it will be used to prefix all HTTP endpoints served by Alertmanager. If omitted, relevant URL components will be derived automatically.")

	f.StringVar(&cfg.FallbackConfigFile, "alertmanager.configs.fallback", "", "Filename of fallback config to use if none specified for instance.")
	f.StringVar(&cfg.AutoWebhookRoot, "alertmanager.configs.auto-webhook-root", "", "Root of URL to generate if config is "+autoWebhookURL)
	f.DurationVar(&cfg.PollInterval, "alertmanager.configs.poll-interval", 15*time.Second, "How frequently to poll Cortex configs")

	f.BoolVar(&cfg.EnableAPI, "experimental.alertmanager.enable-api", false, "Enable the experimental alertmanager config api.")

	f.BoolVar(&cfg.ShardingEnabled, "alertmanager.sharding-enabled", false, "Shard tenants across multiple alertmanager instances.")

	cfg.AlertmanagerClient.RegisterFlagsWithPrefix("alertmanager.alertmanager-client", f)
	cfg.Persister.RegisterFlagsWithPrefix("alertmanager", f)
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

// Validate config and returns error on failure
func (cfg *MultitenantAlertmanagerConfig) Validate(storageCfg alertstore.Config) error {
	if cfg.ExternalURL.URL != nil && strings.HasSuffix(cfg.ExternalURL.Path, "/") {
		return errInvalidExternalURL
	}

	if err := cfg.Store.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}

	if err := cfg.Persister.Validate(); err != nil {
		return err
	}

	if cfg.ShardingEnabled {
		if !cfg.Store.IsDefaults() {
			return errShardingLegacyStorage
		}
		if !storageCfg.IsFullStateSupported() {
			return errShardingUnsupportedStorage
		}
		if cfg.ShardingRing.ZoneAwarenessEnabled && cfg.ShardingRing.InstanceZone == "" {
			return errZoneAwarenessEnabledWithoutZoneInfo
		}
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

// Limits defines limits used by Alertmanager.
type Limits interface {
	// AlertmanagerReceiversBlockCIDRNetworks returns the list of network CIDRs that should be blocked
	// in the Alertmanager receivers for the given user.
	AlertmanagerReceiversBlockCIDRNetworks(user string) []flagext.CIDR

	// AlertmanagerReceiversBlockPrivateAddresses returns true if private addresses should be blocked
	// in the Alertmanager receivers for the given user.
	AlertmanagerReceiversBlockPrivateAddresses(user string) bool

	// NotificationRateLimit methods return limit used by rate-limiter for given integration.
	// If set to 0, no notifications are allowed.
	// rate.Inf = all notifications are allowed.
	//
	// Note that when negative or zero values specified by user are translated to rate.Limit by Overrides,
	// and may have different meaning there.
	NotificationRateLimit(tenant string, integration string) rate.Limit

	// NotificationBurstSize returns burst-size for rate limiter for given integration type. If 0, no notifications are allowed except
	// when limit == rate.Inf.
	NotificationBurstSize(tenant string, integration string) int

	// AlertmanagerMaxConfigSize returns max size of configuration file that user is allowed to upload. If 0, there is no limit.
	AlertmanagerMaxConfigSize(tenant string) int

	// AlertmanagerMaxTemplatesCount returns max number of templates that tenant can use in the configuration. 0 = no limit.
	AlertmanagerMaxTemplatesCount(tenant string) int

	// AlertmanagerMaxTemplateSize returns max size of individual template. 0 = no limit.
	AlertmanagerMaxTemplateSize(tenant string) int
}

// A MultitenantAlertmanager manages Alertmanager instances for multiple
// organizations.
type MultitenantAlertmanager struct {
	services.Service

	cfg *MultitenantAlertmanagerConfig

	// Ring used for sharding alertmanager instances.
	// When sharding is disabled, the flow is:
	//   ServeHTTP() -> serveRequest()
	// When sharding is enabled:
	//   ServeHTTP() -> distributor.DistributeRequest() -> (sends to other AM or even the current)
	//     -> HandleRequest() (gRPC call) -> grpcServer() -> handlerForGRPCServer.ServeHTTP() -> serveRequest().
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring
	distributor    *Distributor
	grpcServer     *server.Server

	// Subservices manager (ring, lifecycler)
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	store alertstore.AlertStore

	// The fallback config is stored as a string and parsed every time it's needed
	// because we mutate the parsed results and don't want those changes to take
	// effect here.
	fallbackConfig string

	alertmanagersMtx sync.Mutex
	alertmanagers    map[string]*Alertmanager
	// Stores the current set of configurations we're running in each tenant's Alertmanager.
	// Used for comparing configurations as we synchronize them.
	cfgs map[string]alertspb.AlertConfigDesc

	logger              log.Logger
	alertmanagerMetrics *alertmanagerMetrics
	multitenantMetrics  *multitenantAlertmanagerMetrics

	peer                    *cluster.Peer
	alertmanagerClientsPool ClientsPool

	limits Limits

	registry          prometheus.Registerer
	ringCheckErrors   prometheus.Counter
	tenantsOwned      prometheus.Gauge
	tenantsDiscovered prometheus.Gauge
	syncTotal         *prometheus.CounterVec
	syncFailures      *prometheus.CounterVec
}

// NewMultitenantAlertmanager creates a new MultitenantAlertmanager.
func NewMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, store alertstore.AlertStore, limits Limits, logger log.Logger, registerer prometheus.Registerer) (*MultitenantAlertmanager, error) {
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

	var peer *cluster.Peer
	// We need to take this case into account to support our legacy upstream clustering.
	if cfg.Cluster.ListenAddr != "" && !cfg.ShardingEnabled {
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

	return createMultitenantAlertmanager(cfg, fallbackConfig, peer, store, ringStore, limits, logger, registerer)
}

func createMultitenantAlertmanager(cfg *MultitenantAlertmanagerConfig, fallbackConfig []byte, peer *cluster.Peer, store alertstore.AlertStore, ringStore kv.Client, limits Limits, logger log.Logger, registerer prometheus.Registerer) (*MultitenantAlertmanager, error) {
	am := &MultitenantAlertmanager{
		cfg:                 cfg,
		fallbackConfig:      string(fallbackConfig),
		cfgs:                map[string]alertspb.AlertConfigDesc{},
		alertmanagers:       map[string]*Alertmanager{},
		alertmanagerMetrics: newAlertmanagerMetrics(),
		multitenantMetrics:  newMultitenantAlertmanagerMetrics(registerer),
		peer:                peer,
		store:               store,
		logger:              log.With(logger, "component", "MultiTenantAlertmanager"),
		registry:            registerer,
		limits:              limits,
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

		am.grpcServer = server.NewServer(&handlerForGRPCServer{am: am})

		am.alertmanagerClientsPool = newAlertmanagerClientsPool(client.NewRingServiceDiscovery(am.ring), cfg.AlertmanagerClient, logger, am.registry)
		am.distributor, err = NewDistributor(cfg.AlertmanagerClient, cfg.MaxRecvMsgSize, am.ring, am.alertmanagerClientsPool, log.With(logger, "component", "AlertmanagerDistributor"), am.registry)
		if err != nil {
			return nil, errors.Wrap(err, "create distributor")
		}
	}

	if registerer != nil {
		registerer.MustRegister(am.alertmanagerMetrics)
	}

	am.Service = services.NewBasicService(am.starting, am.run, am.stopping)

	return am, nil
}

// handlerForGRPCServer acts as a handler for gRPC server to serve
// the serveRequest() via the standard ServeHTTP.
type handlerForGRPCServer struct {
	am *MultitenantAlertmanager
}

func (h *handlerForGRPCServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.am.serveRequest(w, req)
}

func (am *MultitenantAlertmanager) starting(ctx context.Context) (err error) {
	err = am.migrateStateFilesToPerTenantDirectories()
	if err != nil {
		return err
	}

	defer func() {
		if err == nil || am.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), am.subservices); stopErr != nil {
			level.Error(am.logger).Log("msg", "failed to gracefully stop alertmanager dependencies", "err", stopErr)
		}
	}()

	if am.cfg.ShardingEnabled {
		if am.subservices, err = services.NewManager(am.ringLifecycler, am.ring, am.distributor); err != nil {
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
		// Make sure that all the alertmanagers we were initially configured with have
		// fetched state from the replicas, before advertising as ACTIVE. This will
		// reduce the possibility that we lose state when new instances join/leave.
		level.Info(am.logger).Log("msg", "waiting until initial state sync is complete for all users")
		if err := am.waitInitialStateSync(ctx); err != nil {
			return errors.Wrap(err, "failed to wait for initial state sync")
		}
		level.Info(am.logger).Log("msg", "initial state sync is complete")

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

// migrateStateFilesToPerTenantDirectories migrates any existing configuration from old place to new hierarchy.
// TODO: Remove in Cortex 1.11.
func (am *MultitenantAlertmanager) migrateStateFilesToPerTenantDirectories() error {
	migrate := func(from, to string) error {
		level.Info(am.logger).Log("msg", "migrating alertmanager state", "from", from, "to", to)
		err := os.Rename(from, to)
		return errors.Wrapf(err, "failed to migrate alertmanager state from %v to %v", from, to)
	}

	st, err := am.getObsoleteFilesPerUser()
	if err != nil {
		return errors.Wrap(err, "failed to migrate alertmanager state files")
	}

	for userID, files := range st {
		tenantDir := am.getTenantDirectory(userID)
		err := os.MkdirAll(tenantDir, 0777)
		if err != nil {
			return errors.Wrapf(err, "failed to create per-tenant directory %v", tenantDir)
		}

		errs := tsdb_errors.NewMulti()

		if files.notificationLogSnapshot != "" {
			errs.Add(migrate(files.notificationLogSnapshot, filepath.Join(tenantDir, notificationLogSnapshot)))
		}

		if files.silencesSnapshot != "" {
			errs.Add(migrate(files.silencesSnapshot, filepath.Join(tenantDir, silencesSnapshot)))
		}

		if files.templatesDir != "" {
			errs.Add(migrate(files.templatesDir, filepath.Join(tenantDir, templatesDir)))
		}

		if err := errs.Err(); err != nil {
			return err
		}
	}
	return nil
}

type obsoleteStateFiles struct {
	notificationLogSnapshot string
	silencesSnapshot        string
	templatesDir            string
}

// getObsoleteFilesPerUser returns per-user set of files that should be migrated from old structure to new structure.
func (am *MultitenantAlertmanager) getObsoleteFilesPerUser() (map[string]obsoleteStateFiles, error) {
	files, err := ioutil.ReadDir(am.cfg.DataDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list dir %v", am.cfg.DataDir)
	}

	// old names
	const (
		notificationLogPrefix = "nflog:"
		silencesPrefix        = "silences:"
		templates             = "templates"
	)

	result := map[string]obsoleteStateFiles{}

	for _, f := range files {
		fullPath := filepath.Join(am.cfg.DataDir, f.Name())

		if f.IsDir() {
			// Process templates dir.
			if f.Name() != templates {
				// Ignore other files -- those are likely per tenant directories.
				continue
			}

			templateDirs, err := ioutil.ReadDir(fullPath)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to list dir %v", fullPath)
			}

			// Previously templates directory contained per-tenant subdirectory.
			for _, d := range templateDirs {
				if d.IsDir() {
					v := result[d.Name()]
					v.templatesDir = filepath.Join(fullPath, d.Name())
					result[d.Name()] = v
				} else {
					level.Warn(am.logger).Log("msg", "ignoring unknown local file while migrating local alertmanager state files", "file", filepath.Join(fullPath, d.Name()))
				}
			}
			continue
		}

		switch {
		case strings.HasPrefix(f.Name(), notificationLogPrefix):
			userID := strings.TrimPrefix(f.Name(), notificationLogPrefix)
			v := result[userID]
			v.notificationLogSnapshot = fullPath
			result[userID] = v

		case strings.HasPrefix(f.Name(), silencesPrefix):
			userID := strings.TrimPrefix(f.Name(), silencesPrefix)
			v := result[userID]
			v.silencesSnapshot = fullPath
			result[userID] = v

		default:
			level.Warn(am.logger).Log("msg", "ignoring unknown local data file while migrating local alertmanager state files", "file", fullPath)
		}
	}

	return result, nil
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

	allUsers, cfgs, err := am.loadAlertmanagerConfigs(ctx)
	if err != nil {
		am.syncFailures.WithLabelValues(syncReason).Inc()
		return err
	}

	am.syncConfigs(cfgs)
	am.deleteUnusedLocalUserState()

	// Currently, remote state persistence is only used when sharding is enabled.
	if am.cfg.ShardingEnabled {
		// Note when cleaning up remote state, remember that the user may not necessarily be configured
		// in this instance. Therefore, pass the list of _all_ configured users to filter by.
		am.deleteUnusedRemoteUserState(ctx, allUsers)
	}

	return nil
}

func (am *MultitenantAlertmanager) waitInitialStateSync(ctx context.Context) error {
	am.alertmanagersMtx.Lock()
	ams := make([]*Alertmanager, 0, len(am.alertmanagers))
	for _, userAM := range am.alertmanagers {
		ams = append(ams, userAM)
	}
	am.alertmanagersMtx.Unlock()

	for _, userAM := range ams {
		if err := userAM.WaitInitialStateSync(ctx); err != nil {
			return err
		}
	}

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

// loadAlertmanagerConfigs Loads (and filters) the alertmanagers configuration from object storage, taking into consideration the sharding strategy. Returns:
// - The list of discovered users (all users with a configuration in storage)
// - The configurations of users owned by this instance.
func (am *MultitenantAlertmanager) loadAlertmanagerConfigs(ctx context.Context) ([]string, map[string]alertspb.AlertConfigDesc, error) {
	// Find all users with an alertmanager config.
	allUserIDs, err := am.store.ListAllUsers(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to list users with alertmanager configuration")
	}
	numUsersDiscovered := len(allUserIDs)
	ownedUserIDs := make([]string, 0, len(allUserIDs))

	// Filter out users not owned by this shard.
	for _, userID := range allUserIDs {
		if am.isUserOwned(userID) {
			ownedUserIDs = append(ownedUserIDs, userID)
		}
	}
	numUsersOwned := len(ownedUserIDs)

	// Load the configs for the owned users.
	configs, err := am.store.GetAlertConfigs(ctx, ownedUserIDs)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to load alertmanager configurations for owned users")
	}

	am.tenantsDiscovered.Set(float64(numUsersDiscovered))
	am.tenantsOwned.Set(float64(numUsersOwned))
	return allUserIDs, configs, nil
}

func (am *MultitenantAlertmanager) isUserOwned(userID string) bool {
	// If sharding is disabled, any alertmanager instance owns all users.
	if !am.cfg.ShardingEnabled {
		return true
	}

	alertmanagers, err := am.ring.Get(shardByUser(userID), SyncRingOp, nil, nil, nil)
	if err != nil {
		am.ringCheckErrors.Inc()
		level.Error(am.logger).Log("msg", "failed to load alertmanager configuration", "user", userID, "err", err)
		return false
	}

	return alertmanagers.Includes(am.ringLifecycler.GetInstanceAddr())
}

func (am *MultitenantAlertmanager) syncConfigs(cfgs map[string]alertspb.AlertConfigDesc) {
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

	userAlertmanagersToStop := map[string]*Alertmanager{}

	am.alertmanagersMtx.Lock()
	for userID, userAM := range am.alertmanagers {
		if _, exists := cfgs[userID]; !exists {
			userAlertmanagersToStop[userID] = userAM
			delete(am.alertmanagers, userID)
			delete(am.cfgs, userID)
			am.multitenantMetrics.lastReloadSuccessful.DeleteLabelValues(userID)
			am.multitenantMetrics.lastReloadSuccessfulTimestamp.DeleteLabelValues(userID)
			am.alertmanagerMetrics.removeUserRegistry(userID)
		}
	}
	am.alertmanagersMtx.Unlock()

	// Now stop alertmanagers and wait until they are really stopped, without holding lock.
	for userID, userAM := range userAlertmanagersToStop {
		level.Info(am.logger).Log("msg", "deactivating per-tenant alertmanager", "user", userID)
		userAM.StopAndWait()
		level.Info(am.logger).Log("msg", "deactivated per-tenant alertmanager", "user", userID)
	}
}

// setConfig applies the given configuration to the alertmanager for `userID`,
// creating an alertmanager if it doesn't already exist.
func (am *MultitenantAlertmanager) setConfig(cfg alertspb.AlertConfigDesc) error {
	var userAmConfig *amconfig.Config
	var err error
	var hasTemplateChanges bool

	for _, tmpl := range cfg.Templates {
		templateFilepath, err := safeTemplateFilepath(filepath.Join(am.getTenantDirectory(cfg.User), templatesDir), tmpl.Filename)
		if err != nil {
			return err
		}

		hasChanged, err := storeTemplateFile(templateFilepath, tmpl.Body)
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

func (am *MultitenantAlertmanager) getTenantDirectory(userID string) string {
	return filepath.Join(am.cfg.DataDir, userID)
}

func (am *MultitenantAlertmanager) newAlertmanager(userID string, amConfig *amconfig.Config, rawCfg string) (*Alertmanager, error) {
	reg := prometheus.NewRegistry()

	tenantDir := am.getTenantDirectory(userID)
	err := os.MkdirAll(tenantDir, 0777)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create per-tenant directory %v", tenantDir)
	}

	newAM, err := New(&Config{
		UserID:            userID,
		TenantDataDir:     tenantDir,
		Logger:            am.logger,
		Peer:              am.peer,
		PeerTimeout:       am.cfg.Cluster.PeerTimeout,
		Retention:         am.cfg.Retention,
		ExternalURL:       am.cfg.ExternalURL.URL,
		ShardingEnabled:   am.cfg.ShardingEnabled,
		Replicator:        am,
		ReplicationFactor: am.cfg.ShardingRing.ReplicationFactor,
		Store:             am.store,
		PersisterConfig:   am.cfg.Persister,
		Limits:            am.limits,
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

// GetPositionForUser returns the position this Alertmanager instance holds in the ring related to its other replicas for an specific user.
func (am *MultitenantAlertmanager) GetPositionForUser(userID string) int {
	// If we have a replication factor of 1 or less we don't need to do any work and can immediately return.
	if am.ring == nil || am.ring.ReplicationFactor() <= 1 {
		return 0
	}

	set, err := am.ring.Get(shardByUser(userID), RingOp, nil, nil, nil)
	if err != nil {
		level.Error(am.logger).Log("msg", "unable to read the ring while trying to determine the alertmanager position", "err", err)
		// If we're  unable to determine the position, we don't want a tenant to miss out on the notification - instead,
		// just assume we're the first in line and run the risk of a double notification.
		return 0
	}

	var position int
	for i, instance := range set.Instances {
		if instance.Addr == am.ringLifecycler.GetInstanceAddr() {
			position = i
			break
		}
	}

	return position
}

// ServeHTTP serves the Alertmanager's web UI and API.
func (am *MultitenantAlertmanager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if am.State() != services.Running {
		http.Error(w, "Alertmanager not ready", http.StatusServiceUnavailable)
		return
	}

	if am.cfg.ShardingEnabled && am.distributor.IsPathSupported(req.URL.Path) {
		am.distributor.DistributeRequest(w, req)
		return
	}

	// If sharding is not enabled or Distributor does not support this path,
	// it is served by this instance.
	am.serveRequest(w, req)
}

// HandleRequest implements gRPC Alertmanager service, which receives request from AlertManager-Distributor.
func (am *MultitenantAlertmanager) HandleRequest(ctx context.Context, in *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error) {
	return am.grpcServer.Handle(ctx, in)
}

// serveRequest serves the Alertmanager's web UI and API.
func (am *MultitenantAlertmanager) serveRequest(w http.ResponseWriter, req *http.Request) {
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
	cfgDesc := alertspb.ToProto("", nil, userID)
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

// ReplicateStateForUser attempts to replicate a partial state sent by an alertmanager to its other replicas through the ring.
func (am *MultitenantAlertmanager) ReplicateStateForUser(ctx context.Context, userID string, part *clusterpb.Part) error {
	level.Debug(am.logger).Log("msg", "message received for replication", "user", userID, "key", part.Key)

	selfAddress := am.ringLifecycler.GetInstanceAddr()
	err := ring.DoBatch(ctx, RingOp, am.ring, []uint32{shardByUser(userID)}, func(desc ring.InstanceDesc, _ []int) error {
		if desc.GetAddr() == selfAddress {
			return nil
		}

		c, err := am.alertmanagerClientsPool.GetClientFor(desc.GetAddr())
		if err != nil {
			return err
		}

		resp, err := c.UpdateState(user.InjectOrgID(ctx, userID), part)
		if err != nil {
			return err
		}

		switch resp.Status {
		case alertmanagerpb.MERGE_ERROR:
			level.Error(am.logger).Log("msg", "state replication failed", "user", userID, "key", part.Key, "err", resp.Error)
		case alertmanagerpb.USER_NOT_FOUND:
			level.Debug(am.logger).Log("msg", "user not found while trying to replicate state", "user", userID, "key", part.Key)
		}
		return nil
	}, func() {})

	return err
}

// ReadFullStateForUser attempts to read the full state from each replica for user. Note that it will try to obtain and return
// state from all replicas, but will consider it a success if state is obtained from at least one replica.
func (am *MultitenantAlertmanager) ReadFullStateForUser(ctx context.Context, userID string) ([]*clusterpb.FullState, error) {

	// Only get the set of replicas which contain the specified user.
	key := shardByUser(userID)
	replicationSet, err := am.ring.Get(key, RingOp, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// We should only query state from other replicas, and not our own state.
	addrs := replicationSet.GetAddressesWithout(am.ringLifecycler.GetInstanceAddr())

	var (
		resultsMtx sync.Mutex
		results    []*clusterpb.FullState
	)

	// Note that the jobs swallow the errors - this is because we want to give each replica a chance to respond.
	jobs := concurrency.CreateJobsFromStrings(addrs)
	err = concurrency.ForEach(ctx, jobs, len(jobs), func(ctx context.Context, job interface{}) error {
		addr := job.(string)
		level.Debug(am.logger).Log("msg", "contacting replica for full state", "user", userID, "addr", addr)

		c, err := am.alertmanagerClientsPool.GetClientFor(addr)
		if err != nil {
			level.Error(am.logger).Log("msg", "failed to get rpc client", "err", err)
			return nil
		}

		resp, err := c.ReadState(user.InjectOrgID(ctx, userID), &alertmanagerpb.ReadStateRequest{})
		if err != nil {
			level.Error(am.logger).Log("msg", "rpc reading state from replica failed", "addr", addr, "user", userID, "err", err)
			return nil
		}

		switch resp.Status {
		case alertmanagerpb.READ_OK:
			resultsMtx.Lock()
			results = append(results, resp.State)
			resultsMtx.Unlock()
		case alertmanagerpb.READ_ERROR:
			level.Error(am.logger).Log("msg", "error trying to read state", "addr", addr, "user", userID, "err", resp.Error)
		case alertmanagerpb.READ_USER_NOT_FOUND:
			level.Debug(am.logger).Log("msg", "user not found while trying to read state", "addr", addr, "user", userID)
		default:
			level.Error(am.logger).Log("msg", "unknown response trying to read state", "addr", addr, "user", userID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// We only require the state from a single replica, though we return as many as we were able to obtain.
	if len(results) == 0 {
		return nil, fmt.Errorf("failed to read state from any replica")
	}

	return results, nil
}

// UpdateState implements the Alertmanager service.
func (am *MultitenantAlertmanager) UpdateState(ctx context.Context, part *clusterpb.Part) (*alertmanagerpb.UpdateStateResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	am.alertmanagersMtx.Lock()
	userAM, ok := am.alertmanagers[userID]
	am.alertmanagersMtx.Unlock()

	if !ok {
		// We can end up trying to replicate state to an alertmanager that is no longer available due to e.g. a ring topology change.
		level.Debug(am.logger).Log("msg", "user does not have an alertmanager in this instance", "user", userID)
		return &alertmanagerpb.UpdateStateResponse{
			Status: alertmanagerpb.USER_NOT_FOUND,
			Error:  "alertmanager for this user does not exists",
		}, nil
	}

	if err = userAM.mergePartialExternalState(part); err != nil {
		return &alertmanagerpb.UpdateStateResponse{
			Status: alertmanagerpb.MERGE_ERROR,
			Error:  err.Error(),
		}, nil
	}

	return &alertmanagerpb.UpdateStateResponse{Status: alertmanagerpb.OK}, nil
}

// deleteUnusedRemoteUserState deletes state objects in remote storage for users that are no longer configured.
func (am *MultitenantAlertmanager) deleteUnusedRemoteUserState(ctx context.Context, allUsers []string) {

	users := make(map[string]struct{}, len(allUsers))
	for _, userID := range allUsers {
		users[userID] = struct{}{}
	}

	usersWithState, err := am.store.ListUsersWithFullState(ctx)
	if err != nil {
		level.Warn(am.logger).Log("msg", "failed to list users with state", "err", err)
		return
	}

	for _, userID := range usersWithState {
		if _, ok := users[userID]; ok {
			continue
		}

		err := am.store.DeleteFullState(ctx, userID)
		if err != nil {
			level.Warn(am.logger).Log("msg", "failed to delete remote state for user", "user", userID, "err", err)
		} else {
			level.Info(am.logger).Log("msg", "deleted remote state for user", "user", userID)
		}
	}
}

// deleteUnusedLocalUserState deletes local files for users that we no longer need.
func (am *MultitenantAlertmanager) deleteUnusedLocalUserState() {
	userDirs := am.getPerUserDirectories()

	// And delete remaining files.
	for userID, dir := range userDirs {
		am.alertmanagersMtx.Lock()
		userAM := am.alertmanagers[userID]
		am.alertmanagersMtx.Unlock()

		// Don't delete directory if AM for user still exists.
		if userAM != nil {
			continue
		}

		err := os.RemoveAll(dir)
		if err != nil {
			level.Warn(am.logger).Log("msg", "failed to delete directory for user", "dir", dir, "user", userID, "err", err)
		} else {
			level.Info(am.logger).Log("msg", "deleted local directory for user", "dir", dir, "user", userID)
		}
	}
}

// getPerUserDirectories returns map of users to their directories (full path). Only users with local
// directory are returned.
func (am *MultitenantAlertmanager) getPerUserDirectories() map[string]string {
	files, err := ioutil.ReadDir(am.cfg.DataDir)
	if err != nil {
		level.Warn(am.logger).Log("msg", "failed to list local dir", "dir", am.cfg.DataDir, "err", err)
		return nil
	}

	result := map[string]string{}

	for _, f := range files {
		fullPath := filepath.Join(am.cfg.DataDir, f.Name())

		if !f.IsDir() {
			level.Warn(am.logger).Log("msg", "ignoring unexpected file while scanning local alertmanager configs", "file", fullPath)
			continue
		}

		result[f.Name()] = fullPath
	}
	return result
}

// UpdateState implements the Alertmanager service.
func (am *MultitenantAlertmanager) ReadState(ctx context.Context, req *alertmanagerpb.ReadStateRequest) (*alertmanagerpb.ReadStateResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	am.alertmanagersMtx.Lock()
	userAM, ok := am.alertmanagers[userID]
	am.alertmanagersMtx.Unlock()

	if !ok {
		level.Debug(am.logger).Log("msg", "user does not have an alertmanager in this instance", "user", userID)
		return &alertmanagerpb.ReadStateResponse{
			Status: alertmanagerpb.READ_USER_NOT_FOUND,
			Error:  "alertmanager for this user does not exists",
		}, nil
	}

	state, err := userAM.getFullState()
	if err != nil {
		return &alertmanagerpb.ReadStateResponse{
			Status: alertmanagerpb.READ_ERROR,
			Error:  err.Error(),
		}, nil
	}

	return &alertmanagerpb.ReadStateResponse{
		Status: alertmanagerpb.READ_OK,
		State:  state,
	}, nil
}

// StatusHandler shows the status of the alertmanager.
type StatusHandler struct {
	am *MultitenantAlertmanager
}

// ServeHTTP serves the status of the alertmanager.
func (s StatusHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	err := statusTemplate.Execute(w, s.am.peer.Info())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// validateTemplateFilename validated the template filename and returns error if it's not valid.
// The validation done in this function is a first fence to avoid having a tenant submitting
// a config which may escape the per-tenant data directory on disk.
func validateTemplateFilename(filename string) error {
	if filepath.Base(filename) != filename {
		return fmt.Errorf("invalid template name %q: the template name cannot contain any path", filename)
	}

	// Further enforce no path in the template name.
	if filepath.Dir(filepath.Clean(filename)) != "." {
		return fmt.Errorf("invalid template name %q: the template name cannot contain any path", filename)
	}

	return nil
}

// safeTemplateFilepath builds and return the template filepath within the provided dir.
// This function also performs a security check to make sure the provided templateName
// doesn't contain a relative path escaping the provided dir.
func safeTemplateFilepath(dir, templateName string) (string, error) {
	// We expect all template files to be stored and referenced within the provided directory.
	containerDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Build the actual path of the template.
	actualPath, err := filepath.Abs(filepath.Join(containerDir, templateName))
	if err != nil {
		return "", err
	}

	// Ensure the actual path of the template is within the expected directory.
	// This check is a counter-measure to make sure the tenant is not trying to
	// escape its own directory on disk.
	if !strings.HasPrefix(actualPath, containerDir) {
		return "", fmt.Errorf("invalid template name %q: the template filepath is escaping the per-tenant local directory", templateName)
	}

	return actualPath, nil
}

// storeTemplateFile stores template file at the given templateFilepath.
// Returns true, if file content has changed (new or updated file), false if file with the same name
// and content was already stored locally.
func storeTemplateFile(templateFilepath, content string) (bool, error) {
	// Make sure the directory exists.
	dir := filepath.Dir(templateFilepath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return false, fmt.Errorf("unable to create Alertmanager templates directory %q: %s", dir, err)
	}

	// Check if the template file already exists and if it has changed
	if tmpl, err := ioutil.ReadFile(templateFilepath); err == nil && string(tmpl) == content {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	if err := ioutil.WriteFile(templateFilepath, []byte(content), 0644); err != nil {
		return false, fmt.Errorf("unable to create Alertmanager template file %q: %s", templateFilepath, err)
	}

	return true, nil
}
