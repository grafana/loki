package rules

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

// testEvaluator evaluates rules for unit testing.
// It manages rule loading, evaluation at specific time intervals, and alert tracking.
type testEvaluator struct {
	// storage is the test storage backend
	storage *testStorage

	// engine is the LogQL query engine
	engine *logql.QueryEngine

	// ruleGroups contains the loaded rule groups
	ruleGroups []*ruleGroup

	// logger for evaluation messages
	logger log.Logger

	// currentTime tracks the current evaluation time
	currentTime time.Time
}

// ruleGroup represents a group of rules with evaluation state.
type ruleGroup struct {
	Name     string
	Interval time.Duration
	Rules    []evaluableRule

	// External labels and URL for alert generation
	ExternalLabels labels.Labels
	ExternalURL    string
}

// evaluableRule wraps a rule with evaluation state.
type evaluableRule struct {
	// Rule definition
	Record string // For recording rules
	Alert  string // For alerting rules
	Expr   string // LogQL expression
	Labels labels.Labels
	Annotations labels.Labels
	For    model.Duration

	// Evaluation state
	activeAlerts []*activeAlert
	lastEvalTime time.Time
}

// activeAlert tracks an active alert instance.
type activeAlert struct {
	Labels      labels.Labels
	Annotations labels.Labels
	ActiveAt    time.Time
	FiredAt     time.Time // Zero if not yet fired (still pending)
	Value       float64
}

// newTestEvaluator creates a new test evaluator.
func newTestEvaluator(storage *testStorage, logger log.Logger) *testEvaluator {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	// Create LogQL engine with test querier
	engine := logql.NewEngine(logql.EngineOpts{
		MaxLookBackPeriod: 5 * time.Minute,
	}, storage.Querier(), logql.NoLimits, logger)

	return &testEvaluator{
		storage:     storage,
		engine:      engine,
		ruleGroups:  []*ruleGroup{},
		logger:      logger,
		currentTime: time.Unix(0, 0).UTC(),
	}
}

// loadRules loads rule files and prepares them for evaluation.
func (te *testEvaluator) loadRules(ruleFiles []string, evalInterval model.Duration, externalLabels labels.Labels, externalURL string) error {
	// Parse rule files using Loki's parser
	namespaces, err := ParseFiles(ruleFiles)
	if err != nil {
		return fmt.Errorf("failed to parse rule files: %w", err)
	}

	// Convert to evaluable rule groups
	for nsName, ns := range namespaces {
		for _, group := range ns.Groups {
			rg := &ruleGroup{
				Name:           group.Name,
				Interval:       time.Duration(group.Interval),
				Rules:          make([]evaluableRule, 0, len(group.Rules)),
				ExternalLabels: externalLabels,
				ExternalURL:    externalURL,
			}

			// Use default eval interval if not set
			if rg.Interval == 0 {
				rg.Interval = time.Duration(evalInterval)
			}

			// Convert each rule
			for _, rule := range group.Rules {
				er := evaluableRule{
					Record:       rule.Record,
					Alert:        rule.Alert,
					Expr:         rule.Expr,
					Labels:       convertMapToLabels(rule.Labels),
					Annotations:  convertMapToLabels(rule.Annotations),
					For:          rule.For,
					activeAlerts: []*activeAlert{},
				}
				rg.Rules = append(rg.Rules, er)
			}

			te.ruleGroups = append(te.ruleGroups, rg)
		}

		_ = nsName // namespace is stored in the group for reference
	}

	return nil
}

// evaluateAtTime evaluates all rules at the specified time.
func (te *testEvaluator) evaluateAtTime(ctx context.Context, evalTime time.Time) error {
	te.currentTime = evalTime
	te.storage.SetCurrentTime(evalTime)

	for _, group := range te.ruleGroups {
		if err := te.evaluateGroup(ctx, group, evalTime); err != nil {
			return fmt.Errorf("failed to evaluate group %s: %w", group.Name, err)
		}
	}

	return nil
}

// evaluateGroup evaluates all rules in a group.
func (te *testEvaluator) evaluateGroup(ctx context.Context, group *ruleGroup, evalTime time.Time) error {
	for i := range group.Rules {
		rule := &group.Rules[i]
		if err := te.evaluateRule(ctx, rule, group, evalTime); err != nil {
			return fmt.Errorf("failed to evaluate rule %s: %w", te.getRuleName(rule), err)
		}
	}
	return nil
}

// evaluateRule evaluates a single rule.
func (te *testEvaluator) evaluateRule(ctx context.Context, rule *evaluableRule, group *ruleGroup, evalTime time.Time) error {
	rule.lastEvalTime = evalTime

	// Parse the LogQL expression
	expr, err := syntax.ParseExpr(rule.Expr)
	if err != nil {
		return fmt.Errorf("failed to parse expression: %w", err)
	}

	// For alerting rules, evaluate and track alert state
	if rule.Alert != "" {
		return te.evaluateAlertRule(ctx, rule, group, expr, evalTime)
	}

	// For recording rules, just evaluate the expression
	return te.evaluateRecordingRule(ctx, rule, expr, evalTime)
}

// evaluateAlertRule evaluates an alerting rule and updates alert state.
func (te *testEvaluator) evaluateAlertRule(ctx context.Context, rule *evaluableRule, group *ruleGroup, expr syntax.Expr, evalTime time.Time) error {
	// Create query parameters
	params, err := logql.NewLiteralParams(
		rule.Expr,
		evalTime,
		evalTime,
		0, // step (instant query)
		0, // interval
		logproto.BACKWARD,
		0,   // limit
		nil, // shards
		nil, // storeChunks
	)
	if err != nil {
		return fmt.Errorf("failed to create params: %w", err)
	}

	// Execute query
	query := te.engine.Query(params)
	result, err := query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	// Process results and update alert state
	newAlerts := make([]*activeAlert, 0)

	// Handle different result types
	switch v := result.Data.(type) {
	case promql.Vector:
		for _, sample := range v {
			// Check if alert should fire (value > 0 for most alert conditions)
			if sample.F > 0 || sample.H != nil {
				alert := &activeAlert{
					Labels:      appendLabels(sample.Metric, rule.Labels, group.ExternalLabels),
					Annotations: rule.Annotations,
					ActiveAt:    evalTime,
					Value:       sample.F,
				}

				// Check if this alert was already active
				existing := te.findActiveAlert(rule.activeAlerts, alert.Labels)
				if existing != nil {
					// Alert was already active, preserve ActiveAt
					alert.ActiveAt = existing.ActiveAt
					alert.FiredAt = existing.FiredAt

					// Check if alert should transition to firing
					if alert.FiredAt.IsZero() && time.Duration(rule.For) > 0 {
						if evalTime.Sub(alert.ActiveAt) >= time.Duration(rule.For) {
							alert.FiredAt = evalTime
						}
					}
				} else {
					// New alert, fire immediately if no "for" duration
					if rule.For == 0 {
						alert.FiredAt = evalTime
					}
				}

				newAlerts = append(newAlerts, alert)
			}
		}

	case promql.Scalar:
		// Scalar result - single alert if value > 0
		if v.V > 0 {
			alert := &activeAlert{
				Labels:      appendLabels(labels.EmptyLabels(), rule.Labels, group.ExternalLabels),
				Annotations: rule.Annotations,
				ActiveAt:    evalTime,
				FiredAt:     evalTime, // Scalars fire immediately
				Value:       v.V,
			}
			newAlerts = append(newAlerts, alert)
		}
	case parser.Value:
		// Generic parser.Value - try to handle it
		// For now, just log that we received a result
		_ = v
	}

	// Update active alerts
	rule.activeAlerts = newAlerts

	return nil
}

// evaluateRecordingRule evaluates a recording rule.
func (te *testEvaluator) evaluateRecordingRule(ctx context.Context, rule *evaluableRule, expr syntax.Expr, evalTime time.Time) error {
	// Create query parameters
	params, err := logql.NewLiteralParams(
		rule.Expr,
		evalTime,
		evalTime,
		0, // step (instant query)
		0, // interval
		logproto.BACKWARD,
		0,   // limit
		nil, // shards
		nil, // storeChunks
	)
	if err != nil {
		return fmt.Errorf("failed to create params: %w", err)
	}

	// Execute query
	query := te.engine.Query(params)
	_, err = query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	// For recording rules, we don't need to do anything with the result
	// In a real ruler, this would be written back to storage
	return nil
}

// findActiveAlert finds an existing alert by labels.
func (te *testEvaluator) findActiveAlert(alerts []*activeAlert, lbls labels.Labels) *activeAlert {
	for _, alert := range alerts {
		if labels.Equal(alert.Labels, lbls) {
			return alert
		}
	}
	return nil
}

// getActiveAlertsForRule returns all currently firing alerts for a rule.
func (te *testEvaluator) getActiveAlertsForRule(ruleName string) []*activeAlert {
	for _, group := range te.ruleGroups {
		for _, rule := range group.Rules {
			if rule.Alert == ruleName {
				// Return only firing alerts (not pending)
				firing := make([]*activeAlert, 0)
				for _, alert := range rule.activeAlerts {
					if !alert.FiredAt.IsZero() {
						firing = append(firing, alert)
					}
				}
				return firing
			}
		}
	}
	return nil
}

// getRuleName returns the name of a rule (alert or record name).
func (te *testEvaluator) getRuleName(rule *evaluableRule) string {
	if rule.Alert != "" {
		return rule.Alert
	}
	return rule.Record
}

// getRuleGroups returns all loaded rule groups (for testing).
func (te *testEvaluator) getRuleGroups() []*ruleGroup {
	return te.ruleGroups
}

// Helper functions

// convertMapToLabels converts a string map to labels.Labels.
func convertMapToLabels(m map[string]string) labels.Labels {
	if len(m) == 0 {
		return labels.EmptyLabels()
	}

	builder := labels.NewBuilder(labels.EmptyLabels())
	for k, v := range m {
		builder.Set(k, v)
	}
	return builder.Labels()
}

// appendLabels combines multiple label sets, with later sets taking precedence.
func appendLabels(base labels.Labels, additional ...labels.Labels) labels.Labels {
	builder := labels.NewBuilder(base)

	for _, lbls := range additional {
		lbls.Range(func(lbl labels.Label) {
			builder.Set(lbl.Name, lbl.Value)
		})
	}

	return builder.Labels()
}

// sortRuleGroups sorts rule groups by the provided order map.
func (te *testEvaluator) sortRuleGroups(groupOrderMap map[string]int) {
	sort.Slice(te.ruleGroups, func(i, j int) bool {
		orderI, okI := groupOrderMap[te.ruleGroups[i].Name]
		orderJ, okJ := groupOrderMap[te.ruleGroups[j].Name]

		// If both have explicit order, sort by order
		if okI && okJ {
			return orderI < orderJ
		}

		// If only one has order, it comes first
		if okI {
			return true
		}
		if okJ {
			return false
		}

		// Otherwise, maintain original order
		return i < j
	})
}

// LoadRuleFiles loads rule files from the given list of rule group definitions.
func LoadRuleFiles(ruleFiles []string) (map[string]*rwrulefmt.RuleGroup, error) {
	namespaces, err := ParseFiles(ruleFiles)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rule files: %w", err)
	}

	groups := make(map[string]*rwrulefmt.RuleGroup)
	for _, ns := range namespaces {
		for i := range ns.Groups {
			group := &ns.Groups[i]
			groups[group.Name] = group
		}
	}

	return groups, nil
}
