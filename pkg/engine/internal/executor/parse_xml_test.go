package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestParseXMLLine(t *testing.T) {
	parser := newXMLParser(false)

	tests := []struct {
		name        string
		line        string
		requested   map[string]struct{}
		stripNS     bool
		expected    map[string]string
	}{
		{
			"simple element",
			`<root><app>myapp</app></root>`,
			map[string]struct{}{},
			false,
			map[string]string{"root_app": "myapp"},
		},
		{
			"nested elements",
			`<root><pod><uuid>abc123</uuid></pod></root>`,
			map[string]struct{}{},
			false,
			map[string]string{"root_pod_uuid": "abc123"},
		},
		{
			"with attributes",
			`<root><pod id="pod-1" name="test"></pod></root>`,
			map[string]struct{}{},
			false,
			map[string]string{
				"pod_id":   "pod-1",
				"pod_name": "test",
			},
		},
		{
			"multiple elements",
			`<root><app>app1</app><env>prod</env></root>`,
			map[string]struct{}{},
			false,
			map[string]string{
				"root_app": "app1",
				"root_env": "prod",
			},
		},
		{
			"requested keys filter",
			`<root><app>app1</app><env>prod</env><pod>pod1</pod></root>`,
			map[string]struct{}{"root_app": {}},
			false,
			map[string]string{"root_app": "app1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parser.parseXMLLine([]byte(tc.line), tc.requested, tc.stripNS)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestBuildXMLColumns(t *testing.T) {
	// Create test data
	alloc := memory.DefaultAllocator
	builder := array.NewStringBuilder(alloc)
	defer builder.Release()

	builder.Append(`<root><app>app1</app><version>1.0</version></root>`)
	builder.Append(`<root><app>app2</app><version>2.0</version></root>`)
	builder.Append(`<root><app>app3</app><version>3.0</version></root>`)

	input := builder.NewStringArray()
	defer input.Release()

	// Build columns
	keys, arrays := buildXMLColumns(input, nil, false)

	// Verify keys were extracted
	require.NotEmpty(t, keys)
	require.Len(t, arrays, len(keys))

	// Verify arrays have correct length
	for _, arr := range arrays {
		require.Equal(t, 3, arr.Len())
		defer arr.Release()
	}
}

func TestBuildXMLColumnsWithRequestedKeys(t *testing.T) {
	// Create test data
	alloc := memory.DefaultAllocator
	builder := array.NewStringBuilder(alloc)
	defer builder.Release()

	builder.Append(`<root><app>app1</app><env>prod</env><version>1.0</version></root>`)
	builder.Append(`<root><app>app2</app><env>staging</env><version>2.0</version></root>`)

	input := builder.NewStringArray()
	defer input.Release()

	// Build columns with specific requested keys
	requestedKeys := []string{"root_app", "root_env"}
	keys, arrays := buildXMLColumns(input, requestedKeys, false)

	// Verify correct columns
	require.Len(t, arrays, len(keys))

	// Each array should have 2 rows
	for _, arr := range arrays {
		require.Equal(t, 2, arr.Len())
		defer arr.Release()
	}
}

func TestXMLParserStripNamespaces(t *testing.T) {
	parser := newXMLParser(true) // strip namespaces

	line := `<root><ns:app>myapp</ns:app><pod ns:id="123"></pod></root>`
	result, err := parser.parseXMLLine([]byte(line), make(map[string]struct{}), true)

	require.NoError(t, err)
	// Should have stripped the "ns:" prefix
	require.NotNil(t, result)
}

func TestXMLParserEmptyInput(t *testing.T) {
	parser := newXMLParser(false)

	tests := []struct {
		name string
		line string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"not xml", "not xml at all"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parser.parseXMLLine([]byte(tc.line), nil, false)
			require.NoError(t, err)
			require.Empty(t, result)
		})
	}
}
