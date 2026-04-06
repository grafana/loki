package push

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/relabel"
)

// Action is the action to be performed on OTLP Resource Attribute.
type Action string

const (
	// IndexLabel stores a Resource Attribute as a label in index to identify streams.
	IndexLabel Action = "index_label"
	// StructuredMetadata stores an Attribute as Structured Metadata with each log entry.
	StructuredMetadata Action = "structured_metadata"
	// Drop drops Attributes for which the Attribute name does match the regex.
	Drop Action = "drop"
)

var (
	errUnsupportedAction         = fmt.Errorf("unsupported action, it must be one of: %s, %s, %s", Drop, IndexLabel, StructuredMetadata)
	errAttributesAndRegexNotSet  = fmt.Errorf("attributes or regex must be set")
	errAttributesAndRegexBothSet = fmt.Errorf("only one of attributes or regex must be set")
)

func DefaultOTLPConfig(cfg GlobalOTLPConfig) OTLPConfig {
	return OTLPConfig{
		ResourceAttributes: ResourceAttributesConfig{
			AttributesConfig: []AttributesConfig{
				{
					Action:     IndexLabel,
					Attributes: cfg.DefaultOTLPResourceAttributesAsIndexLabels,
				},
			},
		},
	}
}

type OTLPConfig struct {
	ResourceAttributes  ResourceAttributesConfig `yaml:"resource_attributes,omitempty" json:"resource_attributes,omitempty" doc:"description=Configuration for resource attributes to store them as index labels or Structured Metadata or drop them altogether"`
	ScopeAttributes     []AttributesConfig       `yaml:"scope_attributes,omitempty" json:"scope_attributes,omitempty" doc:"description=Configuration for scope attributes to store them as Structured Metadata or drop them altogether"`
	LogAttributes       []AttributesConfig       `yaml:"log_attributes,omitempty" json:"log_attributes,omitempty" doc:"description=Configuration for log attributes to store them as index labels or Structured Metadata or drop them altogether"`
	SeverityTextAsLabel bool                     `yaml:"severity_text_as_label,omitempty" json:"severity_text_as_label,omitempty" doc:"default=false|description=When true, the severity_text field from log records will be stored as an index label. It is recommended not to use this option unless absolutely necessary"`
}

type GlobalOTLPConfig struct {
	DefaultOTLPResourceAttributesAsIndexLabels []string `yaml:"default_resource_attributes_as_index_labels"`
}

// RegisterFlags registers distributor-related flags.
func (cfg *GlobalOTLPConfig) RegisterFlags(fs *flag.FlagSet) {
	cfg.DefaultOTLPResourceAttributesAsIndexLabels = []string{
		"service.name",
		"service.namespace",
		"service.instance.id",
		"deployment.environment",
		"deployment.environment.name",
		"cloud.region",
		"cloud.availability_zone",
		"k8s.cluster.name",
		"k8s.namespace.name",
		"k8s.pod.name",
		"k8s.container.name",
		"container.name",
		"k8s.replicaset.name",
		"k8s.deployment.name",
		"k8s.statefulset.name",
		"k8s.daemonset.name",
		"k8s.cronjob.name",
		"k8s.job.name",
	}
	fs.Var((*flagext.StringSlice)(&cfg.DefaultOTLPResourceAttributesAsIndexLabels), "distributor.otlp.default_resource_attributes_as_index_labels", "List of default otlp resource attributes to be picked as index labels")
}

// ApplyGlobalOTLPConfig applies global otlp config, specifically DefaultOTLPResourceAttributesAsIndexLabels for the start.
func (c *OTLPConfig) ApplyGlobalOTLPConfig(config GlobalOTLPConfig) {
	if !c.ResourceAttributes.IgnoreDefaults && len(config.DefaultOTLPResourceAttributesAsIndexLabels) != 0 {
		c.ResourceAttributes.AttributesConfig = append([]AttributesConfig{
			{
				Action:     IndexLabel,
				Attributes: config.DefaultOTLPResourceAttributesAsIndexLabels,
			},
		}, c.ResourceAttributes.AttributesConfig...)
	}
}

func (c *OTLPConfig) actionForAttribute(attribute string, cfgs []AttributesConfig) Action {
	for i := 0; i < len(cfgs); i++ {
		if cfgs[i].Regex.Regexp != nil && cfgs[i].Regex.MatchString(attribute) {
			return cfgs[i].Action
		}
		for _, cfgAttr := range cfgs[i].Attributes {
			if cfgAttr == attribute {
				return cfgs[i].Action
			}
		}
	}

	return StructuredMetadata
}

func (c *OTLPConfig) ActionForResourceAttribute(attribute string) Action {
	return c.actionForAttribute(attribute, c.ResourceAttributes.AttributesConfig)
}

func (c *OTLPConfig) ActionForScopeAttribute(attribute string) Action {
	return c.actionForAttribute(attribute, c.ScopeAttributes)
}

func (c *OTLPConfig) ActionForLogAttribute(attribute string) Action {
	return c.actionForAttribute(attribute, c.LogAttributes)
}

func (c *OTLPConfig) Validate() error {
	for _, ac := range c.ScopeAttributes {
		if ac.Action == IndexLabel {
			return fmt.Errorf("%s action is only supported for resource_attributes", IndexLabel)
		}
	}

	return nil
}

type AttributesConfig struct {
	Action     Action         `yaml:"action,omitempty" json:"action,omitempty" doc:"description=Configures action to take on matching attributes. It allows one of [structured_metadata, drop] for all attribute types. It additionally allows index_label action for resource attributes"`
	Attributes []string       `yaml:"attributes,omitempty" json:"attributes,omitempty" doc:"description=List of attributes to configure how to store them or drop them altogether"`
	Regex      relabel.Regexp `yaml:"regex" json:"regex" doc:"description=Regex to choose attributes to configure how to store them or drop them altogether"`
}

func (c *AttributesConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain AttributesConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if c.Action == "" {
		c.Action = StructuredMetadata
	}

	if c.Action != IndexLabel && c.Action != StructuredMetadata && c.Action != Drop {
		return errUnsupportedAction
	}

	if len(c.Attributes) == 0 && c.Regex.Regexp == nil {
		return errAttributesAndRegexNotSet
	}

	if len(c.Attributes) != 0 && c.Regex.Regexp != nil {
		return errAttributesAndRegexBothSet
	}

	return nil
}

func (c AttributesConfig) MarshalJSON() ([]byte, error) {
	aux := getMarshableAttributesConfig(c)
	return json.Marshal(aux)
}

func (c AttributesConfig) MarshalYAML() (any, error) {
	return getMarshableAttributesConfig(c), nil
}

// getMarshableAttributesConfig overrides AttributesConfig to avoid
// marshaling the Regex as nil or empty string when it is not set.
// Note: we need this since relabel.Regexp is a struct and `omitempty`
// does not have effect on nested structs.
func getMarshableAttributesConfig(c AttributesConfig) any {
	aux := struct {
		Action     Action   `yaml:"action,omitempty" json:"action,omitempty"`
		Attributes []string `yaml:"attributes,omitempty" json:"attributes,omitempty"`
		Regex      any      `yaml:"regex,omitempty" json:"regex,omitempty"`
	}{
		Action:     c.Action,
		Attributes: c.Attributes,
	}

	if c.Regex.Regexp != nil {
		aux.Regex = c.Regex.String()
	} else {
		aux.Regex = nil
	}

	return aux
}

type ResourceAttributesConfig struct {
	IgnoreDefaults   bool               `yaml:"ignore_defaults,omitempty" json:"ignore_defaults,omitempty" doc:"default=false|description=Configure whether to ignore the default list of resource attributes set in 'distributor.otlp.default_resource_attributes_as_index_labels' to be stored as index labels and only use the given resource attributes config"`
	AttributesConfig []AttributesConfig `yaml:"attributes_config,omitempty" json:"attributes_config,omitempty"`
}
