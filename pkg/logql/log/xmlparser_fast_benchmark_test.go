package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
)

// BenchmarkFastXMLParserOnly - benchmark FastXMLParser in isolation
func BenchmarkFastXMLParserOnly(b *testing.B) {
	line := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env><pod><uuid>abc-123</uuid><namespace>default</namespace></pod></root>`)
	parser := NewFastXMLParser(false, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		parser.Process(0, line, lb)
	}
}

// BenchmarkXMLParserVsFastXMLParser - direct comparison
func BenchmarkXMLParserVsFastXMLParser(b *testing.B) {
	line := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env><pod><uuid>abc-123</uuid><namespace>default</namespace></pod></root>`)

	b.Run("Original", func(b *testing.B) {
		parser := NewXMLParser(false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, line, lb)
		}
	})

	b.Run("FastOptimized", func(b *testing.B) {
		parser := NewFastXMLParser(false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, line, lb)
		}
	})
}
