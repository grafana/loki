package push

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

var defaultGlobalOTLPConfig = GlobalOTLPConfig{}

func init() {
	flagext.DefaultValues(&defaultGlobalOTLPConfig)
}

func TestUnmarshalOTLPConfig(t *testing.T) {
	for _, tc := range []struct {
		name        string
		yamlConfig  []byte
		expectedCfg OTLPConfig
		expectedErr error
	}{
		{
			name: "only resource_attributes set",
			yamlConfig: []byte(`
resource_attributes:
  attributes_config:
    - action: index_label
      regex: foo`),
			expectedCfg: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						DefaultOTLPConfig(defaultGlobalOTLPConfig).ResourceAttributes.AttributesConfig[0],
						{
							Action: IndexLabel,
							Regex:  relabel.MustNewRegexp("foo"),
						},
					},
				},
			},
		},
		{
			name: "resource_attributes with defaults ignored",
			yamlConfig: []byte(`
resource_attributes:
  ignore_defaults: true
  attributes_config:
    - action: index_label
      regex: foo`),
			expectedCfg: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					IgnoreDefaults: true,
					AttributesConfig: []AttributesConfig{
						{
							Action: IndexLabel,
							Regex:  relabel.MustNewRegexp("foo"),
						},
					},
				},
			},
		},
		{
			name: "resource_attributes not set",
			yamlConfig: []byte(`
scope_attributes:
  - action: drop
    attributes:
      - fizz`),
			expectedCfg: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						{
							Action:     IndexLabel,
							Attributes: defaultGlobalOTLPConfig.DefaultOTLPResourceAttributesAsIndexLabels,
						},
					},
				},
				ScopeAttributes: []AttributesConfig{
					{
						Action:     Drop,
						Attributes: []string{"fizz"},
					},
				},
			},
		},
		{
			name: "all 3 set",
			yamlConfig: []byte(`
resource_attributes:
  attributes_config:
    - action: index_label
      regex: foo
scope_attributes:
  - action: drop
    attributes:
      - fizz
log_attributes:
  - action: structured_metadata
    attributes:
      - buzz`),
			expectedCfg: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						DefaultOTLPConfig(defaultGlobalOTLPConfig).ResourceAttributes.AttributesConfig[0],
						{
							Action: IndexLabel,
							Regex:  relabel.MustNewRegexp("foo"),
						},
					},
				},
				ScopeAttributes: []AttributesConfig{
					{
						Action:     Drop,
						Attributes: []string{"fizz"},
					},
				},
				LogAttributes: []AttributesConfig{
					{
						Action:     StructuredMetadata,
						Attributes: []string{"buzz"},
					},
				},
			},
		},
		{
			name: "unsupported action should error",
			yamlConfig: []byte(`
log_attributes:
  - action: keep
    attributes:
      - fizz`),
			expectedErr: errUnsupportedAction,
		},
		{
			name: "attributes and regex both not set should error",
			yamlConfig: []byte(`
log_attributes:
  - action: drop`),
			expectedErr: errAttributesAndRegexNotSet,
		},
		{
			name: "attributes and regex both being set should error",
			yamlConfig: []byte(`
log_attributes:
  - action: drop
    regex: foo
    attributes:
      - fizz`),
			expectedErr: errAttributesAndRegexBothSet,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := OTLPConfig{}
			err := yaml.UnmarshalStrict(tc.yamlConfig, &cfg)
			if tc.expectedErr != nil {
				require.ErrorIs(t, err, tc.expectedErr)
				return
			}
			cfg.ApplyGlobalOTLPConfig(defaultGlobalOTLPConfig)
			require.Equal(t, tc.expectedCfg, cfg)
		})
	}
}

func TestOTLPConfig(t *testing.T) {
	type attrAndExpAction struct {
		attr           string
		expectedAction Action
	}

	for _, tc := range []struct {
		name       string
		otlpConfig OTLPConfig
		resAttrs   []attrAndExpAction
		scopeAttrs []attrAndExpAction
		logAttrs   []attrAndExpAction
	}{
		{
			name:       "default OTLPConfig",
			otlpConfig: DefaultOTLPConfig(defaultGlobalOTLPConfig),
			resAttrs: []attrAndExpAction{
				{
					attr:           attrServiceName,
					expectedAction: IndexLabel,
				},
				{
					attr:           "not_blessed",
					expectedAction: StructuredMetadata,
				},
			},
			scopeAttrs: []attrAndExpAction{
				{
					attr:           "method",
					expectedAction: StructuredMetadata,
				},
			},
			logAttrs: []attrAndExpAction{
				{
					attr:           "user_id",
					expectedAction: StructuredMetadata,
				},
			},
		},
		{
			name: "drop everything except a few attrs",
			otlpConfig: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						{
							Action:     IndexLabel,
							Attributes: []string{attrServiceName},
						},
						{
							Action: StructuredMetadata,
							Regex:  relabel.MustNewRegexp("^foo.*"),
						},
						{
							Action: Drop,
							Regex:  relabel.MustNewRegexp(".*"),
						},
					},
				},
				ScopeAttributes: []AttributesConfig{
					{
						Action:     StructuredMetadata,
						Attributes: []string{"method"},
					},
					{
						Action: Drop,
						Regex:  relabel.MustNewRegexp(".*"),
					},
				},
				LogAttributes: []AttributesConfig{
					{
						Action:     StructuredMetadata,
						Attributes: []string{"user_id"},
					},
					{
						Action: Drop,
						Regex:  relabel.MustNewRegexp(".*"),
					},
				},
			},
			resAttrs: []attrAndExpAction{
				{
					attr:           attrServiceName,
					expectedAction: IndexLabel,
				},
				{
					attr:           "foo_bar",
					expectedAction: StructuredMetadata,
				},
				{
					attr:           "ping_foo",
					expectedAction: Drop,
				},
			},
			scopeAttrs: []attrAndExpAction{
				{
					attr:           "method",
					expectedAction: StructuredMetadata,
				},
				{
					attr:           "version",
					expectedAction: Drop,
				},
			},
			logAttrs: []attrAndExpAction{
				{
					attr:           "user_id",
					expectedAction: StructuredMetadata,
				},
				{
					attr:           "order_id",
					expectedAction: Drop,
				},
			},
		},
		{
			name: "keep everything except a few attrs",
			otlpConfig: OTLPConfig{
				ResourceAttributes: ResourceAttributesConfig{
					AttributesConfig: []AttributesConfig{
						{
							Action:     Drop,
							Attributes: []string{attrServiceName},
						},
						{
							Action: IndexLabel,
							Regex:  relabel.MustNewRegexp(".*"),
						},
					},
				},
				ScopeAttributes: []AttributesConfig{
					{
						Action:     Drop,
						Attributes: []string{"method"},
					},
					{
						Action: StructuredMetadata,
						Regex:  relabel.MustNewRegexp(".*"),
					},
				},
				LogAttributes: []AttributesConfig{
					{
						Action:     Drop,
						Attributes: []string{"user_id"},
					},
					{
						Action: StructuredMetadata,
						Regex:  relabel.MustNewRegexp(".*"),
					},
				},
			},
			resAttrs: []attrAndExpAction{
				{
					attr:           attrServiceName,
					expectedAction: Drop,
				},
				{
					attr:           "foo_bar",
					expectedAction: IndexLabel,
				},
			},
			scopeAttrs: []attrAndExpAction{
				{
					attr:           "method",
					expectedAction: Drop,
				},
				{
					attr:           "version",
					expectedAction: StructuredMetadata,
				},
			},
			logAttrs: []attrAndExpAction{
				{
					attr:           "user_id",
					expectedAction: Drop,
				},
				{
					attr:           "order_id",
					expectedAction: StructuredMetadata,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range tc.resAttrs {
				require.Equal(t, c.expectedAction, tc.otlpConfig.ActionForResourceAttribute(c.attr))
			}

			for _, c := range tc.scopeAttrs {
				require.Equal(t, c.expectedAction, tc.otlpConfig.ActionForScopeAttribute(c.attr))
			}

			for _, c := range tc.logAttrs {
				require.Equal(t, c.expectedAction, tc.otlpConfig.ActionForLogAttribute(c.attr))
			}
		})
	}
}
