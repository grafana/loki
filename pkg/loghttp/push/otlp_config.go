package push

import (
	"fmt"

	"github.com/prometheus/prometheus/model/relabel"
)

var blessedAttributes = []string{
	"service.name",
	"service.namespace",
	"service.instance.id",
	"deployment.environment",
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

var DefaultOTLPConfig = OTLPConfig{
	ResourceAttributes: ResourceAttributesConfig{
		AttributesConfig: []AttributesConfig{
			{
				Action:     IndexLabel,
				Attributes: blessedAttributes,
			},
		},
	},
}

type OTLPConfig struct {
	ResourceAttributes ResourceAttributesConfig `yaml:"resource_attributes,omitempty"`
	ScopeAttributes    []AttributesConfig       `yaml:"scope_attributes,omitempty"`
	LogAttributes      []AttributesConfig       `yaml:"log_attributes,omitempty"`
}

func (c *OTLPConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultOTLPConfig
	type plain OTLPConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	return nil
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

	for _, ac := range c.LogAttributes {
		if ac.Action == IndexLabel {
			return fmt.Errorf("%s action is only supported for resource_attributes", IndexLabel)
		}
	}

	return nil
}

type AttributesConfig struct {
	Action     Action         `yaml:"action,omitempty"`
	Attributes []string       `yaml:"attributes,omitempty"`
	Regex      relabel.Regexp `yaml:"regex,omitempty"`
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

type ResourceAttributesConfig struct {
	IgnoreDefaults   bool               `yaml:"ignore_defaults,omitempty"`
	AttributesConfig []AttributesConfig `yaml:"attributes,omitempty"`
}

func (c *ResourceAttributesConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ResourceAttributesConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	if !c.IgnoreDefaults {
		c.AttributesConfig = append([]AttributesConfig{
			DefaultOTLPConfig.ResourceAttributes.AttributesConfig[0],
		}, c.AttributesConfig...)
	}

	return nil
}
