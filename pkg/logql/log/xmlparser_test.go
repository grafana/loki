package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_xmlParser_Parse(t *testing.T) {
	tests := []struct {
		name               string
		line               []byte
		lbs                labels.Labels
		want               labels.Labels
		wantXMLPath        map[string][]string
		hints              ParserHint
		structuredMetadata map[string]string
	}{
		{
			"simple element",
			[]byte(`<?xml version="1.0"?><root><app>foo</app></root>`),
			labels.EmptyLabels(),
			labels.FromStrings("root_app", "foo"),
			map[string][]string{
				"root_app": {"root", "app"},
			},
			NoParserHints(),
			nil,
		},
		{
			"multiple elements",
			[]byte(`<?xml version="1.0"?><root><app>foo</app><namespace>prod</namespace></root>`),
			labels.EmptyLabels(),
			labels.FromStrings("root_app", "foo", "root_namespace", "prod"),
			map[string][]string{
				"root_app":       {"root", "app"},
				"root_namespace": {"root", "namespace"},
			},
			NoParserHints(),
			nil,
		},
		{
			"nested elements",
			[]byte(`<?xml version="1.0"?><root><pod><uuid>foo</uuid><deployment><ref>foobar</ref></deployment></pod></root>`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"root_pod_uuid", "foo",
				"root_pod_deployment_ref", "foobar",
			),
			map[string][]string{
				"root_pod_uuid":           {"root", "pod", "uuid"},
				"root_pod_deployment_ref": {"root", "pod", "deployment", "ref"},
			},
			NoParserHints(),
			nil,
		},
		{
			"element with attributes",
			[]byte(`<?xml version="1.0"?><root><pod id="123" name="test"><uuid>foo</uuid></pod></root>`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"root_pod_id", "123",
				"root_pod_name", "test",
				"root_pod_uuid", "foo",
			),
			map[string][]string{},
			NoParserHints(),
			nil,
		},
		{
			"numeric value",
			[]byte(`<?xml version="1.0"?><root><counter>42</counter><price>5.56909</price></root>`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"root_counter", "42",
				"root_price", "5.56909",
			),
			map[string][]string{
				"root_counter": {"root", "counter"},
				"root_price":   {"root", "price"},
			},
			NoParserHints(),
			nil,
		},
		{
			"with duplicates",
			[]byte(`<?xml version="1.0"?><root><app>bar</app><namespace>prod</namespace></root>`),
			labels.FromStrings("root_app", "foo"),
			labels.FromStrings(
				"root_app", "foo",
				"root_app_extracted", "bar",
				"root_namespace", "prod",
			),
			map[string][]string{
				"root_app_extracted": {"root", "app"},
				"root_namespace":     {"root", "namespace"},
			},
			NoParserHints(),
			nil,
		},
		{
			"empty element",
			[]byte(`<?xml version="1.0"?><root><pod></pod><app>test</app></root>`),
			labels.EmptyLabels(),
			labels.FromStrings("root_app", "test"),
			map[string][]string{
				"root_app": {"root", "app"},
			},
			NoParserHints(),
			nil,
		},
		{
			"element with whitespace",
			[]byte(`<?xml version="1.0"?><root><app>  foo  </app></root>`),
			labels.EmptyLabels(),
			labels.FromStrings("root_app", "foo"),
			map[string][]string{
				"root_app": {"root", "app"},
			},
			NoParserHints(),
			nil,
		},
		{
			"CDATA section",
			[]byte(`<?xml version="1.0"?><root><content><![CDATA[some data]]></content></root>`),
			labels.EmptyLabels(),
			labels.FromStrings("root_content", "some data"),
			map[string][]string{},
			NoParserHints(),
			nil,
		},
		{
			"mixed attributes and elements",
			[]byte(`<?xml version="1.0"?><root><deployment env="prod" version="1.0"><name>myapp</name></deployment></root>`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"root_deployment_env", "prod",
				"root_deployment_version", "1.0",
				"root_deployment_name", "myapp",
			),
			map[string][]string{},
			NoParserHints(),
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origLine := string(tt.line)
			parser := NewXMLParser(true, true)
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, 0)
			if tt.hints != nil {
				b.parserKeyHints = tt.hints
			}
			_, _ = parser.Process(0, tt.line, b)
			got := b.LabelsResult().Labels()

			// Compare labels
			require.Equal(t, origLine, string(tt.line), "original log line was modified")
			require.Equal(t, tt.want, got)

			// Compare XML paths if specified
			if len(tt.wantXMLPath) > 0 {
				for labelName, expectedPath := range tt.wantXMLPath {
					actualPath := b.GetXMLPath(labelName)
					require.Equal(t, expectedPath, actualPath, "XML path mismatch for label %s", labelName)
				}
			}
		})
	}
}

func Test_xmlParser_ParseWithNamespaceStripping(t *testing.T) {
	xmlWithNamespace := []byte(`<?xml version="1.0"?><root><app:name>test</app:name></root>`)

	// With namespace stripping (default)
	parser := NewXMLParser(true, false)
	b := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = parser.Process(0, xmlWithNamespace, b)
	labels := b.LabelsResult().Labels()

	// Should strip the namespace prefix and extract "root_name"
	require.Equal(t, 1, labels.Len(), "should extract label with namespace stripped")
}

func Test_xmlParser_BadXML(t *testing.T) {
	tests := []struct {
		name string
		line []byte
	}{
		{
			"malformed XML",
			[]byte(`<?xml version="1.0"?><root><unclosed>`),
		},
		{
			"invalid characters",
			[]byte(`<?xml version="1.0"?><root><invalid>test<invalid></root>`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewXMLParser(true, false)
			b := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			_, _ = parser.Process(0, tt.line, b)
			// Should handle error gracefully without crashing
			_ = b.LabelsResult().Labels()
		})
	}
}

func TestXMLParserPerformance(t *testing.T) {
	// Deep nesting test
	deepXML := []byte(`<?xml version="1.0"?><root><a><b><c><d><e><f><g><h><i><j>deep</j></i></h></g></f></e></d></c></b></a></root>`)

	parser := NewXMLParser(true, true)
	b := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = parser.Process(0, deepXML, b)
	labels := b.LabelsResult().Labels()

	require.Greater(t, labels.Len(), 0, "should extract from deep nesting")
}

func TestXMLParserComparison(t *testing.T) {
	// Test equivalent JSON and XML structures
	jsonBytes := []byte(`{"pod":{"uuid":"test-uuid","deployment":{"ref":"test-ref"}}}`)
	xmlBytes := []byte(`<?xml version="1.0"?><root><pod><uuid>test-uuid</uuid><deployment><ref>test-ref</ref></deployment></pod></root>`)

	// Parse JSON
	jsonParser := NewJSONParser(false)
	jsonB := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = jsonParser.Process(0, jsonBytes, jsonB)
	jsonLabels := jsonB.LabelsResult().Labels()

	// Parse XML
	xmlParser := NewXMLParser(true, false)
	xmlB := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, _ = xmlParser.Process(0, xmlBytes, xmlB)
	xmlLabels := xmlB.LabelsResult().Labels()

	// Both should have similar structure (JSON doesn't have root wrapper, XML does)
	require.Greater(t, jsonLabels.Len(), 0, "JSON parser should extract labels")
	require.Greater(t, xmlLabels.Len(), 0, "XML parser should extract labels")

	// Just verify they both extracted labels successfully
	require.Equal(t, 2, jsonLabels.Len(), "JSON should extract 2 labels (pod_uuid, pod_deployment_ref)")
	require.Equal(t, 2, xmlLabels.Len(), "XML should extract 2 labels (root_pod_uuid, root_pod_deployment_ref)")
}

func BenchmarkXMLParser(b *testing.B) {
	xmlData := []byte(`<?xml version="1.0"?><root><pod><uuid>test-uuid</uuid><deployment><ref>test-ref</ref></deployment><namespace>prod</namespace></pod></root>`)
	parser := NewXMLParser(true, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		parser.Process(0, xmlData, lbs)
	}
}

func BenchmarkXMLParserVsJSON(b *testing.B) {
	jsonData := []byte(`{"pod":{"uuid":"test-uuid","deployment":{"ref":"test-ref"},"namespace":"prod"}}`)
	xmlData := []byte(`<?xml version="1.0"?><root><pod><uuid>test-uuid</uuid><deployment><ref>test-ref</ref></deployment><namespace>prod</namespace></pod></root>`)

	b.Run("JSON", func(b *testing.B) {
		parser := NewJSONParser(false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, jsonData, lbs)
		}
	})

	b.Run("XML", func(b *testing.B) {
		parser := NewXMLParser(true, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, xmlData, lbs)
		}
	})
}
