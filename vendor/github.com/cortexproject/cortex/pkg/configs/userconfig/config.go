package userconfig

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"gopkg.in/yaml.v3"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"

	legacy_promql "github.com/cortexproject/cortex/pkg/configs/legacy_promql"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

// An ID is the ID of a single users's Cortex configuration. When a
// configuration changes, it gets a new ID.
type ID int

// RuleFormatVersion indicates which Prometheus rule format (v1 vs. v2) to use in parsing.
type RuleFormatVersion int

const (
	// RuleFormatV1 is the Prometheus 1.x rule format.
	RuleFormatV1 RuleFormatVersion = iota
	// RuleFormatV2 is the Prometheus 2.x rule format.
	RuleFormatV2 RuleFormatVersion = iota
)

// IsValid returns whether the rules format version is a valid (known) version.
func (v RuleFormatVersion) IsValid() bool {
	switch v {
	case RuleFormatV1, RuleFormatV2:
		return true
	default:
		return false
	}
}

// MarshalJSON implements json.Marshaler.
func (v RuleFormatVersion) MarshalJSON() ([]byte, error) {
	switch v {
	case RuleFormatV1:
		return json.Marshal("1")
	case RuleFormatV2:
		return json.Marshal("2")
	default:
		return nil, fmt.Errorf("unknown rule format version %d", v)
	}
}

// MarshalYAML implements yaml.Marshaler.
func (v RuleFormatVersion) MarshalYAML() (interface{}, error) {
	switch v {
	case RuleFormatV1:
		return yaml.Marshal("1")
	case RuleFormatV2:
		return yaml.Marshal("2")
	default:
		return nil, fmt.Errorf("unknown rule format version %d", v)
	}
}

// UnmarshalJSON implements json.Unmarshaler.
func (v *RuleFormatVersion) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "1":
		*v = RuleFormatV1
	case "2":
		*v = RuleFormatV2
	default:
		return fmt.Errorf("unknown rule format version %q", string(data))
	}
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (v *RuleFormatVersion) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	switch s {
	case "1":
		*v = RuleFormatV1
	case "2":
		*v = RuleFormatV2
	default:
		return fmt.Errorf("unknown rule format version %q", s)
	}
	return nil
}

// A Config is a Cortex configuration for a single user.
type Config struct {
	// RulesFiles maps from a rules filename to file contents.
	RulesConfig        RulesConfig
	TemplateFiles      map[string]string
	AlertmanagerConfig string
}

// configCompat is a compatibility struct to support old JSON config blobs
// saved in the config DB that didn't have a rule format version yet and
// just had a top-level field for the rule files.
type configCompat struct {
	RulesFiles         map[string]string `json:"rules_files" yaml:"rules_files"`
	RuleFormatVersion  RuleFormatVersion `json:"rule_format_version" yaml:"rule_format_version"`
	TemplateFiles      map[string]string `json:"template_files" yaml:"template_files"`
	AlertmanagerConfig string            `json:"alertmanager_config" yaml:"alertmanager_config"`
}

// MarshalJSON implements json.Marshaler.
func (c Config) MarshalJSON() ([]byte, error) {
	compat := &configCompat{
		RulesFiles:         c.RulesConfig.Files,
		RuleFormatVersion:  c.RulesConfig.FormatVersion,
		TemplateFiles:      c.TemplateFiles,
		AlertmanagerConfig: c.AlertmanagerConfig,
	}

	return json.Marshal(compat)
}

// MarshalYAML implements yaml.Marshaler.
func (c Config) MarshalYAML() (interface{}, error) {
	compat := &configCompat{
		RulesFiles:         c.RulesConfig.Files,
		RuleFormatVersion:  c.RulesConfig.FormatVersion,
		TemplateFiles:      c.TemplateFiles,
		AlertmanagerConfig: c.AlertmanagerConfig,
	}

	return yaml.Marshal(compat)
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *Config) UnmarshalJSON(data []byte) error {
	compat := configCompat{}
	if err := json.Unmarshal(data, &compat); err != nil {
		return err
	}
	*c = Config{
		RulesConfig: RulesConfig{
			Files:         compat.RulesFiles,
			FormatVersion: compat.RuleFormatVersion,
		},
		TemplateFiles:      compat.TemplateFiles,
		AlertmanagerConfig: compat.AlertmanagerConfig,
	}
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	compat := configCompat{}
	if err := unmarshal(&compat); err != nil {
		return errors.WithStack(err)
	}
	*c = Config{
		RulesConfig: RulesConfig{
			Files:         compat.RulesFiles,
			FormatVersion: compat.RuleFormatVersion,
		},
		TemplateFiles:      compat.TemplateFiles,
		AlertmanagerConfig: compat.AlertmanagerConfig,
	}
	return nil
}

// View is what's returned from the Weave Cloud configs service
// when we ask for all Cortex configurations.
//
// The configs service is essentially a JSON blob store that gives each
// _version_ of a configuration a unique ID and guarantees that later versions
// have greater IDs.
type View struct {
	ID        ID        `json:"id"`
	Config    Config    `json:"config"`
	DeletedAt time.Time `json:"deleted_at"`
}

// IsDeleted tells you if the config is deleted.
func (v View) IsDeleted() bool {
	return !v.DeletedAt.IsZero()
}

// GetVersionedRulesConfig specializes the view to just the rules config.
func (v View) GetVersionedRulesConfig() *VersionedRulesConfig {
	if v.Config.RulesConfig.Files == nil {
		return nil
	}
	return &VersionedRulesConfig{
		ID:        v.ID,
		Config:    v.Config.RulesConfig,
		DeletedAt: v.DeletedAt,
	}
}

// RulesConfig is the rules configuration for a particular organization.
type RulesConfig struct {
	FormatVersion RuleFormatVersion `json:"format_version"`
	Files         map[string]string `json:"files"`
}

// Equal compares two RulesConfigs for equality.
//
// instance Eq RulesConfig
func (c RulesConfig) Equal(o RulesConfig) bool {
	if c.FormatVersion != o.FormatVersion {
		return false
	}
	if len(o.Files) != len(c.Files) {
		return false
	}
	for k, v1 := range c.Files {
		v2, ok := o.Files[k]
		if !ok || v1 != v2 {
			return false
		}
	}
	return true
}

// Parse parses and validates the content of the rule files in a RulesConfig
// according to the passed rule format version.
func (c RulesConfig) Parse() (map[string][]rules.Rule, error) {
	switch c.FormatVersion {
	case RuleFormatV1:
		return c.parseV1()
	case RuleFormatV2:
		return c.parseV2()
	default:
		return nil, fmt.Errorf("unknown rule format version %v", c.FormatVersion)
	}
}

// ParseFormatted returns the rulefmt map of a users rules configs. It allows
// for rules to be mapped to disk and read by the prometheus rules manager.
func (c RulesConfig) ParseFormatted() (map[string]rulefmt.RuleGroups, error) {
	switch c.FormatVersion {
	case RuleFormatV1:
		return c.parseV1Formatted()
	case RuleFormatV2:
		return c.parseV2Formatted()
	default:
		return nil, fmt.Errorf("unknown rule format version %v", c.FormatVersion)
	}
}

// parseV2 parses and validates the content of the rule files in a RulesConfig
// according to the Prometheus 2.x rule format.
func (c RulesConfig) parseV2Formatted() (map[string]rulefmt.RuleGroups, error) {
	ruleMap := map[string]rulefmt.RuleGroups{}

	for fn, content := range c.Files {
		rgs, errs := rulefmt.Parse([]byte(content))
		for _, err := range errs { // return just the first error, if any
			return nil, err
		}
		ruleMap[fn] = *rgs

	}
	return ruleMap, nil
}

// parseV1 parses and validates the content of the rule files in a RulesConfig
// according to the Prometheus 1.x rule format.
func (c RulesConfig) parseV1Formatted() (map[string]rulefmt.RuleGroups, error) {
	result := map[string]rulefmt.RuleGroups{}
	for fn, content := range c.Files {
		stmts, err := legacy_promql.ParseStmts(content)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %s", fn, err)
		}

		ra := []rulefmt.RuleNode{}
		for _, stmt := range stmts {
			var rule rulefmt.RuleNode
			switch r := stmt.(type) {
			case *legacy_promql.AlertStmt:
				_, err := parser.ParseExpr(r.Expr.String())
				if err != nil {
					return nil, err
				}

				rule = rulefmt.RuleNode{
					Alert:       yaml.Node{Value: r.Name},
					Expr:        yaml.Node{Value: r.Expr.String()},
					For:         model.Duration(r.Duration),
					Labels:      r.Labels.Map(),
					Annotations: r.Annotations.Map(),
				}

			case *legacy_promql.RecordStmt:
				_, err := parser.ParseExpr(r.Expr.String())
				if err != nil {
					return nil, err
				}

				rule = rulefmt.RuleNode{
					Record: yaml.Node{Value: r.Name},
					Expr:   yaml.Node{Value: r.Expr.String()},
					Labels: r.Labels.Map(),
				}

			default:
				return nil, fmt.Errorf("ruler.GetRules: unknown statement type")
			}
			ra = append(ra, rule)
		}
		result[fn] = rulefmt.RuleGroups{
			Groups: []rulefmt.RuleGroup{
				{
					Name:  "rg:" + fn,
					Rules: ra,
				},
			},
		}
	}
	return result, nil
}

// parseV2 parses and validates the content of the rule files in a RulesConfig
// according to the Prometheus 2.x rule format.
//
// NOTE: On one hand, we cannot return fully-fledged lists of rules.Group
// here yet, as creating a rules.Group requires already
// passing in rules.ManagerOptions options (which in turn require a
// notifier, appender, etc.), which we do not want to create simply
// for parsing. On the other hand, we should not return barebones
// rulefmt.RuleGroup sets here either, as only a fully-converted rules.Rule
// is able to track alert states over multiple rule evaluations. The caller
// would otherwise have to ensure to convert the rulefmt.RuleGroup only exactly
// once, not for every evaluation (or risk losing alert pending states). So
// it's probably better to just return a set of rules.Rule here.
func (c RulesConfig) parseV2() (map[string][]rules.Rule, error) {
	groups := map[string][]rules.Rule{}

	for fn, content := range c.Files {
		rgs, errs := rulefmt.Parse([]byte(content))
		if len(errs) > 0 {
			return nil, fmt.Errorf("error parsing %s: %v", fn, errs[0])
		}

		for _, rg := range rgs.Groups {
			rls := make([]rules.Rule, 0, len(rg.Rules))
			for _, rl := range rg.Rules {
				expr, err := parser.ParseExpr(rl.Expr.Value)
				if err != nil {
					return nil, err
				}

				if rl.Alert.Value != "" {
					rls = append(rls, rules.NewAlertingRule(
						rl.Alert.Value,
						expr,
						time.Duration(rl.For),
						labels.FromMap(rl.Labels),
						labels.FromMap(rl.Annotations),
						nil,
						true,
						log.With(util_log.Logger, "alert", rl.Alert.Value),
					))
					continue
				}
				rls = append(rls, rules.NewRecordingRule(
					rl.Record.Value,
					expr,
					labels.FromMap(rl.Labels),
				))
			}

			// Group names have to be unique in Prometheus, but only within one rules file.
			groups[rg.Name+";"+fn] = rls
		}
	}

	return groups, nil
}

// parseV1 parses and validates the content of the rule files in a RulesConfig
// according to the Prometheus 1.x rule format.
//
// The same comment about rule groups as on ParseV2() applies here.
func (c RulesConfig) parseV1() (map[string][]rules.Rule, error) {
	result := map[string][]rules.Rule{}
	for fn, content := range c.Files {
		stmts, err := legacy_promql.ParseStmts(content)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s: %s", fn, err)
		}
		ra := []rules.Rule{}
		for _, stmt := range stmts {
			var rule rules.Rule

			switch r := stmt.(type) {
			case *legacy_promql.AlertStmt:
				// legacy_promql.ParseStmts has parsed the whole rule for us.
				// Ideally we'd just use r.Expr and pass that to rules.NewAlertingRule,
				// but it is of the type legacy_proql.Expr and not promql.Expr.
				// So we convert it back to a string, and then parse it again with the
				// upstream parser to get it into the right type.
				expr, err := parser.ParseExpr(r.Expr.String())
				if err != nil {
					return nil, err
				}

				rule = rules.NewAlertingRule(
					r.Name, expr, r.Duration, r.Labels, r.Annotations, nil, true,
					log.With(util_log.Logger, "alert", r.Name),
				)

			case *legacy_promql.RecordStmt:
				expr, err := parser.ParseExpr(r.Expr.String())
				if err != nil {
					return nil, err
				}

				rule = rules.NewRecordingRule(r.Name, expr, r.Labels)

			default:
				return nil, fmt.Errorf("ruler.GetRules: unknown statement type")
			}
			ra = append(ra, rule)
		}
		result[fn] = ra
	}
	return result, nil
}

// VersionedRulesConfig is a RulesConfig together with a version.
// `data Versioned a = Versioned { id :: ID , config :: a }`
type VersionedRulesConfig struct {
	ID        ID          `json:"id"`
	Config    RulesConfig `json:"config"`
	DeletedAt time.Time   `json:"deleted_at"`
}

// IsDeleted tells you if the config is deleted.
func (vr VersionedRulesConfig) IsDeleted() bool {
	return !vr.DeletedAt.IsZero()
}
