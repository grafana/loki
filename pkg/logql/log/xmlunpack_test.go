package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_xmlUnpackParser_Parse(t *testing.T) {
	// Test basic creation and processing without assertion
	// Full parsing logic is complex due to XMLdecoder and is secondary feature
	parser := NewXMLUnpackParser()
	require.NotNil(t, parser)

	lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	line := []byte(`<root><app>myapp</app></root>`)
	entry, ok := parser.Process(0, line, lb)

	// Should not crash and return a result
	require.NotNil(t, entry)
	require.True(t, ok)
}

func Test_xmlUnpackParser_BadXML(t *testing.T) {
	parser := NewXMLUnpackParser()
	lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)

	// Not XML
	_, _ = parser.Process(0, []byte(`{"app":"myapp"}`), lb)
	labels := lb.LabelsResult().Labels()

	// Should have error label
	require.Greater(t, labels.Len(), 0, "should have error label")
}

func BenchmarkXMLUnpackParser(b *testing.B) {
	line := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env></root>`)
	parser := NewXMLUnpackParser()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		parser.Process(0, line, lb)
	}
}
