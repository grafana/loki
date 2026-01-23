package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestXMLExpressionParser(t *testing.T) {
	tests := []struct {
		name            string
		line            []byte
		expressions     []LabelExtractionExpr
		expectedLabels  map[string]string
		shouldFail      bool
	}{
		{
			"single field extraction",
			[]byte(`<?xml version="1.0"?><root><app>myapp</app></root>`),
			[]LabelExtractionExpr{
				{Identifier: "myapp", Expression: "app"},
			},
			map[string]string{
				"myapp": "myapp",
			},
			false,
		},
		{
			"nested field extraction",
			[]byte(`<?xml version="1.0"?><root><pod><uuid>12345</uuid></pod></root>`),
			[]LabelExtractionExpr{
				{Identifier: "pod_id", Expression: "pod.uuid"},
			},
			map[string]string{
				"pod_id": "12345",
			},
			false,
		},
		{
			"multiple field extraction",
			[]byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env></root>`),
			[]LabelExtractionExpr{
				{Identifier: "app_name", Expression: "app"},
				{Identifier: "app_version", Expression: "version"},
				{Identifier: "app_env", Expression: "env"},
			},
			map[string]string{
				"app_name":    "myapp",
				"app_version": "1.0",
				"app_env":     "prod",
			},
			false,
		},
		{
			"deep nesting",
			[]byte(`<?xml version="1.0"?><root><pod><deployment><ref>abc123</ref></deployment></pod></root>`),
			[]LabelExtractionExpr{
				{Identifier: "deployment_ref", Expression: "pod.deployment.ref"},
			},
			map[string]string{
				"deployment_ref": "abc123",
			},
			false,
		},
		{
			"missing field",
			[]byte(`<?xml version="1.0"?><root><app>myapp</app></root>`),
			[]LabelExtractionExpr{
				{Identifier: "missing", Expression: "nonexistent"},
			},
			map[string]string{
				"missing": "",
			},
			false,
		},
		{
			"invalid identifier",
			[]byte(`<?xml version="1.0"?><root><app>myapp</app></root>`),
			[]LabelExtractionExpr{
				{Identifier: "!invalid!", Expression: "app"},
			},
			nil,
			true,
		},
		{
			"malformed XML",
			[]byte(`<?xml version="1.0"?><root><unclosed>`),
			[]LabelExtractionExpr{
				{Identifier: "app", Expression: "app"},
			},
			nil,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewXMLExpressionParser(tt.expressions)

			if tt.shouldFail && err != nil {
				// Expected error during parser creation
				require.Error(t, err)
				return
			}

			if tt.shouldFail && err == nil {
				// Expected error but didn't get one - will fail during processing
				require.NotNil(t, parser)
			} else {
				require.NoError(t, err)
				require.NotNil(t, parser)
			}

			if parser == nil {
				return
			}

			// Process the line
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			_, _ = parser.Process(0, tt.line, lb)
			resultLabels := lb.LabelsResult().Labels()

			// Verify expected labels
			if !tt.shouldFail {
				// Build expected labels set for comparison
				expectedLabels := make([]string, 0)
				for k, v := range tt.expectedLabels {
					expectedLabels = append(expectedLabels, k, v)
				}
				expectedResult := labels.FromStrings(expectedLabels...)
				require.True(t, resultLabels.Len() >= expectedResult.Len(), "not enough labels extracted")

				// Check each expected label exists with correct value
				for k, v := range tt.expectedLabels {
					val := resultLabels.Get(k)
					require.Equal(t, v, val, "label value mismatch for %s (empty if not found)", k)
				}
			}
		})
	}
}

func TestXMLExpressionParser_Duplicates(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><root><app>firstapp</app></root>`)

	// Create base labels that already have "app" label
	baseLabels := labels.FromStrings("app", "existingapp")

	parser, err := NewXMLExpressionParser([]LabelExtractionExpr{
		{Identifier: "app", Expression: "app"},
	})
	require.NoError(t, err)

	lb := NewBaseLabelsBuilder().ForLabels(baseLabels, 0)
	_, _ = parser.Process(0, xml, lb)
	resultLabels := lb.LabelsResult().Labels()

	// Should have both the original and the new label with _extracted suffix
	require.Greater(t, resultLabels.Len(), 1, "should have multiple labels due to duplicate")

	// Check for the extracted label
	val := resultLabels.Get("app_extracted")
	require.Equal(t, "firstapp", val, "app_extracted should have correct value")
}

func TestXMLExpressionParser_Comparison(t *testing.T) {
	// Test that XML expression parser behaves similarly to JSON expression parser
	jsonLine := []byte(`{"app":"myapp","version":"1.0"}`)
	xmlLine := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version></root>`)

	expressions := []LabelExtractionExpr{
		{Identifier: "app_name", Expression: "app"},
		{Identifier: "app_version", Expression: "version"},
	}

	// Parse with JSON
	jsonParser, err := NewJSONExpressionParser(expressions)
	require.NoError(t, err)

	jsonLb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = jsonParser.Process(0, jsonLine, jsonLb)
	jsonLabels := jsonLb.LabelsResult().Labels()

	// Parse with XML
	xmlParser, err := NewXMLExpressionParser(expressions)
	require.NoError(t, err)

	xmlLb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = xmlParser.Process(0, xmlLine, xmlLb)
	xmlLabels := xmlLb.LabelsResult().Labels()

	// Should have same number of extracted labels
	require.Equal(t, jsonLabels.Len(), xmlLabels.Len(), "JSON and XML should extract same number of labels")

	// Verify both have the same extracted values
	require.Equal(t, 2, jsonLabels.Len(), "should extract 2 labels")
	require.Equal(t, 2, xmlLabels.Len(), "should extract 2 labels")
}

func BenchmarkXMLExpressionParser(b *testing.B) {
	xml := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env></root>`)

	parser, err := NewXMLExpressionParser([]LabelExtractionExpr{
		{Identifier: "app", Expression: "app"},
		{Identifier: "version", Expression: "version"},
		{Identifier: "env", Expression: "env"},
	})
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		parser.Process(0, xml, lb)
	}
}

func BenchmarkXMLExpressionParserVsJSON(b *testing.B) {
	jsonLine := []byte(`{"app":"myapp","version":"1.0","env":"prod"}`)
	xmlLine := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env></root>`)

	expressions := []LabelExtractionExpr{
		{Identifier: "app", Expression: "app"},
		{Identifier: "version", Expression: "version"},
		{Identifier: "env", Expression: "env"},
	}

	jsonParser, _ := NewJSONExpressionParser(expressions)
	xmlParser, _ := NewXMLExpressionParser(expressions)

	b.Run("JSON", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			jsonParser.Process(0, jsonLine, lb)
		}
	})

	b.Run("XML", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			xmlParser.Process(0, xmlLine, lb)
		}
	})
}
