package xmlexpr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		wantPath  []string
		wantError bool
	}{
		{
			"simple field",
			"app",
			[]string{"app"},
			false,
		},
		{
			"nested field with dot",
			"pod.uuid",
			[]string{"pod", "uuid"},
			false,
		},
		{
			"three level nested",
			"pod.deployment.ref",
			[]string{"pod", "deployment", "ref"},
			false,
		},
		{
			"field with slash separator",
			"pod/uuid",
			[]string{"pod", "uuid"},
			false,
		},
		{
			"mixed separators",
			"pod.deployment/ref",
			[]string{"pod", "deployment", "ref"},
			false,
		},
		{
			"leading slash removed",
			"/pod/uuid",
			[]string{"pod", "uuid"},
			false,
		},
		{
			"empty expression",
			"",
			nil,
			true,
		},
		{
			"only slashes",
			"///",
			nil,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.expr)

			if tt.wantError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantPath, expr.Path)
		})
	}
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name    string
		xml     []byte
		expr    string
		want    []string
		wantErr bool
	}{
		{
			"extract simple element",
			[]byte(`<?xml version="1.0"?><root><app>myapp</app></root>`),
			"app",
			[]string{"myapp"},
			false,
		},
		{
			"extract nested element",
			[]byte(`<?xml version="1.0"?><root><pod><uuid>123</uuid></pod></root>`),
			"uuid",
			[]string{"123"},
			false,
		},
		{
			"extract deep nested",
			[]byte(`<?xml version="1.0"?><root><pod><deployment><ref>abc</ref></deployment></pod></root>`),
			"pod.deployment.ref",
			[]string{"abc"},
			false,
		},
		{
			"extract with prefix path",
			[]byte(`<?xml version="1.0"?><root><pod><uuid>uuid1</uuid><name>pod1</name></pod></root>`),
			"pod.uuid",
			[]string{"uuid1"},
			false,
		},
		{
			"extract no matches",
			[]byte(`<?xml version="1.0"?><root><app>test</app></root>`),
			"nonexistent",
			[]string{},
			false,
		},
		{
			"extract multiple same-level elements",
			[]byte(`<?xml version="1.0"?><root><item>value1</item><item>value2</item></root>`),
			"item",
			[]string{"value1", "value2"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := Parse(tt.expr)
			require.NoError(t, err)

			results, err := expr.Extract(tt.xml)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, results)
		})
	}
}

func TestParseString(t *testing.T) {
	expr, err := Parse("pod.uuid.value")
	require.NoError(t, err)
	require.Equal(t, "pod.uuid.value", expr.String())
}
