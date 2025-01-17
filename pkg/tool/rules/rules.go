package rules

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	log "github.com/sirupsen/logrus"

	logql "github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/tool/rules/rwrulefmt"
)

// RuleNamespace is used to parse a slightly modified prometheus
// rule file format, if no namespace is set, the default namespace
// is used. Namespace is functionally the same as a file name.
type RuleNamespace struct {
	// Namespace field only exists for setting namespace in namespace body instead of file name
	Namespace string `yaml:"namespace,omitempty"`
	Filepath  string `yaml:"-"`

	Groups []rwrulefmt.RuleGroup `yaml:"groups"`
}

// LintExpressions runs the `expr` from a rule through the PromQL or LogQL parser and
// compares its output. If it differs from the parser, it uses the parser's instead.
func (r RuleNamespace) LintExpressions() (int, int, error) {
	var parseFn func(string) (fmt.Stringer, error)
	queryLanguage := "LogQL"

	parseFn = func(s string) (fmt.Stringer, error) {
		return logql.ParseExpr(s)
	}

	// `count` represents the number of rules we evalated.
	// `mod` represents the number of rules linted.
	var count, mod int
	for i, group := range r.Groups {
		for j, rule := range group.Rules {
			log.WithFields(log.Fields{"rule": getRuleName(rule)}).Debugf("linting %s", queryLanguage)
			exp, err := parseFn(rule.Expr.Value)
			if err != nil {
				return count, mod, err
			}

			count++
			if rule.Expr.Value != exp.String() {
				log.WithFields(log.Fields{
					"rule":        getRuleName(rule),
					"currentExpr": rule.Expr,
					"afterExpr":   exp.String(),
				}).Debugf("expression differs")

				mod++
				r.Groups[i].Rules[j].Expr.Value = exp.String()
			}
		}
	}

	return count, mod, nil
}

// CheckRecordingRules checks that recording rules have at least one colon in their name, this is based
// on the recording rules best practices here: https://prometheus.io/docs/practices/rules/
// Returns the number of rules that don't match the requirements.
func (r RuleNamespace) CheckRecordingRules(strict bool) int {
	var name string
	var count int
	reqChunks := 2
	if strict {
		reqChunks = 3
	}
	for _, group := range r.Groups {
		for _, rule := range group.Rules {
			// Assume if there is a rule.Record that this is a recording rule.
			if rule.Record.Value == "" {
				continue
			}
			name = rule.Record.Value
			log.WithFields(log.Fields{"rule": name}).Debugf("linting recording rule name")
			chunks := strings.Split(name, ":")
			if len(chunks) < reqChunks {
				count++
				log.WithFields(log.Fields{
					"rule":      getRuleName(rule),
					"ruleGroup": group.Name,
					"file":      r.Filepath,
					"error":     "recording rule name does not match level:metric:operation format, must contain at least one colon",
				}).Errorf("bad recording rule name")
			}
		}
	}
	return count
}

// AggregateBy modifies the aggregation rules in groups to include a given Label.
// If the applyTo function is provided, the aggregation is applied only to rules
// for which the applyTo function returns true.
func (r RuleNamespace) AggregateBy(label string, applyTo func(group rwrulefmt.RuleGroup, rule rulefmt.RuleNode) bool) (int, int, error) {
	// `count` represents the number of rules we evaluated.
	// `mod` represents the number of rules we modified - a modification can either be a lint or adding the
	// label in the aggregation.
	var count, mod int

	for i, group := range r.Groups {
		for j, rule := range group.Rules {
			// Skip it if the applyTo function returns false.
			if applyTo != nil && !applyTo(group, rule) {
				log.WithFields(log.Fields{
					"group": group.Name,
					"rule":  getRuleName(rule),
				}).Debugf("skipped")

				count++
				continue
			}

			log.WithFields(log.Fields{"rule": getRuleName(rule)}).Debugf("evaluating...")
			exp, err := parser.ParseExpr(rule.Expr.Value)
			if err != nil {
				return count, mod, err
			}

			count++
			// Given inspect will help us traverse every node in the AST, Let's create the
			// function that will modify the labels.
			f := exprNodeInspectorFunc(rule, label)
			parser.Inspect(exp, f)

			// Only modify the ones that actually changed.
			if rule.Expr.Value != exp.String() {
				log.WithFields(log.Fields{
					"rule":        getRuleName(rule),
					"currentExpr": rule.Expr,
					"afterExpr":   exp.String(),
				}).Debugf("expression differs")
				mod++
				r.Groups[i].Rules[j].Expr.Value = exp.String()
			}
		}
	}

	return count, mod, nil
}

// exprNodeInspectorFunc returns a PromQL inspector.
// It modifies most PromQL expressions to include a given label.
func exprNodeInspectorFunc(rule rulefmt.RuleNode, label string) func(node parser.Node, path []parser.Node) error {
	return func(node parser.Node, _ []parser.Node) error {
		var err error
		switch n := node.(type) {
		case *parser.AggregateExpr:
			err = prepareAggregationExpr(n, label, getRuleName(rule))
		case *parser.BinaryExpr:
			err = prepareBinaryExpr(n, label, getRuleName(rule))
		default:
			return err
		}

		return err
	}
}

func prepareAggregationExpr(e *parser.AggregateExpr, label string, ruleName string) error {
	// If the aggregation is about dropping labels (e.g. without), we don't want to modify
	// this expression. Omission as long as it is not the cluster label will include it.
	// TODO: We probably want to check whenever the label we're trying to include is included in the omission.
	if e.Without {
		return nil
	}

	for _, lbl := range e.Grouping {
		// It already has the label we want to aggregate by.
		if lbl == label {
			return nil
		}
	}

	log.WithFields(
		log.Fields{"rule": ruleName, "lbls": strings.Join(e.Grouping, ", ")},
	).Debugf("aggregation without '%s' label, adding.", label)

	e.Grouping = append(e.Grouping, label)
	return nil
}

func prepareBinaryExpr(e *parser.BinaryExpr, label string, rule string) error {
	if e.VectorMatching == nil {
		return nil
	}

	if !e.VectorMatching.On {
		return nil
	}

	for _, lbl := range e.VectorMatching.MatchingLabels {
		// It already has the label we want to add in the expression.
		if lbl == label {
			return nil
		}
	}

	log.WithFields(
		log.Fields{"rule": rule, "lbls": strings.Join(e.VectorMatching.MatchingLabels, ", ")},
	).Debugf("binary expression without '%s' label, adding.", label)

	e.VectorMatching.MatchingLabels = append(e.VectorMatching.MatchingLabels, label)
	return nil
}

// Validate each rule in the rule namespace is valid
func (r RuleNamespace) Validate() []error {
	set := map[string]struct{}{}
	var errs []error

	for _, g := range r.Groups {
		if g.Name == "" {
			errs = append(errs, fmt.Errorf("groupname should not be empty"))
		}

		if _, ok := set[g.Name]; ok {
			errs = append(
				errs,
				fmt.Errorf("groupname: \"%s\" is repeated in the same namespace", g.Name),
			)
		}

		set[g.Name] = struct{}{}

		errs = append(errs, ValidateRuleGroup(g)...)
	}

	return errs
}

// ValidateRuleGroup validates a rulegroup
func ValidateRuleGroup(g rwrulefmt.RuleGroup) []error {
	var errs []error
	for i, r := range g.Rules {
		for _, err := range r.Validate() {
			var ruleName string
			if r.Alert.Value != "" {
				ruleName = r.Alert.Value
			} else {
				ruleName = r.Record.Value
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

func getRuleName(r rulefmt.RuleNode) string {
	if r.Record.Value != "" {
		return r.Record.Value
	}

	return r.Alert.Value
}
