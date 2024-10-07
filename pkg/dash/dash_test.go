package dash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTpl(t *testing.T) {
	tpl := forceTpl("wip", `{{.Foo}} -- {{.Bar}} -- {{.Bazz}}`)

	var b strings.Builder
	require.NoError(t, tpl.Execute(&b, map[string]string{
		"Foo":  "foo",
		"Bazz": "baz",
	}))
	require.Equal(
		t,
		"foo -- <no value> -- baz",
		b.String(),
	)
}

func TestArgsMap(t *testing.T) {
	a := TemplateArgs{
		BaseName:        "base",
		TopologyLabels:  "top",
		PartitionFields: "part",
	}

	out := a.Map()

	require.Equal(t, "base", out["BaseName"])
	require.Equal(t, "top", out["TopologyLabels"])
	require.Equal(t, "part", out["PartitionFields"])
}
