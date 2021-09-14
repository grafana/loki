package ruler

import (
	"bytes"
	"context"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/template"
	"github.com/weaveworks/common/user"
	yaml "gopkg.in/yaml.v3"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/pkg/ruler/storage/instance"
	"github.com/grafana/loki/pkg/ruler/storage/wal"
)

// RulesLimits is the one function we need from limits.Overrides, and
// is here to limit coupling.
type RulesLimits interface {
	ruler.RulesLimits

	RulerRemoteWrite(userID string, base RemoteWriteConfig) RemoteWriteConfig
}

// engineQueryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func engineQueryFunc(logger log.Logger, engine *logql.Engine, overrides RulesLimits, manager instance.Manager, userID string) rules.QueryFunc {
	return rules.QueryFunc(func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		// check if storage instance is ready; if not, fail the rule evaluation;
		// we do this to prevent an attempt to append new samples before the WAL appender is ready
		if inst, err := manager.GetInstance(userID); err != nil || !inst.Ready() {
			return nil, errNotReady
		}

		adjusted := t.Add(-overrides.EvaluationDelay(userID))
		params := logql.NewLiteralParams(
			qs,
			adjusted,
			adjusted,
			0,
			0,
			logproto.FORWARD,
			0,
			nil,
		)
		q := engine.Query(params)

		res, err := q.Exec(ctx)
		if err != nil {
			return nil, err
		}
		switch v := res.Data.(type) {
		case promql.Vector:
			return v, nil
		case promql.Scalar:
			return promql.Vector{promql.Sample{
				Point:  promql.Point(v),
				Metric: labels.Labels{},
			}}, nil
		default:
			return nil, errors.New("rule result is not a vector or scalar")
		}
	})
}

// MultiTenantManagerAdapter will wrap a MultiTenantManager which validates loki rules
func MultiTenantManagerAdapter(mgr ruler.MultiTenantManager) ruler.MultiTenantManager {
	return &MultiTenantManager{mgr}
}

// MultiTenantManager wraps a cortex MultiTenantManager but validates loki rules
type MultiTenantManager struct {
	ruler.MultiTenantManager
}

// ValidateRuleGroup validates a rulegroup
func (m *MultiTenantManager) ValidateRuleGroup(grp rulefmt.RuleGroup) []error {
	return ValidateGroups(grp)
}

type storageRegistry struct {
	sync.RWMutex

	logger  log.Logger
	manager instance.Manager

	appenderReady *prometheus.GaugeVec
}

func newStorageRegistry(logger log.Logger, appenderReady *prometheus.GaugeVec) *storageRegistry {
	return &storageRegistry{
		logger:        logger,
		appenderReady: appenderReady,
	}
}

func (r *storageRegistry) Set(manager instance.Manager) {
	r.Lock()
	defer r.Unlock()
	r.manager = manager
}

func (r *storageRegistry) Get(tenant string) storage.Storage {
	r.RLock()
	defer r.RUnlock()

	ready := r.appenderReady.WithLabelValues(tenant)

	if r.manager == nil {
		// TODO error handling
		ready.Set(0)
		return nil
	}

	inst, err := r.manager.GetInstance(tenant)
	if err != nil {
		// TODO error handling
		ready.Set(0)
		return nil
	}

	i, ok := inst.(*instance.Instance)
	if !ok {
		// TODO error handling
		ready.Set(0)
		return nil
	}

	if !i.Ready() {
		ready.Set(0)
		return nil
	}

	ready.Set(1)
	return i.Storage()
}

func (r *storageRegistry) Appender(ctx context.Context) storage.Appender {
	tenant, _ := user.ExtractOrgID(ctx)

	inst := r.Get(tenant)
	if inst == nil {
		level.Warn(r.logger).Log("tenant", tenant, "msg", "not ready")
		return notReadyAppender{}
	}

	return inst.Appender(ctx)
}

type notReadyAppender struct{}

var errNotReady = errors.New("appender not ready")

func (n notReadyAppender) Append(ref uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, errNotReady
}

func (n notReadyAppender) AppendExemplar(ref uint64, l labels.Labels, e exemplar.Exemplar) (uint64, error) {
	return 0, errNotReady
}

func (n notReadyAppender) Commit() error { return errNotReady }

func (n notReadyAppender) Rollback() error { return errNotReady }

const MetricsPrefix = "loki_ruler_wal_"

var walCleaner *cleaner.WALCleaner

type TenantWALManager struct {
	logger log.Logger
	reg    prometheus.Registerer
}

func (t *TenantWALManager) newInstance(c instance.Config) (instance.ManagedInstance, error) {
	reg := prometheus.WrapRegistererWith(prometheus.Labels{
		"tenant": c.Tenant,
	}, t.reg)

	// TODO create new metrics each time? (probably yes, but do they clash when registering?)
	// TODO how will unregistering work when a rule group is removed?

	// create metrics here and pass down
	return instance.New(reg, c, wal.NewMetrics(reg), t.logger)
}

func MemstoreTenantManager(cfg Config, engine *logql.Engine, overrides RulesLimits, logger log.Logger, reg prometheus.Registerer) ruler.ManagerFactory {
	var msMetrics *memstoreMetrics

	reg = prometheus.WrapRegistererWithPrefix(MetricsPrefix, reg)

	appenderReady := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "appender_ready",
	}, []string{"tenant"})

	reg.MustRegister(appenderReady)

	walRegistry := newStorageRegistry(log.With(logger, "storage", "registry"), appenderReady)

	instMetrics := instance.NewMetrics(reg)
	msMetrics = newMemstoreMetrics(reg)

	man := &TenantWALManager{
		reg:    reg,
		logger: log.With(logger, "manager", "tenant-wal"),
	}

	manager := instance.NewBasicManager(instance.BasicManagerConfig{
		InstanceRestartBackoff: time.Second,
	}, instMetrics, log.With(logger, "manager", "tenant-wal"), man.newInstance)


	if walCleaner != nil {
		walCleaner.Stop()
	}

	walCleaner = cleaner.NewWALCleaner(
		logger,
		manager,
		cleaner.NewMetrics(reg),
		cfg.WAL.Path,
		cfg.WALCleaner)

	walRegistry.Set(manager)

	return func(
		ctx context.Context,
		userID string,
		notifier *notifier.Manager,
		logger log.Logger,
		reg prometheus.Registerer,
	) ruler.RulesManager {
		setupStorage(cfg, manager, userID, overrides)

		logger = log.With(logger, "user", userID)
		queryFunc := engineQueryFunc(logger, engine, overrides, manager, userID)
		memStore := NewMemStore(userID, queryFunc, msMetrics, 5*time.Minute, log.With(logger, "subcomponent", "MemStore"))

		mgr := rules.NewManager(&rules.ManagerOptions{
			Appendable:      walRegistry,
			Queryable:       memStore,
			QueryFunc:       queryFunc,
			Context:         user.InjectOrgID(ctx, userID),
			ExternalURL:     cfg.ExternalURL.URL,
			NotifyFunc:      ruler.SendAlerts(notifier, cfg.ExternalURL.URL.String()),
			Logger:          logger,
			Registerer:      reg,
			OutageTolerance: cfg.OutageTolerance,
			ForGracePeriod:  cfg.ForGracePeriod,
			ResendDelay:     cfg.ResendDelay,
			GroupLoader:     GroupLoader{},
		})

		// initialize memStore, bound to the manager's alerting rules
		memStore.Start(mgr)

		return mgr
	}
}

func setupStorage(cfg Config, manager instance.Manager, tenant string, overrides RulesLimits) {
	// make a copy
	conf := cfg.WAL

	conf.Name = tenant
	conf.Tenant = tenant

	// we don't need to send metadata - we have no scrape targets
	cfg.RemoteWrite.Client.MetadataConfig.Send = false

	// retrieve remote-write config for this tenant, using the global remote-write for defaults
	rwCfg := overrides.RulerRemoteWrite(tenant, cfg.RemoteWrite)

	// TODO(dannyk): implement multiple RW configs
	if rwCfg.Enabled {
		conf.RemoteWrite = []*config.RemoteWriteConfig{
			configureRemoteWrite(rwCfg, tenant),
		}
	} else {
		// reset if remote-write is disabled at runtime
		conf.RemoteWrite = []*config.RemoteWriteConfig{}
	}

	if err := manager.ApplyConfig(conf); err != nil {
		// TODO: don't panic
		panic(err)
	}
}

func configureRemoteWrite(cfg RemoteWriteConfig, tenant string) *config.RemoteWriteConfig {
	// always inject the X-Org-ScopeId header for multi-tenant metrics backends

	if cfg.Client.Headers == nil {
		cfg.Client.Headers = make(map[string]string)
	}

	cfg.Client.Headers[user.OrgIDHeaderName] = tenant

	return &cfg.Client
}

type GroupLoader struct{}

func (GroupLoader) Parse(query string) (parser.Expr, error) {
	expr, err := logql.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	return exprAdapter{expr}, nil
}

func (g GroupLoader) Load(identifier string) (*rulefmt.RuleGroups, []error) {
	b, err := ioutil.ReadFile(identifier)
	if err != nil {
		return nil, []error{errors.Wrap(err, identifier)}
	}
	rgs, errs := g.parseRules(b)
	for i := range errs {
		errs[i] = errors.Wrap(errs[i], identifier)
	}
	return rgs, errs
}

func (GroupLoader) parseRules(content []byte) (*rulefmt.RuleGroups, []error) {
	var (
		groups rulefmt.RuleGroups
		errs   []error
	)

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	if err := decoder.Decode(&groups); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, errs
	}

	return &groups, ValidateGroups(groups.Groups...)
}

func ValidateGroups(grps ...rulefmt.RuleGroup) (errs []error) {
	set := map[string]struct{}{}

	for i, g := range grps {
		if g.Name == "" {
			errs = append(errs, errors.Errorf("group %d: Groupname must not be empty", i))
		}

		if _, ok := set[g.Name]; ok {
			errs = append(
				errs,
				errors.Errorf("groupname: \"%s\" is repeated in the same file", g.Name),
			)
		}

		set[g.Name] = struct{}{}

		for _, r := range g.Rules {
			if err := validateRuleNode(&r); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

func validateRuleNode(r *rulefmt.RuleNode) error {
	if r.Record.Value != "" && r.Alert.Value != "" {
		return errors.Errorf("only one of 'record' and 'alert' must be set")
	}

	if r.Record.Value == "" && r.Alert.Value == "" {
		return errors.Errorf("one of 'record' or 'alert' must be set")
	}

	if r.Record.Value != "" && r.Alert.Value != "" {
		return errors.Errorf("only one of 'record' or 'alert' must be set")
	}

	if r.Expr.Value == "" {
		return errors.Errorf("field 'expr' must be set in rule")
	} else if _, err := logql.ParseExpr(r.Expr.Value); err != nil {
		return errors.Wrapf(err, "could not parse expression")
	}

	if r.Record.Value != "" {
		if len(r.Annotations) > 0 {
			return errors.Errorf("invalid field 'annotations' in recording rule")
		}
		if r.For != 0 {
			return errors.Errorf("invalid field 'for' in recording rule")
		}
		if !model.IsValidMetricName(model.LabelValue(r.Record.Value)) {
			return errors.Errorf("invalid recording rule name: %s", r.Record.Value)
		}
	}

	for k, v := range r.Labels {
		if !model.LabelName(k).IsValid() || k == model.MetricNameLabel {
			return errors.Errorf("invalid label name: %s", k)
		}

		if !model.LabelValue(v).IsValid() {
			return errors.Errorf("invalid label value: %s", v)
		}
	}

	for k := range r.Annotations {
		if !model.LabelName(k).IsValid() {
			return errors.Errorf("invalid annotation name: %s", k)
		}
	}

	for _, err := range testTemplateParsing(r) {
		return err
	}

	return nil
}

// testTemplateParsing checks if the templates used in labels and annotations
// of the alerting rules are parsed correctly.
func testTemplateParsing(rl *rulefmt.RuleNode) (errs []error) {
	if rl.Alert.Value == "" {
		// Not an alerting rule.
		return errs
	}

	// Trying to parse templates.
	tmplData := template.AlertTemplateData(map[string]string{}, map[string]string{}, "", 0)
	defs := []string{
		"{{$labels := .Labels}}",
		"{{$externalLabels := .ExternalLabels}}",
		"{{$value := .Value}}",
	}
	parseTest := func(text string) error {
		tmpl := template.NewTemplateExpander(
			context.TODO(),
			strings.Join(append(defs, text), ""),
			"__alert_"+rl.Alert.Value,
			tmplData,
			model.Time(timestamp.FromTime(time.Now())),
			nil,
			nil,
		)
		return tmpl.ParseTest()
	}

	// Parsing Labels.
	for k, val := range rl.Labels {
		err := parseTest(val)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "label %q", k))
		}
	}

	// Parsing Annotations.
	for k, val := range rl.Annotations {
		err := parseTest(val)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "annotation %q", k))
		}
	}

	return errs
}

// Allows logql expressions to be treated as promql expressions by the prometheus rules pkg.
type exprAdapter struct {
	logql.Expr
}

func (exprAdapter) PositionRange() parser.PositionRange { return parser.PositionRange{} }
func (exprAdapter) PromQLExpr()                         {}
func (exprAdapter) Type() parser.ValueType              { return parser.ValueType("unimplemented") }
