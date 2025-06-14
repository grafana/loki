package base

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/notifier"
	promRules "github.com/prometheus/prometheus/rules"
	"go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/context/ctxhttp"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
)

type DefaultMultiTenantManager struct {
	cfg            Config
	notifiersCfg   map[string]*config.Config
	managerFactory ManagerFactory
	limits         RulesLimits

	mapper *mapper

	// Structs for holding per-user Prometheus rules Managers
	// and a corresponding metrics struct
	userManagerMtx     sync.Mutex
	userManagers       map[string]RulesManager
	userManagerMetrics *ManagerMetrics

	// Per-user notifiers with separate queues.
	notifiersMtx sync.Mutex
	notifiers    map[string]*rulerNotifier

	managersTotal                 prometheus.Gauge
	lastReloadSuccessful          *prometheus.GaugeVec
	lastReloadSuccessfulTimestamp *prometheus.GaugeVec
	configUpdatesTotal            *prometheus.CounterVec
	registry                      prometheus.Registerer
	logger                        log.Logger
	metricsNamespace              string
}

func NewDefaultMultiTenantManager(cfg Config, managerFactory ManagerFactory, reg prometheus.Registerer, logger log.Logger, limits RulesLimits, metricsNamespace string) (*DefaultMultiTenantManager, error) {
	userManagerMetrics := NewManagerMetrics(cfg.DisableRuleGroupLabel, func(k, v string) string {
		// When "by-rule" sharding is enabled, each rule group is assigned a unique name to work around some of Prometheus'
		// assumptions, and metrics are exported based on these rule group names. If we kept these unique rule group names
		// in place, this would explode cardinality.
		if k == RuleGroupLabel {
			return RemoveRuleTokenFromGroupName(v)
		}

		return v

	}, metricsNamespace)
	if reg != nil {
		reg.MustRegister(userManagerMetrics)
	}

	return &DefaultMultiTenantManager{
		cfg:                cfg,
		notifiersCfg:       map[string]*config.Config{},
		managerFactory:     managerFactory,
		limits:             limits,
		notifiers:          map[string]*rulerNotifier{},
		mapper:             newMapper(cfg.RulePath, logger),
		userManagers:       map[string]RulesManager{},
		userManagerMetrics: userManagerMetrics,
		metricsNamespace:   metricsNamespace,
		managersTotal: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_managers_total",
			Help:      "Total number of managers registered and running in the ruler",
		}),
		lastReloadSuccessful: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_config_last_reload_successful",
			Help:      "Boolean set to 1 whenever the last configuration reload attempt was successful.",
		}, []string{"user"}),
		lastReloadSuccessfulTimestamp: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_config_last_reload_successful_seconds",
			Help:      "Timestamp of the last successful configuration reload.",
		}, []string{"user"}),
		configUpdatesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "ruler_config_updates_total",
			Help:      "Total number of config updates triggered by a user",
		}, []string{"user"}),
		registry: reg,
		logger:   logger,
	}, nil
}

func (r *DefaultMultiTenantManager) SyncRuleGroups(ctx context.Context, ruleGroups map[string]rulespb.RuleGroupList) {
	// A lock is taken to ensure if this function is called concurrently, then each call
	// returns after the call map files and check for updates
	r.userManagerMtx.Lock()
	defer r.userManagerMtx.Unlock()

	for userID, ruleGroup := range ruleGroups {
		r.syncRulesToManager(ctx, userID, ruleGroup)
	}

	// Check for deleted users and remove them
	for userID, mngr := range r.userManagers {
		if _, exists := ruleGroups[userID]; !exists {
			go mngr.Stop()
			delete(r.userManagers, userID)

			r.mapper.cleanupUser(userID)
			r.lastReloadSuccessful.DeleteLabelValues(userID)
			r.lastReloadSuccessfulTimestamp.DeleteLabelValues(userID)
			r.configUpdatesTotal.DeleteLabelValues(userID)
			r.userManagerMetrics.RemoveUserRegistry(userID)
			level.Info(r.logger).Log("msg", "deleted rule manager and local rule files", "user", userID)
		}
	}

	r.managersTotal.Set(float64(len(r.userManagers)))
}

// syncRulesToManager maps the rule files to disk, detects any changes and will create/update the
// the users Prometheus Rules Manager.
func (r *DefaultMultiTenantManager) syncRulesToManager(ctx context.Context, user string, groups rulespb.RuleGroupList) {
	// Map the files to disk and return the file names to be passed to the users manager if they
	// have been updated
	update, files, err := r.mapper.MapRules(user, groups.Formatted())
	if err != nil {
		r.lastReloadSuccessful.WithLabelValues(user).Set(0)
		level.Error(r.logger).Log("msg", "unable to map rule files", "user", user, "err", err)
		return
	}

	manager, exists := r.userManagers[user]
	if !exists || update {
		level.Debug(r.logger).Log("msg", "updating rules", "user", user)
		r.configUpdatesTotal.WithLabelValues(user).Inc()
		if !exists {
			level.Debug(r.logger).Log("msg", "creating rule manager for user", "user", user)
			manager, err = r.newManager(ctx, user)
			if err != nil {
				r.lastReloadSuccessful.WithLabelValues(user).Set(0)
				level.Error(r.logger).Log("msg", "unable to create rule manager", "user", user, "err", err)
				return
			}
			// manager.Run() starts running the manager and blocks until Stop() is called.
			// Hence run it as another goroutine.
			go manager.Run()
			r.userManagers[user] = manager
		}
		err = manager.Update(r.cfg.EvaluationInterval, files, r.cfg.ExternalLabels, r.cfg.ExternalURL.String(), nil)
		if err != nil {
			r.lastReloadSuccessful.WithLabelValues(user).Set(0)
			level.Error(r.logger).Log("msg", "unable to update rule manager", "user", user, "err", err)
			return
		}

		r.lastReloadSuccessful.WithLabelValues(user).Set(1)
		r.lastReloadSuccessfulTimestamp.WithLabelValues(user).SetToCurrentTime()
	}
}

// newManager creates a prometheus rule manager wrapped with a user id
// configured storage, appendable, notifier, and instrumentation
func (r *DefaultMultiTenantManager) newManager(ctx context.Context, userID string) (RulesManager, error) {
	notifier, err := r.getOrCreateNotifier(userID)
	if err != nil {
		return nil, err
	}

	// Create a new Prometheus registry and register it within
	// our metrics struct for the provided user.
	reg := prometheus.NewRegistry()
	r.userManagerMetrics.AddUserRegistry(userID, reg)

	return r.managerFactory(ctx, userID, notifier, r.logger, reg), nil
}

func (r *DefaultMultiTenantManager) getOrCreateNotifier(userID string) (*notifier.Manager, error) {
	r.notifiersMtx.Lock()
	defer r.notifiersMtx.Unlock()

	n, ok := r.notifiers[userID]
	if ok {
		return n.notifier, nil
	}

	nCfg, ok := r.notifiersCfg[userID]
	if !ok {
		amCfg := r.cfg.AlertManagerConfig

		// Apply the tenant specific alertmanager config when defined
		if amOverrides := r.limits.RulerAlertManagerConfig(userID); amOverrides != nil {
			amCfg = applyAlertmanagerDefaults(*amOverrides)
		}

		var err error
		nCfg, err = buildNotifierConfig(&amCfg, r.cfg.ExternalLabels)
		if err != nil {
			return nil, fmt.Errorf("failed to build notifier config for tenant %s: %w", userID, err)
		}

		if nCfg != nil {
			r.notifiersCfg[userID] = nCfg
		}
	}

	reg := prometheus.WrapRegistererWith(prometheus.Labels{"user": userID}, r.registry)
	reg = prometheus.WrapRegistererWithPrefix(r.metricsNamespace+"_", reg)
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

			_, sp := tracer.Start(context.Background(), "notify", trace.WithAttributes(attribute.String("organization", userID)))
			defer sp.End()
			ctx = trace.ContextWithSpan(ctx, sp)
			otelhttptrace.Inject(ctx, req)
			return ctxhttp.Do(ctx, client, req)
		},
	}, log.With(r.logger, "user", userID))

	n.run()

	// This should never fail, unless there's a programming mistake.
	if err := n.applyConfig(nCfg); err != nil {
		return nil, err
	}

	r.notifiers[userID] = n
	return n.notifier, nil
}

func (r *DefaultMultiTenantManager) GetRules(userID string) []*promRules.Group {
	var groups []*promRules.Group
	r.userManagerMtx.Lock()
	if mngr, exists := r.userManagers[userID]; exists {
		groups = mngr.RuleGroups()
	}
	r.userManagerMtx.Unlock()
	return groups
}

func (r *DefaultMultiTenantManager) Stop() {
	r.notifiersMtx.Lock()
	for _, n := range r.notifiers {
		n.stop()
	}
	r.notifiersMtx.Unlock()

	level.Info(r.logger).Log("msg", "stopping user managers")
	wg := sync.WaitGroup{}
	r.userManagerMtx.Lock()
	for user, manager := range r.userManagers {
		level.Debug(r.logger).Log("msg", "shutting down user  manager", "user", user)
		wg.Add(1)
		go func(manager RulesManager, user string) {
			manager.Stop()
			wg.Done()
			level.Debug(r.logger).Log("msg", "user manager shut down", "user", user)
		}(manager, user)
	}
	wg.Wait()
	r.userManagerMtx.Unlock()
	level.Info(r.logger).Log("msg", "all user managers stopped")

	// cleanup user rules directories
	r.mapper.cleanup()
}

func (*DefaultMultiTenantManager) ValidateRuleGroup(g rulefmt.RuleGroup) []error {
	var errs []error

	if g.Name == "" {
		errs = append(errs, errors.New("invalid rules config: rule group name must not be empty"))
		return errs
	}

	if len(g.Rules) == 0 {
		errs = append(errs, fmt.Errorf("invalid rules config: rule group '%s' has no rules", g.Name))
		return errs
	}

	for i, r := range g.Rules {
		ruleNode := rulefmt.RuleNode{
			Record: yaml.Node{Value: r.Record},
			Alert:  yaml.Node{Value: r.Alert},
			Expr:   yaml.Node{Value: r.Expr},
		}
		for _, err := range r.Validate(ruleNode) {
			var ruleName string
			if r.Alert != "" {
				ruleName = r.Alert
			} else {
				ruleName = r.Record
			}
			errs = append(errs, &rulefmt.Error{
				Group:    g.Name,
				Rule:     i,
				RuleName: ruleName,
				Err:      err,
			})
		}
	}

	return errs
}
