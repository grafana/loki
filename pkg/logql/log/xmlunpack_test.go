package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_xmlUnpackParser_Parse(t *testing.T) {
	tests := []struct {
		name     string
		line     []byte
		checkKey string
		checkVal string
	}{
		{
			"basic unpack",
			[]byte(`<root><app>myapp</app><version>1.0</version></root>`),
			"app",
			"myapp",
		},
		{
			"multiple elements",
			[]byte(`<root><app>myapp</app><env>prod</env><version>1.0</version></root>`),
			"env",
			"prod",
		},
		{
			"with whitespace",
			[]byte(`<root><app>  myapp  </app></root>`),
			"app",
			"myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewXMLUnpackParser()
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			_, _ = parser.Process(0, tt.line, lb)
			got := lb.LabelsResult().Labels()

			// Check that expected key is extracted
			val := got.Get(tt.checkKey)
			require.Equal(t, tt.checkVal, val, "should extract %s=%s", tt.checkKey, tt.checkVal)
		})
	}
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
