package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestUltraFastXMLParser_Basic(t *testing.T) {
	tests := []struct {
		name string
		line []byte
	}{
		{
			"simple element",
			[]byte(`<?xml version="1.0"?><root><app>foo</app></root>`),
		},
		{
			"multiple elements",
			[]byte(`<?xml version="1.0"?><root><app>foo</app><env>prod</env></root>`),
		},
		{
			"nested elements",
			[]byte(`<?xml version="1.0"?><root><pod><uuid>foo</uuid></pod></root>`),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parser := NewUltraFastXMLParser(false, false)
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			_, ok := parser.Process(0, tc.line, lb)
			require.True(t, ok)
			// Just verify it doesn't crash
		})
	}
}

// BenchmarkUltraFastXMLParser - benchmark the ultra-optimized parser
func BenchmarkUltraFastXMLParser(b *testing.B) {
	line := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env><pod><uuid>abc-123</uuid><namespace>default</namespace></pod></root>`)
	parser := NewUltraFastXMLParser(false, false)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
		parser.Process(0, line, lb)
	}
}

// BenchmarkXMLParserComparison3Way - compare all three implementations
func BenchmarkXMLParserComparison3Way(b *testing.B) {
	line := []byte(`<?xml version="1.0"?><root><app>myapp</app><version>1.0</version><env>prod</env><pod><uuid>abc-123</uuid><namespace>default</namespace></pod></root>`)

	b.Run("Original", func(b *testing.B) {
		parser := NewXMLParser(false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, line, lb)
		}
	})

	b.Run("Fast", func(b *testing.B) {
		parser := NewFastXMLParser(false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, line, lb)
		}
	})

	b.Run("UltraFast", func(b *testing.B) {
		parser := NewUltraFastXMLParser(false, false)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lb := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
			parser.Process(0, line, lb)
		}
	})
}
