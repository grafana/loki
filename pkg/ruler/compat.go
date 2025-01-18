package ruler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/sigv4"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/promql/parser/posrange"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/template"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	ruler "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
	rulerutil "github.com/grafana/loki/v3/pkg/ruler/util"
	"github.com/grafana/loki/v3/pkg/util"
)

// RulesLimits is the one function we need from limits.Overrides, and
// is here to limit coupling.
type RulesLimits interface {
	ruler.RulesLimits

	RulerRemoteWriteDisabled(userID string) bool
	RulerRemoteWriteURL(userID string) string
	RulerRemoteWriteTimeout(userID string) time.Duration
	RulerRemoteWriteHeaders(userID string) map[string]string
	RulerRemoteWriteRelabelConfigs(userID string) []*rulerutil.RelabelConfig
	RulerRemoteWriteConfig(userID string, id string) *config.RemoteWriteConfig
	RulerRemoteWriteQueueCapacity(userID string) int
	RulerRemoteWriteQueueMinShards(userID string) int
	RulerRemoteWriteQueueMaxShards(userID string) int
	RulerRemoteWriteQueueMaxSamplesPerSend(userID string) int
	RulerRemoteWriteQueueBatchSendDeadline(userID string) time.Duration
	RulerRemoteWriteQueueMinBackoff(userID string) time.Duration
	RulerRemoteWriteQueueMaxBackoff(userID string) time.Duration
	RulerRemoteWriteQueueRetryOnRateLimit(userID string) bool
	RulerRemoteWriteSigV4Config(userID string) *sigv4.SigV4Config

	RulerRemoteEvaluationTimeout(userID string) time.Duration
	RulerRemoteEvaluationMaxResponseSize(userID string) int64
}

// queryFunc returns a new query function using the rules.EngineQueryFunc function
// and passing an altered timestamp.
func queryFunc(evaluator Evaluator, checker readyChecker, userID string, logger log.Logger) rules.QueryFunc {
	return func(ctx context.Context, qs string, t time.Time) (promql.Vector, error) {
		hash := util.HashedQuery(qs)
		detail := rules.FromOriginContext(ctx)
		detailLog := log.With(logger, "rule_name", detail.Name, "rule_type", detail.Kind, "query", qs, "query_hash", hash)

		level.Info(detailLog).Log("msg", "evaluating rule")

		// check if storage instance is ready; if not, fail the rule evaluation;
		// we do this to prevent an attempt to append new samples before the WAL appender is ready
		if !checker.isReady(userID) {
			return nil, errNotReady
		}

		// Extract rule details
		ruleName := detail.Name
		ruleType := detail.Kind

		// Add rule details to context
		ctx = AddRuleDetailsToContext(ctx, ruleName, ruleType)
		res, err := evaluator.Eval(ctx, qs, t)

		if err != nil {
			level.Error(detailLog).Log("msg", "rule evaluation failed", "err", err)
			return nil, fmt.Errorf("rule evaluation failed: %w", err)
		}
		switch v := res.Data.(type) {
		case promql.Vector:
			return v, nil
		case promql.Scalar:
			return promql.Vector{promql.Sample{
				T: v.T, F: v.V,
				Metric: labels.Labels{},
			}}, nil
		default:
			level.Error(detailLog).Log("msg", "rule result is not a vector or scalar", "err", err)
			return nil, errors.New("rule result is not a vector or scalar")
		}
	}
}

// MultiTenantManagerAdapter will wrap a MultiTenantManager which validates loki rules
func MultiTenantManagerAdapter(mgr ruler.MultiTenantManager) ruler.MultiTenantManager {
	return &MultiTenantManager{inner: mgr}
}

// MultiTenantManager wraps a cortex MultiTenantManager but validates loki rules
type MultiTenantManager struct {
	inner ruler.MultiTenantManager
}

func (m *MultiTenantManager) SyncRuleGroups(ctx context.Context, ruleGroups map[string]rulespb.RuleGroupList) {
	m.inner.SyncRuleGroups(ctx, ruleGroups)
}

func (m *MultiTenantManager) GetRules(userID string) []*rules.Group {
	return m.inner.GetRules(userID)
}

func (m *MultiTenantManager) Stop() {
	if registry != nil {
		registry.stop()
	}

	m.inner.Stop()
}

// ValidateRuleGroup validates a rulegroup
func (m *MultiTenantManager) ValidateRuleGroup(grp rulefmt.RuleGroup) []error {
	return ValidateGroups(grp)
}

// MetricsPrefix defines the prefix to use for all metrics in this package
const MetricsPrefix = "loki_ruler_wal_"

var registry storageRegistry

func MultiTenantRuleManager(cfg Config, evaluator Evaluator, overrides RulesLimits, logger log.Logger, reg prometheus.Registerer) ruler.ManagerFactory {
	reg = prometheus.WrapRegistererWithPrefix(MetricsPrefix, reg)

	registry = newWALRegistry(log.With(logger, "storage", "registry"), reg, cfg, overrides)

	return func(
		ctx context.Context,
		userID string,
		notifier *notifier.Manager,
		logger log.Logger,
		reg prometheus.Registerer,
	) ruler.RulesManager {
		registry.configureTenantStorage(userID)

		logger = log.With(logger, "user", userID)
		queryFn := queryFunc(evaluator, registry, userID, logger)
		memStore := NewMemStore(userID, queryFn, newMemstoreMetrics(reg), 5*time.Minute, log.With(logger, "subcomponent", "MemStore"))

		// GroupLoader builds a cache of the rules as they're loaded by the
		// manager.This is used to back the memstore
		groupLoader := NewCachingGroupLoader(GroupLoader{})

		mgr := rules.NewManager(&rules.ManagerOptions{
			Appendable:               registry,
			Queryable:                memStore,
			QueryFunc:                queryFn,
			Context:                  user.InjectOrgID(ctx, userID),
			ExternalURL:              cfg.ExternalURL.URL,
			NotifyFunc:               ruler.SendAlerts(notifier, cfg.ExternalURL.URL.String(), cfg.DatasourceUID),
			Logger:                   logger,
			Registerer:               reg,
			OutageTolerance:          cfg.OutageTolerance,
			ForGracePeriod:           cfg.ForGracePeriod,
			ResendDelay:              cfg.ResendDelay,
			GroupLoader:              groupLoader,
			RuleDependencyController: &noopRuleDependencyController{},
		})

		cachingManager := &CachingRulesManager{
			manager:     mgr,
			groupLoader: groupLoader,
		}

		memStore.Start(groupLoader)

		return cachingManager
	}
}

// CachingRulesManager holds a CachingGroupLoader to make sure the GroupLoader
// has consistent state after update operations. Manager needs to hold the same
// caching grouploader
type CachingRulesManager struct {
	manager     ruler.RulesManager
	groupLoader *CachingGroupLoader
}

// Update reconciles the state of the CachingGroupLoader after a manager.Update.
// The GroupLoader is mutated as part of a call to Update but it might still
// contain removed files. Update tells the loader which files to keep
func (m *CachingRulesManager) Update(interval time.Duration, files []string, externalLabels labels.Labels, externalURL string, ruleGroupPostProcessFunc rules.GroupEvalIterationFunc) error {
	err := m.manager.Update(interval, files, externalLabels, externalURL, ruleGroupPostProcessFunc)
	if err != nil {
		return err
	}

	m.groupLoader.Prune(files)
	return nil
}

func (m *CachingRulesManager) Run() {
	m.manager.Run()
}

func (m *CachingRulesManager) Stop() {
	m.manager.Stop()
}

func (m *CachingRulesManager) RuleGroups() []*rules.Group {
	return m.manager.RuleGroups()
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
			if err := validateRuleNode(&r, g.Name); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

func validateRuleNode(r *rulefmt.RuleNode, groupName string) error {
	if r.Record.Value != "" && r.Alert.Value != "" {
		return errors.Errorf("only one of 'record' and 'alert' must be set")
	}

	if r.Record.Value == "" && r.Alert.Value == "" {
		return errors.Errorf("one of 'record' or 'alert' must be set")
	}

	if r.Expr.Value == "" {
		return errors.Errorf("field 'expr' must be set in rule")
	} else if _, err := syntax.ParseExpr(r.Expr.Value); err != nil {
		if r.Record.Value != "" {
			return errors.Wrapf(err, "could not parse expression for record '%s' in group '%s'", r.Record.Value, groupName)
		}
		return errors.Wrapf(err, "could not parse expression for alert '%s' in group '%s'", r.Alert.Value, groupName)
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
	tmplData := template.AlertTemplateData(map[string]string{}, map[string]string{}, "", promql.Sample{})
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
	syntax.Expr
}

func (exprAdapter) PositionRange() posrange.PositionRange { return posrange.PositionRange{} }
func (exprAdapter) PromQLExpr()                           {}
func (exprAdapter) Type() parser.ValueType                { return parser.ValueType("unimplemented") }
func (exprAdapter) Pretty(_ int) string                   { return "" }

type noopRuleDependencyController struct{}

// Prometheus rules manager calls AnalyseRules to determine the dependents and dependencies of a rule
// which it then uses to decide if a rule within a group is eligible for concurrent execution.
// AnalyseRules is a noop for Loki since there is no dependency relation between rules.
func (*noopRuleDependencyController) AnalyseRules([]rules.Rule) {
	// Do nothing
}

// Define context keys to avoid collisions
type contextKey string

const (
	ruleNameKey contextKey = "rule_name"
	ruleTypeKey contextKey = "rule_type"
)

// AddRuleDetailsToContext adds rule details to the context
func AddRuleDetailsToContext(ctx context.Context, ruleName string, ruleType string) context.Context {
	ctx = context.WithValue(ctx, ruleNameKey, ruleName)
	ctx = context.WithValue(ctx, ruleTypeKey, ruleType)
	return ctx
}

// GetRuleDetailsFromContext retrieves rule details from the context
func GetRuleDetailsFromContext(ctx context.Context) (string, string) {
	ruleName, _ := ctx.Value(ruleNameKey).(string)
	ruleType, _ := ctx.Value(ruleTypeKey).(string)
	return ruleName, ruleType
}
