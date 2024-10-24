package dash

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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

func TestBaseMetricName(t *testing.T) {

	cases := []struct {
		name     string
		builder  *RedMethodBuilder
		expected string
	}{
		{
			name: "simple case",
			builder: &RedMethodBuilder{
				metric: prometheus.NewHistogramVec(prometheus.HistogramOpts{
					Name: "test_metric",
				}, []string{}),
			},
			expected: "test_metric",
		},
		// Add more test cases here
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.builder.baseMetricName()
			require.Equal(t, tc.expected, actual)
		})
	}
}
