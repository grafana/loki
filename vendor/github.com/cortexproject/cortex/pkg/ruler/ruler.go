package ruler

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/notifier"
	promRules "github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/weaveworks/common/user"
	"golang.org/x/net/context/ctxhttp"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	store "github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/tls"
)

var (
	ringCheckErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "ruler_ring_check_errors_total",
		Help:      "Number of errors that have occurred when checking the ring for ownership",
	})
	configUpdatesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "ruler_config_updates_total",
		Help:      "Total number of config updates triggered by a user",
	}, []string{"user"})
	configUpdateFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "ruler_config_update_failures_total",
		Help:      "Total number of config update failures triggered by a user",
	}, []string{"user", "reason"})
	managersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "ruler_managers_total",
		Help:      "Total number of managers registered and running in the ruler",
	})
)

// Config is the configuration for the recording rules server.
type Config struct {
	// This is used for template expansion in alerts; must be a valid URL.
	ExternalURL flagext.URLValue `yaml:"external_url"`
	// TLS parameters for the GRPC Client
	ClientTLSConfig tls.ClientConfig `yaml:"ruler_client"`
	// How frequently to evaluate rules by default.
	EvaluationInterval time.Duration `yaml:"evaluation_interval"`
	// Delay the evaluation of all rules by a set interval to give a buffer
	// to metric that haven't been forwarded to cortex yet.
	EvaluationDelay time.Duration `yaml:"evaluation_delay_duration"`
	// How frequently to poll for updated rules.
	PollInterval time.Duration `yaml:"poll_interval"`
	// Rule Storage and Polling configuration.
	StoreConfig RuleStoreConfig `yaml:"storage"`
	// Path to store rule files for prom manager.
	RulePath string `yaml:"rule_path"`

	// URL of the Alertmanager to send notifications to.
	AlertmanagerURL flagext.StringSlice `yaml:"alertmanager_url"`
	// Whether to use DNS SRV records to discover Alertmanager.
	AlertmanagerDiscovery bool `yaml:"enable_alertmanager_discovery"`
	// How long to wait between refreshing the list of Alertmanager based on DNS service discovery.
	AlertmanagerRefreshInterval time.Duration `yaml:"alertmanager_refresh_interval"`
	// Enables the ruler notifier to use the Alertmananger V2 API.
	AlertmanangerEnableV2API bool `yaml:"enable_alertmanager_v2"`
	// Capacity of the queue for notifications to be sent to the Alertmanager.
	NotificationQueueCapacity int `yaml:"notification_queue_capacity"`
	// HTTP timeout duration when sending notifications to the Alertmanager.
	NotificationTimeout time.Duration `yaml:"notification_timeout"`

	// Max time to tolerate outage for restoring "for" state of alert.
	OutageTolerance time.Duration `yaml:"for_outage_tolerance"`
	// Minimum duration between alert and restored "for" state. This is maintained only for alerts with configured "for" time greater than grace period.
	ForGracePeriod time.Duration `yaml:"for_grace_period"`
	// Minimum amount of time to wait before resending an alert to Alertmanager.
	ResendDelay time.Duration `yaml:"resend_delay"`

	// Enable sharding rule groups.
	EnableSharding   bool          `yaml:"enable_sharding"`
	SearchPendingFor time.Duration `yaml:"search_pending_for"`
	Ring             RingConfig    `yaml:"ring"`
	FlushCheckPeriod time.Duration `yaml:"flush_period"`

	EnableAPI bool `yaml:"enable_api"`
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if err := cfg.StoreConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	return nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientTLSConfig.RegisterFlagsWithPrefix("ruler.client", f)
	cfg.StoreConfig.RegisterFlags(f)
	cfg.Ring.RegisterFlags(f)

	// Deprecated Flags that will be maintained to avoid user disruption
	flagext.DeprecatedFlag(f, "ruler.client-timeout", "This flag has been renamed to ruler.configs.client-timeout")
	flagext.DeprecatedFlag(f, "ruler.group-timeout", "This flag is no longer functional.")
	flagext.DeprecatedFlag(f, "ruler.num-workers", "This flag is no longer functional. For increased concurrency horizontal sharding is recommended")

	cfg.ExternalURL.URL, _ = url.Parse("") // Must be non-nil
	f.Var(&cfg.ExternalURL, "ruler.external.url", "URL of alerts return path.")
	f.DurationVar(&cfg.EvaluationInterval, "ruler.evaluation-interval", 1*time.Minute, "How frequently to evaluate rules")
	f.DurationVar(&cfg.EvaluationDelay, "ruler.evaluation-delay-duration", 0, "Duration to delay the evaluation of rules to ensure they underlying metrics have been pushed to cortex.")
	f.DurationVar(&cfg.PollInterval, "ruler.poll-interval", 1*time.Minute, "How frequently to poll for rule changes")

	f.Var(&cfg.AlertmanagerURL, "ruler.alertmanager-url", "Space-separated list of URL(s) of the Alertmanager(s) to send notifications to. Each Alertmanager URL is treated as a separate group in the configuration. Multiple Alertmanagers in HA per group can be supported by using DNS resolution via -ruler.alertmanager-discovery.")
	f.BoolVar(&cfg.AlertmanagerDiscovery, "ruler.alertmanager-discovery", false, "Use DNS SRV records to discover Alertmanager hosts.")
	f.DurationVar(&cfg.AlertmanagerRefreshInterval, "ruler.alertmanager-refresh-interval", 1*time.Minute, "How long to wait between refreshing DNS resolutions of Alertmanager hosts.")
	f.BoolVar(&cfg.AlertmanangerEnableV2API, "ruler.alertmanager-use-v2", false, "If enabled requests to Alertmanager will utilize the V2 API.")
	f.IntVar(&cfg.NotificationQueueCapacity, "ruler.notification-queue-capacity", 10000, "Capacity of the queue for notifications to be sent to the Alertmanager.")
	f.DurationVar(&cfg.NotificationTimeout, "ruler.notification-timeout", 10*time.Second, "HTTP timeout duration when sending notifications to the Alertmanager.")

	f.DurationVar(&cfg.SearchPendingFor, "ruler.search-pending-for", 5*time.Minute, "Time to spend searching for a pending ruler when shutting down.")
	f.BoolVar(&cfg.EnableSharding, "ruler.enable-sharding", false, "Distribute rule evaluation using ring backend")
	f.DurationVar(&cfg.FlushCheckPeriod, "ruler.flush-period", 1*time.Minute, "Period with which to attempt to flush rule groups.")
	f.StringVar(&cfg.RulePath, "ruler.rule-path", "/rules", "file path to store temporary rule files for the prometheus rule managers")
	f.BoolVar(&cfg.EnableAPI, "experimental.ruler.enable-api", false, "Enable the ruler api")
	f.DurationVar(&cfg.OutageTolerance, "ruler.for-outage-tolerance", time.Hour, `Max time to tolerate outage for restoring "for" state of alert.`)
	f.DurationVar(&cfg.ForGracePeriod, "ruler.for-grace-period", 10*time.Minute, `Minimum duration between alert and restored "for" state. This is maintained only for alerts with configured "for" time greater than grace period.`)
	f.DurationVar(&cfg.ResendDelay, "ruler.resend-delay", time.Minute, `Minimum amount of time to wait before resending an alert to Alertmanager.`)
}

// Ruler evaluates rules.
type Ruler struct {
	services.Service

	cfg            Config
	notifierCfg    *config.Config
	managerFactory ManagerFactory

	lifecycler  *ring.BasicLifecycler
	ring        *ring.Ring
	subservices *services.Manager

	store  rules.RuleStore
	mapper *mapper

	// Structs for holding per-user Prometheus rules Managers
	// and a corresponding metrics struct
	userManagerMtx     sync.Mutex
	userManagers       map[string]*promRules.Manager
	userManagerMetrics *ManagerMetrics

	// Per-user notifiers with separate queues.
	notifiersMtx sync.Mutex
	notifiers    map[string]*rulerNotifier

	registry prometheus.Registerer
	logger   log.Logger
}

// NewRuler creates a new ruler from a distributor and chunk store.
func NewRuler(cfg Config, managerFactory ManagerFactory, reg prometheus.Registerer, logger log.Logger, ruleStore rules.RuleStore) (*Ruler, error) {
	ncfg, err := buildNotifierConfig(&cfg)
	if err != nil {
		return nil, err
	}

	userManagerMetrics := NewManagerMetrics()

	if reg != nil {
		reg.MustRegister(userManagerMetrics)
	}

	ruler := &Ruler{
		cfg:                cfg,
		notifierCfg:        ncfg,
		managerFactory:     managerFactory,
		notifiers:          map[string]*rulerNotifier{},
		store:              ruleStore,
		mapper:             newMapper(cfg.RulePath, logger),
		userManagers:       map[string]*promRules.Manager{},
		userManagerMetrics: userManagerMetrics,
		registry:           reg,
		logger:             logger,
	}

	if cfg.EnableSharding {
		ringStore, err := kv.NewClient(
			cfg.Ring.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(reg, "ruler"),
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
	lifecyclerCfg, err := r.cfg.Ring.ToLifecyclerConfig()
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's lifecycler config")
	}

	// Define lifecycler delegates in reverse order (last to be called defined first because they're
	// chained via "next delegate").
	delegate := ring.BasicLifecyclerDelegate(r)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, r.logger)
	delegate = ring.NewAutoForgetDelegate(r.cfg.Ring.HeartbeatTimeout*ringAutoForgetUnhealthyPeriods, delegate, r.logger)

	r.lifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ring.RulerRingKey, ring.RulerRingKey, ringStore, delegate, r.logger, r.registry)
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's lifecycler")
	}

	r.ring, err = ring.NewWithStoreClientAndStrategy(r.cfg.Ring.ToRingConfig(), ring.RulerRingKey, ring.RulerRingKey, ringStore, &ring.DefaultReplicationStrategy{})
	if err != nil {
		return errors.Wrap(err, "failed to initialize ruler's ring")
	}

	return nil
}

func (r *Ruler) starting(ctx context.Context) error {
	// If sharding is enabled, start the ruler ring subservices
	if r.cfg.EnableSharding {
		var err error
		r.subservices, err = services.NewManager(r.lifecycler, r.ring)
		if err == nil {
			err = services.StartManagerAndAwaitHealthy(ctx, r.subservices)
		}
		return errors.Wrap(err, "failed to start ruler's services")
	}

	// TODO: ideally, ruler would wait until its queryable is finished starting.
	return nil
}

// Stop stops the Ruler.
// Each function of the ruler is terminated before leaving the ring
func (r *Ruler) stopping(_ error) error {
	r.notifiersMtx.Lock()
	for _, n := range r.notifiers {
		n.stop()
	}
	r.notifiersMtx.Unlock()

	if r.subservices != nil {
		// subservices manages ring and lifecycler, if sharding was enabled.
		_ = services.StopManagerAndAwaitStopped(context.Background(), r.subservices)
	}

	level.Info(r.logger).Log("msg", "stopping user managers")
	wg := sync.WaitGroup{}
	r.userManagerMtx.Lock()
	for user, manager := range r.userManagers {
		level.Debug(r.logger).Log("msg", "shutting down user  manager", "user", user)
		wg.Add(1)
		go func(manager *promRules.Manager, user string) {
			manager.Stop()
			wg.Done()
			level.Debug(r.logger).Log("msg", "user manager shut down", "user", user)
		}(manager, user)
	}
	wg.Wait()
	r.userManagerMtx.Unlock()
	level.Info(r.logger).Log("msg", "all user managers stopped")
	return nil
}

// SendAlerts implements a rules.NotifyFunc for a Notifier.
// It filters any non-firing alerts from the input.
//
// Copied from Prometheus's main.go.
func SendAlerts(n *notifier.Manager, externalURL string) promRules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*promRules.Alert) {
		var res []*notifier.Alert

		for _, alert := range alerts {
			// Only send actually firing alerts.
			if alert.State == promRules.StatePending {
				continue
			}
			a := &notifier.Alert{
				StartsAt:     alert.FiredAt,
				Labels:       alert.Labels,
				Annotations:  alert.Annotations,
				GeneratorURL: externalURL + strutil.TableLinkForExpression(expr),
			}
			if !alert.ResolvedAt.IsZero() {
				a.EndsAt = alert.ResolvedAt
			}
			res = append(res, a)
		}

		if len(alerts) > 0 {
			n.Send(res...)
		}
	}
}

func (r *Ruler) getOrCreateNotifier(userID string) (*notifier.Manager, error) {
	r.notifiersMtx.Lock()
	defer r.notifiersMtx.Unlock()

	n, ok := r.notifiers[userID]
	if ok {
		return n.notifier, nil
	}

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"user": userID}, r.registry)
	reg = prometheus.WrapRegistererWithPrefix("cortex_", reg)
	n = newRulerNotifier(&notifier.Options{
		QueueCapacity: r.cfg.NotificationQueueCapacity,
		Registerer:    reg,
		Do: func(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
			// Note: The passed-in context comes from the Prometheus notifier
			// and does *not* contain the userID. So it needs to be added to the context
			// here before using the context to inject the userID into the HTTP request.
			ctx = user.InjectOrgID(ctx, userID)
			if err := user.InjectOrgIDIntoHTTPRequest(ctx, req); err != nil {
				return nil, err
			}
			// Jaeger complains the passed-in context has an invalid span ID, so start a new root span
			sp := ot.GlobalTracer().StartSpan("notify", ot.Tag{Key: "organization", Value: userID})
			defer sp.Finish()
			ctx = ot.ContextWithSpan(ctx, sp)
			_ = ot.GlobalTracer().Inject(sp.Context(), ot.HTTPHeaders, ot.HTTPHeadersCarrier(req.Header))
			return ctxhttp.Do(ctx, client, req)
		},
	}, util.Logger)

	go n.run()

	// This should never fail, unless there's a programming mistake.
	if err := n.applyConfig(r.notifierCfg); err != nil {
		return nil, err
	}

	r.notifiers[userID] = n
	return n.notifier, nil
}

func (r *Ruler) ownsRule(hash uint32) (bool, error) {
	rlrs, err := r.ring.Get(hash, ring.Read, []ring.IngesterDesc{})
	if err != nil {
		level.Warn(r.logger).Log("msg", "error reading ring to verify rule group ownership", "err", err)
		ringCheckErrors.Inc()
		return false, err
	}

	localAddr := r.lifecycler.GetInstanceAddr()

	if rlrs.Ingesters[0].Addr == localAddr {
		level.Debug(r.logger).Log("msg", "rule group owned", "owner_addr", rlrs.Ingesters[0].Addr, "addr", localAddr)
		return true, nil
	}
	level.Debug(r.logger).Log("msg", "rule group not owned, address does not match", "owner_addr", rlrs.Ingesters[0].Addr, "addr", localAddr)
	return false, nil
}

func (r *Ruler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.cfg.EnableSharding {
		r.ring.ServeHTTP(w, req)
	} else {
		var unshardedPage = `
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
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(unshardedPage))
		if err != nil {
			level.Error(r.logger).Log("msg", "unable to serve status page", "err", err)
		}
	}
}

func (r *Ruler) run(ctx context.Context) error {
	level.Info(r.logger).Log("msg", "ruler up and running")

	tick := time.NewTicker(r.cfg.PollInterval)
	defer tick.Stop()

	r.loadRules(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			r.loadRules(ctx)
			r.userManagerMtx.Lock()
			managersTotal.Set(float64(len(r.userManagers)))
			r.userManagerMtx.Unlock()
		}
	}
}

func (r *Ruler) loadRules(ctx context.Context) {
	ringHasher := fnv.New32a()

	configs, err := r.store.ListAllRuleGroups(ctx)
	if err != nil {
		level.Error(r.logger).Log("msg", "unable to poll for rules", "err", err)
		return
	}

	// Iterate through each users configuration and determine if the on-disk
	// configurations need to be updated
	for user, cfg := range configs {
		filteredGroups := store.RuleGroupList{}

		// If sharding is enabled, prune the rule group to only contain rules
		// this ruler is responsible for.
		if r.cfg.EnableSharding {
			for _, g := range cfg {
				id := g.User + "/" + g.Namespace + "/" + g.Name
				ringHasher.Reset()
				_, err = ringHasher.Write([]byte(id))
				if err != nil {
					level.Error(r.logger).Log("msg", "failed to create group for user", "user", user, "namespace", g.Namespace, "group", g.Name, "err", err)
					continue
				}
				hash := ringHasher.Sum32()
				owned, err := r.ownsRule(hash)
				if err != nil {
					level.Error(r.logger).Log("msg", "unable to verify rule group ownership ownership, will retry on the next poll", "err", err)
					return
				}
				if owned {
					filteredGroups = append(filteredGroups, g)
				}
			}
		} else {
			filteredGroups = cfg
		}

		r.syncManager(ctx, user, filteredGroups)
	}

	// Check for deleted users and remove them
	r.userManagerMtx.Lock()
	defer r.userManagerMtx.Unlock()
	for user, mngr := range r.userManagers {
		if _, exists := configs[user]; !exists {
			go mngr.Stop()
			delete(r.userManagers, user)
			level.Info(r.logger).Log("msg", "deleting rule manager", "user", user)
		}
	}

}

// syncManager maps the rule files to disk, detects any changes and will create/update the
// the users Prometheus Rules Manager.
func (r *Ruler) syncManager(ctx context.Context, user string, groups store.RuleGroupList) {
	// A lock is taken to ensure if syncManager is called concurrently, that each call
	// returns after the call map files and check for updates
	r.userManagerMtx.Lock()
	defer r.userManagerMtx.Unlock()

	// Map the files to disk and return the file names to be passed to the users manager if they
	// have been updated
	update, files, err := r.mapper.MapRules(user, groups.Formatted())
	if err != nil {
		level.Error(r.logger).Log("msg", "unable to map rule files", "user", user, "err", err)
		return
	}

	if update {
		level.Debug(r.logger).Log("msg", "updating rules", "user", "user")
		configUpdatesTotal.WithLabelValues(user).Inc()
		manager, exists := r.userManagers[user]
		if !exists {
			manager, err = r.newManager(ctx, user)
			if err != nil {
				configUpdateFailuresTotal.WithLabelValues(user, "rule-manager-creation-failure").Inc()
				level.Error(r.logger).Log("msg", "unable to create rule manager", "user", user, "err", err)
				return
			}
			// manager.Run() starts running the manager and blocks until Stop() is called.
			// Hence run it as another goroutine.
			go manager.Run()
			r.userManagers[user] = manager
		}
		err = manager.Update(r.cfg.EvaluationInterval, files, nil)
		if err != nil {
			configUpdateFailuresTotal.WithLabelValues(user, "rules-update-failure").Inc()
			level.Error(r.logger).Log("msg", "unable to update rule manager", "user", user, "err", err)
			return
		}
	}
}

// newManager creates a prometheus rule manager wrapped with a user id
// configured storage, appendable, notifier, and instrumentation
func (r *Ruler) newManager(ctx context.Context, userID string) (*promRules.Manager, error) {
	notifier, err := r.getOrCreateNotifier(userID)
	if err != nil {
		return nil, err
	}

	// Create a new Prometheus registry and register it within
	// our metrics struct for the provided user.
	reg := prometheus.NewRegistry()
	r.userManagerMetrics.AddUserRegistry(userID, reg)

	logger := log.With(r.logger, "user", userID)
	return r.managerFactory(ctx, userID, notifier, logger, reg), nil
}

// GetRules retrieves the running rules from this ruler and all running rulers in the ring if
// sharding is enabled
func (r *Ruler) GetRules(ctx context.Context) ([]*GroupStateDesc, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("no user id found in context")
	}

	if r.cfg.EnableSharding {
		return r.getShardedRules(ctx)
	}

	return r.getLocalRules(userID)
}

func (r *Ruler) getLocalRules(userID string) ([]*GroupStateDesc, error) {
	var groups []*promRules.Group
	r.userManagerMtx.Lock()
	if mngr, exists := r.userManagers[userID]; exists {
		groups = mngr.RuleGroups()
	}
	r.userManagerMtx.Unlock()

	groupDescs := make([]*GroupStateDesc, 0, len(groups))
	prefix := filepath.Join(r.cfg.RulePath, userID) + "/"

	for _, group := range groups {
		interval := group.Interval()

		// The mapped filename is url path escaped encoded to make handling `/` characters easier
		decodedNamespace, err := url.PathUnescape(strings.TrimPrefix(group.File(), prefix))
		if err != nil {
			return nil, errors.Wrap(err, "unable to decode rule filename")
		}

		groupDesc := &GroupStateDesc{
			Group: &rules.RuleGroupDesc{
				Name:      group.Name(),
				Namespace: string(decodedNamespace),
				Interval:  interval,
				User:      userID,
			},
			EvaluationTimestamp: group.GetEvaluationTimestamp(),
			EvaluationDuration:  group.GetEvaluationDuration(),
		}
		for _, r := range group.Rules() {
			lastError := ""
			if r.LastError() != nil {
				lastError = r.LastError().Error()
			}

			var ruleDesc *RuleStateDesc
			switch rule := r.(type) {
			case *promRules.AlertingRule:
				rule.ActiveAlerts()
				alerts := []*AlertStateDesc{}
				for _, a := range rule.ActiveAlerts() {
					alerts = append(alerts, &AlertStateDesc{
						State:       a.State.String(),
						Labels:      client.FromLabelsToLabelAdapters(a.Labels),
						Annotations: client.FromLabelsToLabelAdapters(a.Annotations),
						Value:       a.Value,
						ActiveAt:    a.ActiveAt,
						FiredAt:     a.FiredAt,
						ResolvedAt:  a.ResolvedAt,
						LastSentAt:  a.LastSentAt,
						ValidUntil:  a.ValidUntil,
					})
				}
				ruleDesc = &RuleStateDesc{
					Rule: &rules.RuleDesc{
						Expr:        rule.Query().String(),
						Alert:       rule.Name(),
						For:         rule.HoldDuration(),
						Labels:      client.FromLabelsToLabelAdapters(rule.Labels()),
						Annotations: client.FromLabelsToLabelAdapters(rule.Annotations()),
					},
					State:               rule.State().String(),
					Health:              string(rule.Health()),
					LastError:           lastError,
					Alerts:              alerts,
					EvaluationTimestamp: rule.GetEvaluationTimestamp(),
					EvaluationDuration:  rule.GetEvaluationDuration(),
				}
			case *promRules.RecordingRule:
				ruleDesc = &RuleStateDesc{
					Rule: &rules.RuleDesc{
						Record: rule.Name(),
						Expr:   rule.Query().String(),
						Labels: client.FromLabelsToLabelAdapters(rule.Labels()),
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
		groupDescs = append(groupDescs, groupDesc)
	}
	return groupDescs, nil
}

func (r *Ruler) getShardedRules(ctx context.Context) ([]*GroupStateDesc, error) {
	rulers, err := r.ring.GetAll(ring.Read)
	if err != nil {
		return nil, err
	}

	ctx, err = user.InjectIntoGRPCRequest(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to inject user ID into grpc request, %v", err)
	}

	rgs := []*GroupStateDesc{}

	for _, rlr := range rulers.Ingesters {
		dialOpts, err := r.cfg.ClientTLSConfig.GetGRPCDialOptions()
		if err != nil {
			return nil, err
		}
		conn, err := grpc.Dial(rlr.Addr, dialOpts...)
		if err != nil {
			return nil, err
		}
		cc := NewRulerClient(conn)
		newGrps, err := cc.Rules(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve rules from other rulers, %v", err)
		}
		rgs = append(rgs, newGrps.Groups...)
	}

	return rgs, nil
}

// Rules implements the rules service
func (r *Ruler) Rules(ctx context.Context, in *RulesRequest) (*RulesResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("no user id found in context")
	}

	groupDescs, err := r.getLocalRules(userID)
	if err != nil {
		return nil, err
	}

	return &RulesResponse{Groups: groupDescs}, nil
}
