package main

import (
	"encoding/json"
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
)

// copy and modification of github.com/prometheus/prometheus/model/relabel/relabel.go
// reason: the custom types in github.com/prometheus/prometheus/model/relabel/relabel.go are difficult to unmarshal
type RelabelConfig struct {
	// A list of labels from which values are taken and concatenated
	// with the configured separator in order.
	SourceLabels []string `json:"source_labels,omitempty"`
	// Separator is the string between concatenated values from the source labels.
	Separator string `json:"separator,omitempty"`
	// Regex against which the concatenation is matched.
	Regex string `json:"regex,omitempty"`
	// Modulus to take of the hash of concatenated values from the source labels.
	Modulus uint64 `json:"modulus,omitempty"`
	// TargetLabel is the label to which the resulting string is written in a replacement.
	// Regexp interpolation is allowed for the replace action.
	TargetLabel string `json:"target_label,omitempty"`
	// Replacement is the regex replacement pattern to be used.
	Replacement string `json:"replacement,omitempty"`
	// Action is the action to be performed for the relabeling.
	Action string `json:"action,omitempty"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (rc *RelabelConfig) UnmarshalJSON(data []byte) error {
	*rc = RelabelConfig{
		Action:      string(relabel.Replace),
		Separator:   ";",
		Regex:       "(.*)",
		Replacement: "$1",
	}
	type plain RelabelConfig
	if err := json.Unmarshal(data, (*plain)(rc)); err != nil {
		return err
	}
	return nil
}

// ToPrometheusConfig converts our JSON-friendly RelabelConfig to the Prometheus RelabelConfig
func (rc *RelabelConfig) ToPrometheusConfig() (*relabel.Config, error) {
	var regex relabel.Regexp
	if rc.Regex != "" {
		var err error
		regex, err = relabel.NewRegexp(rc.Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", rc.Regex, err)
		}
	} else {
		regex = relabel.DefaultRelabelConfig.Regex
	}

	action := relabel.Action(rc.Action)
	if rc.Action == "" {
		action = relabel.DefaultRelabelConfig.Action
	}

	separator := rc.Separator
	if separator == "" {
		separator = relabel.DefaultRelabelConfig.Separator
	}

	replacement := rc.Replacement
	if replacement == "" {
		replacement = relabel.DefaultRelabelConfig.Replacement
	}

	sourceLabels := make(model.LabelNames, 0, len(rc.SourceLabels))
	for _, l := range rc.SourceLabels {
		sourceLabels = append(sourceLabels, model.LabelName(l))
	}

	cfg := &relabel.Config{
		SourceLabels: sourceLabels,
		Separator:    separator,
		Regex:        regex,
		Modulus:      rc.Modulus,
		TargetLabel:  rc.TargetLabel,
		Replacement:  replacement,
		Action:       action,
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid relabel config: %w", err)
	}
	return cfg, nil
}

func ToPrometheusConfigs(cfgs []*RelabelConfig) ([]*relabel.Config, error) {
	promConfigs := make([]*relabel.Config, 0, len(cfgs))
	for _, cfg := range cfgs {
		promCfg, err := cfg.ToPrometheusConfig()
		if err != nil {
			return nil, fmt.Errorf("invalid relabel config: %w", err)
		}
		promConfigs = append(promConfigs, promCfg)
	}
	return promConfigs, nil
}

func parseRelabelConfigs(relabelConfigsRaw string) ([]*relabel.Config, error) {
	if relabelConfigsRaw == "" {
		return nil, nil
	}

	var relabelConfigs []*RelabelConfig

	if err := json.Unmarshal([]byte(relabelConfigsRaw), &relabelConfigs); err != nil {
		return nil, fmt.Errorf("failed to parse RELABEL_CONFIGS: %v", err)
	}
	promConfigs, err := ToPrometheusConfigs(relabelConfigs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse RELABEL_CONFIGS: %v", err)
	}
	return promConfigs, nil
}
