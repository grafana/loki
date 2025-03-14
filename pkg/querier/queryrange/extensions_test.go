package queryrange

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func Test_setHeader(t *testing.T) {
	headers := []queryrangebase.PrometheusResponseHeader{}

	headers = setHeader(headers, "foo", "bar")
	require.Len(t, headers, 1)
	headers = setHeader(headers, "other", "bar")
	require.Len(t, headers, 2)
	require.ElementsMatch(t, headers, []queryrangebase.PrometheusResponseHeader{{Name: "foo", Values: []string{"bar"}}, {Name: "other", Values: []string{"bar"}}})

	headers = setHeader(headers, "foo", "changed")
	require.Len(t, headers, 2)
	require.ElementsMatch(t, headers, []queryrangebase.PrometheusResponseHeader{{Name: "foo", Values: []string{"changed"}}, {Name: "other", Values: []string{"bar"}}})
}
