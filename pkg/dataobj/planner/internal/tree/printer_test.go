package tree

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrinter(t *testing.T) {
	root := NewNode("Root", "")
	lvl1 := root.AddChild("Merge", "foo", []Property{
		{Key: "key_a", Values: []any{"value_a"}, IsMultiValue: true},
		{Key: "key_b", Values: []any{"value_b", "value_c"}, IsMultiValue: true},
	})
	lvl2 := lvl1.AddChild("Product", "foobar", []Property{
		{Key: "relations", Values: []any{"foo", "bar"}, IsMultiValue: true},
	})
	lvl2.AddChild("Scan", "foo", []Property{
		{Key: "selector", Values: []any{`{env="prod", region=".+"}`}},
	})
	lvl2.AddChild("Scan", "bar", []Property{
		{Key: "selector", Values: []any{`{env="dev", region=".+"}`}},
	})
	_ = lvl1.AddChild("Scan", "baz", []Property{})

	b := &strings.Builder{}
	p := NewPrinter(b)
	p.Print(root)

	t.Log("\n" + b.String())
	expected := `
Root
└── Merge #foo key_a=(value_a) key_b=(value_b, value_c)
    ├── Product #foobar relations=(foo, bar)
    │   ├── Scan #foo selector={env="prod", region=".+"}
    │   └── Scan #bar selector={env="dev", region=".+"}
    └── Scan #baz
`
	require.Equal(t, expected, "\n"+b.String())
}
