package base

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/notifier"
	promRules "github.com/prometheus/prometheus/rules"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	supportedShardingStrategies = []string{util.ShardingStrategyDefault, util.ShardingStrategyShuffle}
	supportedShardingAlgos      = []string{util.ShardingAlgoByGroup, util.ShardingAlgoByRule}
)

const (
	// ringKey is the key under which we store the rulers ring in the KVStore.
	ringKey = "rulers"

	// Number of concurrent group list and group loads operations.
	loadRulesConcurrency  = 10
	fetchRulesConcurrency = 16

	rulerSyncReasonInitial    = "initial"
	rulerSyncReasonPeriodic   = "periodic"
	rulerSyncReasonRingChange = "ring-change"

	// Limit errors
	errMaxRuleGroupsPerUserLimitExceeded        = "per-user rule groups limit (limit: %d actual: %d) exceeded"
	errMaxRulesPerRuleGroupPerUserLimitExceeded = "per-user rules per rule group limit (limit: %d actual: %d) exceeded"

	// errors
	errListAllUser = "unable to list the ruler users"

	// Alertmanager default values
	alertmanagerRefreshIntervalDefault           = 1 * time.Minute
	alertmanagerNotificationQueueCapacityDefault = 10000
	alertmanagerNotificationTimeoutDefault       = 10 * time.Second
)

// Config is the configuration for the recording rules server.
type Config struct {
	// This is used for template expansion in alerts; must be a valid URL.
	ExternalURL flagext.URLValue `yaml:"external_url"`
	// This is used for template expansion in alerts, and represents the corresponding Grafana datasource UID.
	DatasourceUID string `yaml:"datasource_uid"`
	// Labels to add to all alerts
	ExternalLabels labels.Labels `yaml:"external_labels,omitempty" doc:"description=Labels to add to all alerts."`
	// GRPC Client configuration.
	ClientTLSConfig grpcclient.Config `yaml:"ruler_client"`
	// How frequently to evaluate rules by default.
	EvaluationInterval time.Duration `yaml:"evaluation_interval"`
	// How frequently to poll for updated rules.
	PollInterval time.Duration `yaml:"poll_interval"`
	// Rule Storage and Polling configuration.
	StoreConfig RuleStoreConfig `yaml:"storage" doc:"deprecated|description=Use -ruler-storage. CLI flags and their respective YAML config options instead."`
	// Path to store rule files for prom manager.
	RulePath string `yaml:"rule_path"`

	// Global alertmanager config.
	config.AlertManagerConfig `yaml:",inline"`

	// Max time to tolerate outage for restoring "for" state of alert.
	OutageTolerance time.Duration `yaml:"for_outage_tolerance"`
	// Minimum duration between alert and restored "for" state. This is maintained only for alerts with configured "for" time greater than grace period.
	ForGracePeriod time.Duration `yaml:"for_grace_period"`
	// Minimum amount of time to wait before resending an alert to Alertmanager.
	ResendDelay time.Duration `yaml:"resend_delay"`

	// Enable sharding rule groups.
	EnableSharding   bool          `yaml:"enable_sharding"`
	ShardingStrategy string        `yaml:"sharding_strategy"`
	ShardingAlgo     string        `yaml:"sharding_algo"`
	SearchPendingFor time.Duration `yaml:"search_pending_for"`
	Ring             RingConfig    `yaml:"ring" doc:"description=Ring used by Loki ruler. The CLI flags prefix for this block configuration is 'ruler.ring'."`
	FlushCheckPeriod time.Duration `yaml:"flush_period"`

	EnableAPI bool `yaml:"enable_api"`

	EnabledTenants  flagext.StringSliceCSV `yaml:"enabled_tenants"`
	DisabledTenants flagext.StringSliceCSV `yaml:"disabled_tenants"`

	RingCheckPeriod time.Duration `yaml:"-"`

	EnableQueryStats      bool `yaml:"query_stats_enabled"`
	DisableRuleGroupLabel bool `yaml:"disable_rule_group_label"`
}

// Validate config and returns error on failure
func (cfg *Config) Validate(_ log.Logger) error {

	if cfg.ShardingStrategy == "" {
		cfg.ShardingStrategy = util.ShardingStrategyDefault
	}

	// whether using shuffle-sharding or not, by-group sharding algorithm is always applied by default
	if cfg.ShardingAlgo == "" {
		cfg.ShardingAlgo = util.ShardingAlgoByGroup
	}

	if !util.StringsContain(supportedShardingAlgos, cfg.ShardingAlgo) {
		return fmt.Errorf("invalid sharding algorithm %q, supported algorithms are %v", cfg.ShardingAlgo, supportedShardingAlgos)
	}

	if !util.StringsContain(supportedShardingStrategies, cfg.ShardingStrategy) {
		return fmt.Errorf("invalid sharding strategy %q, supported strategies are %v", cfg.ShardingStrategy, supportedShardingStrategies)
	}

	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientTLSConfig.RegisterFlagsWithPrefix("ruler.client", f)
	cfg.StoreConfig.RegisterFlags(f)
	cfg.Ring.RegisterFlags(f)
	cfg.Notifier.RegisterFlags(f)

	// Deprecated Flags that will be maintained to avoid user disruption

	//lint:ignore faillint Need to pass the global logger like this for warning on deprecated methods
	flagext.DeprecatedFlag(f, "ruler.client-timeout", "This flag has been renamed to ruler.configs.client-timeout", util_log.Logger)
	//lint:ignore faillint Need to pass the global logger like this for warning on deprecated methods
	flagext.DeprecatedFlag(f, "ruler.group-timeout", "This flag is no longer functional.", util_log.Logger)
	//lint:ignore faillint Need to pass the global logger like this for warning on deprecated methods
	flagext.DeprecatedFlag(f, "ruler.num-workers", "This flag is no longer functional. For increased concurrency horizontal sharding is recommended", util_log.Logger)

	cfg.ExternalURL.URL, _ = url.Parse("") // Must be non-nil
	f.Var(&cfg.ExternalURL, "ruler.external.url", "Base URL of the Grafana instance.")
	f.StringVar(&cfg.DatasourceUID, "ruler.datasource-uid", "", "Datasource UID for the dashboard.")
	f.DurationVar(&cfg.EvaluationInterval, "ruler.evaluation-interval", 1*time.Minute, "How frequently to evaluate rules.")
	f.DurationVar(&cfg.PollInterval, "ruler.poll-interval", 1*time.Minute, "How frequently to poll for rule changes.")

	f.StringVar(&cfg.AlertmanagerURL, "ruler.alertmanager-url", "", "Comma-separated list of Alertmanager URLs to send notifications to. Each Alertmanager URL is treated as a separate group in the configuration. Multiple Alertmanagers in HA per group can be supported by using DNS resolution via '-ruler.alertmanager-discovery'.")
	f.BoolVar(&cfg.AlertmanagerDiscovery, "ruler.alertmanager-discovery", false, "Use DNS SRV records to discover Alertmanager hosts.")
	f.DurationVar(&cfg.AlertmanagerRefreshInterval, "ruler.alertmanager-refresh-interval", alertmanagerRefreshIntervalDefault, "How long to wait between refreshing DNS resolutions of Alertmanager hosts.")
	f.BoolVar(&cfg.AlertmanangerEnableV2API, "ruler.alertmanager-use-v2", true, "Use Alertmanager APIv2. APIv1 was deprecated in Alertmanager 0.16.0 and is removed as of 0.27.0.")
	f.IntVar(&cfg.NotificationQueueCapacity, "ruler.notification-queue-capacity", alertmanagerNotificationQueueCapacityDefault, "Capacity of the queue for notifications to be sent to the Alertmanager.")
	f.DurationVar(&cfg.NotificationTimeout, "ruler.notification-timeout", alertmanagerNotificationTimeoutDefault, "HTTP timeout duration when sending notifications to the Alertmanager.")

	f.DurationVar(&cfg.SearchPendingFor, "ruler.search-pending-for", 5*time.Minute, "Time to spend searching for a pending ruler when shutting down.")
	f.BoolVar(&cfg.EnableSharding, "ruler.enable-sharding", false, "Distribute rule evaluation using ring backend.")
	f.StringVar(&cfg.ShardingStrategy, "ruler.sharding-strategy", util.ShardingStrategyDefault, fmt.Sprintf("The sharding strategy to use. Supported values are: %s.", strings.Join(supportedShardingStrategies, ", ")))
	f.StringVar(&cfg.ShardingAlgo, "ruler.sharding-algo", util.ShardingAlgoByGroup, fmt.Sprintf("The sharding algorithm to use for deciding how rules & groups are sharded. Supported values are: %s.", strings.Join(supportedShardingAlgos, ", ")))
	f.DurationVar(&cfg.FlushCheckPeriod, "ruler.flush-period", 1*time.Minute, "Period with which to attempt to flush rule groups.")
	f.StringVar(&cfg.RulePath, "ruler.rule-path", "/rules", "File path to store temporary rule files.")
	f.DurationVar(&cfg.OutageTolerance, "ruler.for-outage-tolerance", time.Hour, `Max time to tolerate outage for restoring "for" state of alert.`)
	f.DurationVar(&cfg.ForGracePeriod, "ruler.for-grace-period", 10*time.Minute, `Minimum duration between alert and restored "for" state. This is maintained only for alerts with configured "for" time greater than the grace period.`)
	f.DurationVar(&cfg.ResendDelay, "ruler.resend-delay", time.Minute, `Minimum amount of time to wait before resending an alert to Alertmanager.`)

	f.Var(&cfg.EnabledTenants, "ruler.enabled-tenants", "Comma separated list of tenants whose rules this ruler can evaluate. If specified, only these tenants will be handled by ruler, otherwise this ruler can process rules from all tenants. Subject to sharding.")
	f.Var(&cfg.DisabledTenants, "ruler.disabled-tenants", "Comma separated list of tenants whose rules this ruler cannot evaluate. If specified, a ruler that would normally pick the specified tenant(s) for processing will ignore them instead. Subject to sharding.")

	f.BoolVar(&cfg.EnableQueryStats, "ruler.query-stats-enabled", false, "Report the wall time for ruler queries to complete as a per user metric and as an info level log message.")
	f.BoolVar(&cfg.DisableRuleGroupLabel, "ruler.disable-rule-group-label", false, "Disable the rule_group label on exported metrics.")

	f.BoolVar(&cfg.EnableAPI, "ruler.enable-api", true, "Enable the ruler API.")

	cfg.RingCheckPeriod = 5 * time.Second
}

// MultiTenantManager is the interface of interaction with a Manager that is tenant aware.
type MultiTenantManager interface {
	// SyncRuleGroups is used to sync the Manager with rules from the RuleStore.
	// If existing user is missing in the ruleGroups map, its ruler manager will be stopped.
	SyncRuleGroups(ctx context.Context, ruleGroups map[string]rulespb.RuleGroupList)
	// GetRules fetches rules for a particular tenant (userID).
	GetRules(userID string) []*promRules.Group
	// Stop stops all Manager components.
	Stop()
	// ValidateRuleGroup validates a rulegroup
	ValidateRuleGroup(rulefmt.RuleGroup) []error
}

// Ruler evaluates rules.
//
//	+---------------------------------------------------------------+
//	|                                                               |
//	|                   Query       +-------------+                 |
//	|            +------------------>             |                 |
//	|            |                  |    Store    |                 |
//	|            | +----------------+             |                 |
//	|            | |     Rules      +-------------+                 |
//	|            | |                                                |
//	|            | |                                                |
//	|            | |                                                |
//	|       +----+-v----+   Filter  +------------+                  |
//	|       |           +----------->            |                  |
//	|       |   Ruler   |           |    Ring    |                  |
//	|       |           <-----------+            |                  |
//	|       +-------+---+   Rules   +------------+                  |
//	|               |                                               |
//	|               |                                               |
//	|               |                                               |
//	|               |    Load      +-----------------+              |
//	|               +-------------->                 |              |
//	|                              |     Manager     |              |
//	|                              |                 |              |
//	|                              +-----------------+              |
//	|                                                               |
//	+---------------------------------------------------------------+
type Ruler struct {
	services.Service

	cfg        Config
	lifecycler *ring.BasicLifecycler
	ring       *ring.Ring
	store      rulestore.RuleStore
	manager    MultiTenantManager
	limits     RulesLimits

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Pool of clients used to connect to other ruler replicas.
	clientsPool ClientsPool

	ringCheckErrors prometheus.Counter
	rulerSync       *prometheus.CounterVec

	allowedTenants *util.AllowedTenants

	registry         prometheus.Registerer
	logger           log.Logger
	metricsNamespace string
}

// NewRuler creates a new ruler from a distributor and chunk store.
func NewRuler(cfg Config, manager MultiTenantManager, reg prometheus.Registerer, logger log.Logger, ruleStore rulestore.RuleStore, limits RulesLimits, metricsNamespace string) (*Ruler, error) {
	return newRuler(cfg, manager, reg, logger, ruleStore, limits, newRulerClientPool(cfg.ClientTLSConfig, logger, reg, metricsNamespace), metricsNamespace)
}

func newRuler(cfg Config, manager MultiTenantManager, reg prometheus.Registerer, logger log.Logger, ruleStore rulestore.RuleStore, limits RulesLimits, clientPool ClientsPool, metricsNamespace string) (*Ruler, error) {
	if err := cfg.Validate(logger); err != nil {
		return nil, fmt.Errorf("invalid ruler config: %w", err)
	}

	ruler := &Ruler{
		cfg:              cfg,
		store:            ruleStore,
		manager:          manager,
		registry:         reg,
		logger:           logger,
		limits:           limits,
		clientsPool:      clientPool,
		allowedTenants:   util.NewAllowedTenants(cfg.EnabledTenants, cfg.DisabledTenants),
		metricsNamespace: metricsNamespace,

		ringCheckErrors: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_ring_check_errors_total",
			Help:      "Number of errors that have occurred when checking the ring for ownership",
		}),

		rulerSync: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_sync_rules_total",
			Help:      "Total number of times the ruler sync operation triggered.",
		}, []string{"reason"}),
	}

	if len(cfg.EnabledTenants) > 0 {
		level.Info(ruler.logger).Log("msg", "ruler using enabled users", "enabled", strings.Join(cfg.EnabledTenants, ", "))
	}
	if len(cfg.DisabledTenants) > 0 {
		level.Info(ruler.logger).Log("msg", "ruler using disabled users", "disabled", strings.Join(cfg.DisabledTenants, ", "))
	}

	if cfg.EnableSharding {
		ringStore, err := kv.NewClient(
			cfg.Ring.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", reg), "ruler"),
			logger,
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}

		if err = enableSharding(ruler, ringStore); err != nil {
			return nil, errors.Wrap(err, "setup ruler sharding ring")
		}
	}

	ruler.Service = services.NewBasicService(ruler.starting, ruler.run, ruler.stopping)
	return ruler, nil
}

func enableSharding(r *Ruler, ringStore kv.Client) error {
	lifecyclerCfg, err := r.cfg.Ring.ToLifecyclerConfig(r.logger)
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's lifecycler config")
	}

	// Define lifecycler delegates in reverse order (last to be called defined first because they're
	// chained via "next delegate").
	delegate := ring.BasicLifecyclerDelegate(r)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, r.logger)
	delegate = ring.NewAutoForgetDelegate(r.cfg.Ring.HeartbeatTimeout*ringAutoForgetUnhealthyPeriods, delegate, r.logger)

	rulerRingName := "ruler"
	r.lifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, rulerRingName, ringKey, ringStore, delegate, r.logger, prometheus.WrapRegistererWithPrefix(r.metricsNamespace+"_", r.registry))
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's lifecycler")
	}

	r.ring, err = ring.NewWithStoreClientAndStrategy(r.cfg.Ring.ToRingConfig(), rulerRingName, ringKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix(r.metricsNamespace+"_", r.registry), r.logger)
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's ring")
	}

	return nil
}

func (r *Ruler) starting(ctx context.Context) error {
	// If sharding is enabled, start the used subservices.
	if r.cfg.EnableSharding {
		var err error

		if r.subservices, err = services.NewManager(r.lifecycler, r.ring, r.clientsPool); err != nil {
			return errors.Wrap(err, "unable to start ruler subservices")
		}

		r.subservicesWatcher = services.NewFailureWatcher()
		r.subservicesWatcher.WatchManager(r.subservices)

		if err = services.StartManagerAndAwaitHealthy(ctx, r.subservices); err != nil {
			return errors.Wrap(err, "unable to start ruler subservices")
		}
	}

	// TODO: ideally, ruler would wait until its queryable is finished starting.
	return nil
}

// Stop stops the Ruler.
// Each function of the ruler is terminated before leaving the ring
func (r *Ruler) stopping(_ error) error {
	r.manager.Stop()

	if r.subservices != nil {
		_ = services.StopManagerAndAwaitStopped(context.Background(), r.subservices)
	}
	return nil
}

type sender interface {
	Send(alerts ...*notifier.Alert)
}

type query struct {
	Expr       string      `json:"expr"`
	QueryType  string      `json:"queryType"`
	Datasource *datasource `json:"datasource,omitempty"`
}

type datasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

func grafanaLinkForExpression(expr, datasourceUID string) string {
	exprStruct := query{
		Expr:      expr,
		QueryType: "range",
	}

	if datasourceUID != "" {
		exprStruct.Datasource = &datasource{
			Type: "loki",
			UID:  datasourceUID,
		}
	}

	marshaledExpression, _ := json.Marshal(exprStruct)
	params := url.Values{}
	params.Set("left", fmt.Sprintf(`{"queries":[%s]}`, marshaledExpression))
	return `/explore?` + params.Encode()
}

// SendAlerts implements a rules.NotifyFunc for a Notifier.
// It filters any non-firing alerts from the input.
//
// Copied from Prometheus's main.go.
func SendAlerts(n sender, externalURL, datasourceUID string) promRules.NotifyFunc {
	return func(_ context.Context, expr string, alerts ...*promRules.Alert) {
		var res []*notifier.Alert

		for _, alert := range alerts {
			a := &notifier.Alert{
				StartsAt:     alert.FiredAt,
				Labels:       alert.Labels,
				Annotations:  alert.Annotations,
				GeneratorURL: externalURL + grafanaLinkForExpression(expr, datasourceUID),
			}
			if !alert.ResolvedAt.IsZero() {
				a.EndsAt = alert.ResolvedAt
			} else {
				a.EndsAt = alert.ValidUntil
			}
			res = append(res, a)
		}

		if len(alerts) > 0 {
			n.Send(res...)
		}
	}
}

var sep = []byte("/")

func tokenForGroup(g *rulespb.RuleGroupDesc) uint32 {
	ringHasher := fnv.New32a()

	// Hasher never returns err.
	_, _ = ringHasher.Write([]byte(g.User))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(g.Namespace))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(g.Name))

	return ringHasher.Sum32()
}

func tokenForRule(g *rulespb.RuleGroupDesc, r *rulespb.RuleDesc) uint32 {
	ringHasher := fnv.New32a()

	// Hasher never returns err.
	_, _ = ringHasher.Write([]byte(g.User))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(g.Namespace))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(g.Name))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(getRuleIdentifier(r)))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(r.Expr))
	_, _ = ringHasher.Write(sep)
	_, _ = ringHasher.Write([]byte(logproto.FromLabelAdaptersToLabels(r.Labels).String()))

	return ringHasher.Sum32()
}

func getRuleIdentifier(r *rulespb.RuleDesc) string {
	if r == nil {
		return ""
	}

	if r.Alert == "" {
		return r.Record
	}

	return r.Alert
}

func instanceOwnsRuleGroup(r ring.ReadRing, g *rulespb.RuleGroupDesc, instanceAddr string) (bool, error) {
	hash := tokenForGroup(g)

	rlrs, err := r.Get(hash, RingOp, nil, nil, nil)
	if err != nil {
		return false, errors.Wrap(err, "error reading ring to verify rule group ownership")
	}

	return rlrs.Instances[0].Addr == instanceAddr, nil
}

func instanceOwnsRule(r ring.ReadRing, rg *rulespb.RuleGroupDesc, rd *rulespb.RuleDesc, instanceAddr string) (bool, error) {
	hash := tokenForRule(rg, rd)

	rlrs, err := r.Get(hash, RingOp, nil, nil, nil)
	if err != nil {
		return false, errors.Wrap(err, "error reading ring to verify rule group ownership")
	}

	return rlrs.Instances[0].Addr == instanceAddr, nil
}

func (r *Ruler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.cfg.EnableSharding {
		r.ring.ServeHTTP(w, req)
	} else {
		unshardedPage := `
			<!DOCTYPE html>
			<html>
				<head>
					<meta charset="UTF-8">
					<title>Cortex Ruler Status</title>
				</head>
				<body>
					<h1>Cortex Ruler Status</h1>
					<p>Ruler running with shards disabled</p>
				</body>
			</html>`
		util.WriteHTMLResponse(w, unshardedPage)
	}
}

func (r *Ruler) run(ctx context.Context) error {
	level.Info(r.logger).Log("msg", "ruler up and running")

	tick := time.NewTicker(r.cfg.PollInterval)
	defer tick.Stop()

	var ringTickerChan <-chan time.Time
	var ringLastState ring.ReplicationSet

	if r.cfg.EnableSharding {
		ringLastState, _ = r.ring.GetAllHealthy(RingOp)
		ringTicker := time.NewTicker(util.DurationWithJitter(r.cfg.RingCheckPeriod, 0.2))
		defer ringTicker.Stop()
		ringTickerChan = ringTicker.C
	}

	r.syncRules(ctx, rulerSyncReasonInitial)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			r.syncRules(ctx, rulerSyncReasonPeriodic)
		case <-ringTickerChan:
			// We ignore the error because in case of error it will return an empty
			// replication set which we use to compare with the previous state.
			currRingState, _ := r.ring.GetAllHealthy(RingOp)

			if ring.HasReplicationSetChanged(ringLastState, currRingState) {
				ringLastState = currRingState
				r.syncRules(ctx, rulerSyncReasonRingChange)
			}
		case err := <-r.subservicesWatcher.Chan():
			return errors.Wrap(err, "ruler subservice failed")
		}
	}
}

func (r *Ruler) syncRules(ctx context.Context, reason string) {
	level.Debug(r.logger).Log("msg", "syncing rules", "reason", reason)
	r.rulerSync.WithLabelValues(reason).Inc()

	configs, err := r.listRules(ctx)
	if err != nil {
		level.Error(r.logger).Log("msg", "unable to list rules", "err", err)
		return
	}

	err = r.store.LoadRuleGroups(ctx, configs)
	if err != nil {
		level.Error(r.logger).Log("msg", "unable to load rules owned by this ruler", "err", err)
		return
	}

	// This will also delete local group files for users that are no longer in 'configs' map.
	r.manager.SyncRuleGroups(ctx, configs)
}

func (r *Ruler) listRules(ctx context.Context) (result map[string]rulespb.RuleGroupList, err error) {
	if r.cfg.EnableSharding {
		result, err = r.listRulesSharding(ctx)
	} else {
		result, err = r.listRulesNoSharding(ctx)
	}

	if err != nil {
		return
	}

	for userID := range result {
		if !r.allowedTenants.IsAllowed(userID) {
			level.Debug(r.logger).Log("msg", "ignoring rule groups for user, not allowed", "user", userID)
			delete(result, userID)
		}
	}
	return
}

func (r *Ruler) listRulesNoSharding(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	return r.store.ListAllRuleGroups(ctx)
}

func (r *Ruler) listRulesSharding(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	if r.cfg.ShardingStrategy == util.ShardingStrategyShuffle {
		return r.listRulesShuffleSharding(ctx)
	}

	return r.listRulesShardingDefault(ctx)
}

func (r *Ruler) listRulesShardingDefault(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	configs, err := r.store.ListAllRuleGroups(ctx)
	if err != nil {
		return nil, err
	}

	filteredConfigs := make(map[string]rulespb.RuleGroupList)
	for userID, groups := range configs {
		filtered := filterRules(r.cfg.ShardingAlgo, userID, groups, r.ring, r.lifecycler.GetInstanceAddr(), r.logger, r.ringCheckErrors)
		if len(filtered) > 0 {
			filteredConfigs[userID] = filtered
		}
	}
	return filteredConfigs, nil
}

func (r *Ruler) listRulesShuffleSharding(ctx context.Context) (map[string]rulespb.RuleGroupList, error) {
	users, err := r.store.ListAllUsers(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list users of ruler")
	}

	// Only users in userRings will be used in the to load the rules.
	userRings := map[string]ring.ReadRing{}
	for _, u := range users {
		if shardSize := r.limits.RulerTenantShardSize(u); shardSize > 0 {
			subRing := r.ring.ShuffleShard(u, shardSize)

			// Include the user only if it belongs to this ruler shard.
			if subRing.HasInstance(r.lifecycler.GetInstanceID()) {
				userRings[u] = subRing
			}
		} else {
			// A shard size of 0 means shuffle sharding is disabled for this specific user.
			// In that case we use the full ring so that rule groups will be sharded across all rulers.
			userRings[u] = r.ring
		}
	}

	if len(userRings) == 0 {
		return nil, nil
	}

	userCh := make(chan string, len(userRings))
	for u := range userRings {
		userCh <- u
	}
	close(userCh)

	mu := sync.Mutex{}
	result := map[string]rulespb.RuleGroupList{}

	concurrency := loadRulesConcurrency
	if len(userRings) < concurrency {
		concurrency = len(userRings)
	}

	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < concurrency; i++ {
		g.Go(func() error {
			for userID := range userCh {
				groups, err := r.store.ListRuleGroupsForUserAndNamespace(gctx, userID, "")
				if err != nil {
					return errors.Wrapf(err, "failed to fetch rule groups for user %s", userID)
				}

				filtered := filterRules(r.cfg.ShardingAlgo, userID, groups, userRings[userID], r.lifecycler.GetInstanceAddr(), r.logger, r.ringCheckErrors)
				if len(filtered) == 0 {
					continue
				}

				mu.Lock()
				result[userID] = filtered
				mu.Unlock()
			}
			return nil
		})
	}

	err = g.Wait()
	return result, err
}

// filterRules returns a list of rule groups, each with a single rule, ONLY for rules that are "owned" by the given ring instance.
// the reason why this function is not a method on Ruler is to make sure we don't accidentally use r.ring, but only ring passed as parameter.
func filterRules(shardingAlgo string, userID string, ruleGroups []*rulespb.RuleGroupDesc, ring ring.ReadRing, instanceAddr string, logger log.Logger, ringCheckErrors prometheus.Counter) []*rulespb.RuleGroupDesc {
	// Return one rule group per rule that is owned by this ring instance.
	// One rule group is returned per rule because Prometheus executed rule groups concurrently but rules within a group sequentially;
	// we are sharding by rule here to explicitly *avoid* sequential execution since Loki does not need this.
	var result = make([]*rulespb.RuleGroupDesc, 0, len(ruleGroups))

	for _, g := range ruleGroups {
		glog := log.With(logger, "user", userID, "namespace", g.Namespace, "group", g.Name)

		switch shardingAlgo {

		// if we are sharding by rule group, we can just add the entire group
		case util.ShardingAlgoByGroup:
			owned, err := instanceOwnsRuleGroup(ring, g, instanceAddr)
			if err != nil {
				ringCheckErrors.Inc()
				level.Error(glog).Log("msg", "failed to check if the ruler replica owns the rule group", "err", err)
				continue
			}

			if owned {
				level.Debug(glog).Log("msg", "rule group owned")
				result = append(result, g)
			} else {
				level.Debug(glog).Log("msg", "rule group not owned, ignoring")
			}

			continue

		// if we are sharding by rule, we need to create rule groups for each rule to comply with Prometheus' rule engine's expectations
		case util.ShardingAlgoByRule:
			for _, r := range g.Rules {
				rlog := log.With(glog, "rule", getRuleIdentifier(r))

				owned, err := instanceOwnsRule(ring, g, r, instanceAddr)
				if err != nil {
					ringCheckErrors.Inc()
					level.Error(rlog).Log("msg", "failed to check if the ruler replica owns the rule", "err", err)
					continue
				}

				if !owned {
					level.Debug(rlog).Log("msg", "rule not owned, ignoring")
					continue
				}

				level.Debug(rlog).Log("msg", "rule owned")

				// clone the group and replace the rules
				clone := cloneGroupWithRule(g, r)
				if clone == nil {
					level.Error(rlog).Log("msg", "failed to filter rules", "err", "failed to clone rule group; type coercion failed")
					continue
				}

				result = append(result, clone)
			}
		}
	}

	return result
}

func cloneGroupWithRule(g *rulespb.RuleGroupDesc, r *rulespb.RuleDesc) *rulespb.RuleGroupDesc {
	clone, ok := proto.Clone(g).(*rulespb.RuleGroupDesc)
	if !ok {
		return nil
	}

	// Prometheus relies on group names being unique, and this assumption is very deeply baked in
	// so we append the rule token to make each group name unique.
	// TODO(dannyk) this is all quite hacky, and we shoud look at forking Prometheus' rule evaluation engine at some point.
	clone.Name = AddRuleTokenToGroupName(g, r)
	clone.Rules = []*rulespb.RuleDesc{r}

	return clone
}

// the delimiter is prefixed with ";" since that is what Prometheus uses for its group key
const ruleTokenDelimiter = ";rule-shard-token" //#nosec G101 -- False positive

// AddRuleTokenToGroupName adds a rule shard token to a given group's name to make it unique.
// Only relevant when using "by-rule" sharding strategy.
func AddRuleTokenToGroupName(g *rulespb.RuleGroupDesc, r *rulespb.RuleDesc) string {
	return fmt.Sprintf("%s"+ruleTokenDelimiter+"%d", g.Name, tokenForRule(g, r))
}

// RemoveRuleTokenFromGroupName removes the rule shard token from the group name.
// Only relevant when using "by-rule" sharding strategy.
func RemoveRuleTokenFromGroupName(name string) string {
	return strings.Split(name, ruleTokenDelimiter)[0]
}

// GetRules retrieves the running rules from this ruler and all running rulers in the ring if
// sharding is enabled
func (r *Ruler) GetRules(ctx context.Context, req *RulesRequest) ([]*GroupStateDesc, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, fmt.Errorf("no user id found in context")
	}

	if r.cfg.EnableSharding {
		return r.getShardedRules(ctx, userID, req)
	}

	return r.getLocalRules(userID, req)
}

type StringFilterSet map[string]struct{}

func makeStringFilterSet(values []string) StringFilterSet {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

// IsFiltered returns whether to filter the value or not.
// If the set is empty, then nothing is filtered.
func (fs StringFilterSet) IsFiltered(val string) bool {
	if len(fs) == 0 {
		return false
	}
	_, ok := fs[val]
	return !ok
}

func (r *Ruler) getLocalRules(userID string, req *RulesRequest) ([]*GroupStateDesc, error) {
	var getRecordingRules, getAlertingRules bool

	switch req.Filter {
	case AlertingRule:
		getAlertingRules = true
	case RecordingRule:
		getRecordingRules = true
	case AnyRule:
		getAlertingRules = true
		getRecordingRules = true
	default:
		return nil, fmt.Errorf("unexpected rule filter %s", req.Filter)
	}

	fileSet := makeStringFilterSet(req.File)
	groupSet := makeStringFilterSet(req.RuleGroup)
	ruleSet := makeStringFilterSet(req.RuleName)

	groups := r.manager.GetRules(userID)

	groupDescs := make([]*GroupStateDesc, 0, len(groups))
	prefix := filepath.Join(r.cfg.RulePath, userID) + "/"

	for _, group := range groups {
		if groupSet.IsFiltered(group.Name()) {
			continue
		}

		interval := group.Interval()

		// The mapped filename is url path escaped encoded to make handling `/` characters easier
		decodedNamespace, err := url.PathUnescape(strings.TrimPrefix(group.File(), prefix))
		if err != nil {
			return nil, errors.Wrap(err, "unable to decode rule filename")
		}

		if fileSet.IsFiltered(decodedNamespace) {
			continue
		}

		groupDesc := &GroupStateDesc{
			Group: &rulespb.RuleGroupDesc{
				Name:      group.Name(),
				Namespace: decodedNamespace,
				Interval:  interval,
				User:      userID,
				Limit:     int64(group.Limit()),
			},

			EvaluationTimestamp: group.GetLastEvaluation(),
			EvaluationDuration:  group.GetEvaluationTime(),
		}
		for _, r := range group.Rules() {
			if ruleSet.IsFiltered(r.Name()) {
				continue
			}

			lastError := ""
			if r.LastError() != nil {
				lastError = r.LastError().Error()
			}

			var ruleDesc *RuleStateDesc
			switch rule := r.(type) {
			case *promRules.AlertingRule:
				if !getAlertingRules {
					continue
				}

				rule.ActiveAlerts()
				alerts := []*AlertStateDesc{}
				for _, a := range rule.ActiveAlerts() {
					alerts = append(alerts, &AlertStateDesc{
						State:       a.State.String(),
						Labels:      logproto.FromLabelsToLabelAdapters(a.Labels),
						Annotations: logproto.FromLabelsToLabelAdapters(a.Annotations),
						Value:       a.Value,
						ActiveAt:    a.ActiveAt,
						FiredAt:     a.FiredAt,
						ResolvedAt:  a.ResolvedAt,
						LastSentAt:  a.LastSentAt,
						ValidUntil:  a.ValidUntil,
					})
				}
				ruleDesc = &RuleStateDesc{
					Rule: &rulespb.RuleDesc{
						Expr:        rule.Query().String(),
						Alert:       rule.Name(),
						For:         rule.HoldDuration(),
						Labels:      logproto.FromLabelsToLabelAdapters(rule.Labels()),
						Annotations: logproto.FromLabelsToLabelAdapters(rule.Annotations()),
					},
					State:               rule.State().String(),
					Health:              string(rule.Health()),
					LastError:           lastError,
					Alerts:              alerts,
					EvaluationTimestamp: rule.GetEvaluationTimestamp(),
					EvaluationDuration:  rule.GetEvaluationDuration(),
				}
			case *promRules.RecordingRule:
				if !getRecordingRules {
					continue
				}

				ruleDesc = &RuleStateDesc{
					Rule: &rulespb.RuleDesc{
						Record: rule.Name(),
						Expr:   rule.Query().String(),
						Labels: logproto.FromLabelsToLabelAdapters(rule.Labels()),
					},
					Health:              string(rule.Health()),
					LastError:           lastError,
					EvaluationTimestamp: rule.GetEvaluationTimestamp(),
					EvaluationDuration:  rule.GetEvaluationDuration(),
				}
			default:
				return nil, errors.Errorf("failed to assert type of rule '%v'", rule.Name())
			}
			groupDesc.ActiveRules = append(groupDesc.ActiveRules, ruleDesc)
		}

		// Prometheus does not return a rule group if it has no rules after filtering.
		if len(groupDesc.ActiveRules) > 0 {
			groupDescs = append(groupDescs, groupDesc)
		}
	}
	return groupDescs, nil
}

func (r *Ruler) getShardedRules(ctx context.Context, userID string, rulesReq *RulesRequest) ([]*GroupStateDesc, error) {
	ring := ring.ReadRing(r.ring)

	if shardSize := r.limits.RulerTenantShardSize(userID); shardSize > 0 && r.cfg.ShardingStrategy == util.ShardingStrategyShuffle {
		ring = r.ring.ShuffleShard(userID, shardSize)
	}

	rulers, err := ring.GetReplicationSetForOperation(RingOp)
	if err != nil {
		return nil, err
	}

	ctx, err = user.InjectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to inject user ID into grpc request, %v", err)
	}

	var (
		mergedMx sync.Mutex
		merged   = make(map[string]*GroupStateDesc)
	)

	// Concurrently fetch rules from all rulers. Since rules are not replicated,
	// we need all requests to succeed.
	addresses := rulers.GetAddresses()
	err = concurrency.ForEachJob(ctx, len(addresses), len(addresses), func(ctx context.Context, idx int) error {
		addr := addresses[idx]

		rulerClient, err := r.clientsPool.GetClientFor(addr)
		if err != nil {
			return errors.Wrapf(err, "unable to get client for ruler %s", addr)
		}

		newGrps, err := rulerClient.Rules(ctx, rulesReq)
		if err != nil || newGrps == nil {
			return fmt.Errorf("unable to retrieve rules from ruler %s: %w", addr, err)
		}

		mergedMx.Lock()

		for _, grp := range newGrps.Groups {
			if grp == nil {
				continue
			}

			name := RemoveRuleTokenFromGroupName(grp.Group.Name)
			grp.Group.Name = name

			// When merging include the namepsace in case the same group name exists in multiple namespaces
			name = grp.Group.Namespace + "/" + name

			_, found := merged[name]
			if found {
				merged[name].ActiveRules = append(merged[name].ActiveRules, grp.ActiveRules...)
			} else {
				merged[name] = grp
			}
		}

		mergedMx.Unlock()

		return nil
	})

	mergedMx.Lock()
	descs := make([]*GroupStateDesc, 0, len(merged))
	for _, desc := range merged {
		descs = append(descs, desc)
	}
	mergedMx.Unlock()

	return descs, err
}

// Rules implements the rules service
func (r *Ruler) Rules(ctx context.Context, req *RulesRequest) (*RulesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, fmt.Errorf("no user id found in context")
	}

	groupDescs, err := r.getLocalRules(userID, req)
	if err != nil {
		return nil, err
	}

	return &RulesResponse{Groups: groupDescs}, nil
}

// AssertMaxRuleGroups limit has not been reached compared to the current
// number of total rule groups in input and returns an error if so.
func (r *Ruler) AssertMaxRuleGroups(userID string, rg int) error {
	limit := r.limits.RulerMaxRuleGroupsPerTenant(userID)

	if limit <= 0 {
		return nil
	}

	if rg <= limit {
		return nil
	}

	return fmt.Errorf(errMaxRuleGroupsPerUserLimitExceeded, limit, rg)
}

// AssertMaxRulesPerRuleGroup limit has not been reached compared to the current
// number of rules in a rule group in input and returns an error if so.
func (r *Ruler) AssertMaxRulesPerRuleGroup(userID string, rules int) error {
	limit := r.limits.RulerMaxRulesPerRuleGroup(userID)

	if limit <= 0 {
		return nil
	}

	if rules <= limit {
		return nil
	}
	return fmt.Errorf(errMaxRulesPerRuleGroupPerUserLimitExceeded, limit, rules)
}

func (r *Ruler) DeleteTenantConfiguration(w http.ResponseWriter, req *http.Request) {
	logger := util_log.WithContext(req.Context(), r.logger)

	userID, err := tenant.TenantID(req.Context())
	if err != nil {
		// When Cortex is running, it uses Auth Middleware for checking X-Scope-OrgID and injecting tenant into context.
		// Auth Middleware sends http.StatusUnauthorized if X-Scope-OrgID is missing, so we do too here, for consistency.
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = r.store.DeleteNamespace(req.Context(), userID, "") // Empty namespace = delete all rule groups.
	if err != nil && !errors.Is(err, rulestore.ErrGroupNamespaceNotFound) {
		respondServerError(logger, w, err.Error())
		return
	}

	level.Info(logger).Log("msg", "deleted all tenant rule groups", "user", userID)
	w.WriteHeader(http.StatusOK)
}

func (r *Ruler) ListAllRules(w http.ResponseWriter, req *http.Request) {
	logger := util_log.WithContext(req.Context(), r.logger)

	userIDs, err := r.store.ListAllUsers(req.Context())
	if err != nil {
		level.Error(logger).Log("msg", errListAllUser, "err", err)
		http.Error(w, fmt.Sprintf("%s: %s", errListAllUser, err.Error()), http.StatusInternalServerError)
		return
	}

	done := make(chan struct{})
	iter := make(chan interface{})

	go func() {
		util.StreamWriteYAMLResponse(w, iter, logger)
		close(done)
	}()

	err = concurrency.ForEachUser(req.Context(), userIDs, fetchRulesConcurrency, func(ctx context.Context, userID string) error {
		rg, err := r.store.ListRuleGroupsForUserAndNamespace(ctx, userID, "")
		if err != nil {
			return errors.Wrapf(err, "failed to fetch ruler config for user %s", userID)
		}
		userRules := map[string]rulespb.RuleGroupList{userID: rg}
		if err := r.store.LoadRuleGroups(ctx, userRules); err != nil {
			return errors.Wrapf(err, "failed to load ruler config for user %s", userID)
		}
		data := map[string]map[string][]rulefmt.RuleGroup{userID: userRules[userID].Formatted()}

		select {
		case iter <- data:
		case <-done: // stop early, if sending response has already finished
		}

		return nil
	})
	if err != nil {
		level.Error(logger).Log("msg", "failed to list all ruler configs", "err", err)
	}
	close(iter)
	<-done
}
