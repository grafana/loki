package syntax

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TODO:
// 1. test with offset. - DONE
// 2. test with jsonparser - DONE
// 3. test with unwrap - DONE
// 4. nested aggregation -
// 5. binary op and nested
// 6. unary ops
// 7. nested funcs + nested aggregation + nested binary ops.

func TestPrettify(t *testing.T) {
	maxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "basic stream selector",
			in:   `{job="loki", instance="localhost"}`,
			exp:  `{job="loki", instance="localhost"}`,
		},
		{
			name: "pipeline_label_filter",
			in:   `{job="loki", instance="localhost"}|logfmt|level="error" `,
			exp: `{job="loki", instance="localhost"}
  | logfmt
  | level="error"`,
		},
		{
			name: "pipeline_line_filter",
			in:   `{job="loki", instance="localhost"}|= "error" != "memcached" |= ip("192.168.0.1") |logfmt`,
			exp: `{job="loki", instance="localhost"}
  |= "error"
  != "memcached"
  |= ip("192.168.0.1")
  | logfmt`,
		},
		{
			name: "pipeline_line_format",
			in:   `{job="loki", instance="localhost"}|logfmt|line_format "{{.error}}"`,
			exp: `{job="loki", instance="localhost"}
  | logfmt
  | line_format "{{.error}}"`,
		},
		{
			name: "pipeline_label_format",
			in:   `{job="loki", instance="localhost"}|logfmt|label_format dst="{{.src}}"`,
			exp: `{job="loki", instance="localhost"}
  | logfmt
  | label_format dst="{{.src}}"`,
		},
		{
			name: "aggregation",
			in:   `count_over_time({job="loki", instance="localhost"}|logfmt[1m])`,
			exp: `count_over_time(
  {job="loki", instance="localhost"}
    | logfmt [1m]
)`,
		},
		{
			name: "aggregation_with_offset",
			in:   `count_over_time({job="loki", instance="localhost"}|= "error"[5m] offset 20m)`,
			exp: `count_over_time(
  {job="loki", instance="localhost"}
    |= "error" [5m] offset 20m
)`,
		},
		{
			name: "unwrap",
			in:   `quantile_over_time(0.99,{container="ingress-nginx",service="hosted-grafana"}| json| unwrap response_latency_seconds| __error__=""[1m]) by (cluster)`,
			exp: `quantile_over_time(
  0.99,
  {container="ingress-nginx", service="hosted-grafana"}
    | json
    | unwrap response_latency_seconds
    | __error__="" [1m]
) by (cluster)`,
		},
		{
			name: "pipeline_aggregation_line_filter",
			in:   `count_over_time({job="loki", instance="localhost"}|= "error" != "memcached" |= ip("192.168.0.1") |logfmt[1m])`,
			exp: `count_over_time(
  {job="loki", instance="localhost"}
    |= "error"
    != "memcached"
    |= ip("192.168.0.1")
    | logfmt [1m]
)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			fmt.Printf("%#v\n", expr)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestPretty2(t *testing.T) {
	maxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "jsonparserExpr",
			in:   `{job="loki", namespace="loki-prod", container="nginx-ingress"}| json first_server="servers[0]", ua="request.headers[\"User-Agent\"]" | level="error"`,
			exp: `{job="loki", namespace="loki-prod", container="nginx-ingress"}
  | json first_server="servers[0]",ua="request.headers[\"User-Agent\"]"
  | level="error"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			fmt.Printf("%#v\n", expr)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}
