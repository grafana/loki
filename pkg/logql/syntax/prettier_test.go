package syntax

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	MaxCharsPerLine = 20

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
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestFormat_VectorAggregation(t *testing.T) {
	MaxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "sum",
			in:   `sum(count_over_time({foo="bar",namespace="loki",instance="localhost"}[5m])) by (container)`,
			exp: `sum by (container)(
  count_over_time(
    {foo="bar", namespace="loki", instance="localhost"} [5m]
  )
)`,
		},
		{
			name: "topk",
			in:   `topk(5, count_over_time({foo="bar",namespace="loki",instance="localhost"}[5m])) by (container)`,
			exp: `topk by (container)(
  5,
  count_over_time(
    {foo="bar", namespace="loki", instance="localhost"} [5m]
  )
)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestFormat_LabelAggregation(t *testing.T) {
	orig := MaxCharsPerLine
	t.Cleanup(func() { MaxCharsPerLine = orig })

	cases := []struct {
		name     string
		in       string
		maxChars int
		exp      string
	}{
		{
			name:     "approx_count_distinct",
			in:       `approx_count_distinct(mac, {job="loki", instance="localhost"}|logfmt[1m]) by (version)`,
			maxChars: 20,
			exp: `approx_count_distinct(
  mac,
  {job="loki", instance="localhost"}
    | logfmt [1m]
) by (version)`,
		},
		{
			name:     "approx_count_distinct_ungrouped",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d])`,
			maxChars: 20,
			exp: `approx_count_distinct(
  mac,
  {foo="bar"} [1d]
)`,
		},
		{
			name:     "approx_count_distinct_empty_by",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			maxChars: 20,
			exp: `approx_count_distinct(
  mac,
  {foo="bar"} [1d]
) by ()`,
		},
		{
			name:     "compact omitted grouping uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d])`,
			maxChars: 100,
			exp:      `approx_count_distinct(mac,{foo="bar"}[1d])`,
		},
		{
			name:     "compact empty by uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			maxChars: 100,
			exp:      `approx_count_distinct(mac,{foo="bar"}[1d]) by ()`,
		},
		{
			name:     "compact by labels uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by (version)`,
			maxChars: 100,
			exp:      `approx_count_distinct(mac,{foo="bar"}[1d]) by (version)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			MaxCharsPerLine = c.maxChars
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			require.Equal(t, c.exp, Prettify(expr))
		})
	}
}

func TestLabelAggregationExpr_String(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "omitted grouping",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d])`,
			want: `approx_count_distinct(mac,{foo="bar"}[1d])`,
		},
		{
			name: "empty by",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			want: `approx_count_distinct(mac,{foo="bar"}[1d]) by ()`,
		},
		{
			name: "by labels",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d]) by (version)`,
			want: `approx_count_distinct(mac,{foo="bar"}[1d]) by (version)`,
		},
		{
			name: "pipeline offset and multiple labels",
			in:   `approx_count_distinct(mac, {foo="bar"} | json [1h] offset 5m) by (version, region)`,
			want: `approx_count_distinct(mac,{foo="bar"} | json[1h] offset 5m0s) by (version,region)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, expr.String())
		})
	}
}

func TestFormat_CountDistinctSketch(t *testing.T) {
	orig := MaxCharsPerLine
	t.Cleanup(func() { MaxCharsPerLine = orig })

	cases := []struct {
		name     string
		in       string
		maxChars int
		exp      string
	}{
		{
			name:     "by labels",
			in:       `approx_count_distinct(mac, {job="loki", instance="localhost"}|json[1h]) by (version)`,
			maxChars: 20,
			exp: `__count_distinct_sketch__(
  mac,
  {job="loki", instance="localhost"}
    | json [1h]
) by (version)`,
		},
		{
			name:     "omitted grouping",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d])`,
			maxChars: 20,
			exp: `__count_distinct_sketch__(
  mac,
  {foo="bar"} [1d]
)`,
		},
		{
			name:     "empty by",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			maxChars: 20,
			exp: `__count_distinct_sketch__(
  mac,
  {foo="bar"} [1d]
) by ()`,
		},
		{
			name:     "compact omitted grouping uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d])`,
			maxChars: 100,
			exp:      `__count_distinct_sketch__(mac,{foo="bar"}[1d])`,
		},
		{
			name:     "compact empty by uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			maxChars: 100,
			exp:      `__count_distinct_sketch__(mac,{foo="bar"}[1d]) by ()`,
		},
		{
			name:     "compact by labels uses String",
			in:       `approx_count_distinct(mac, {foo="bar"}[1d]) by (version)`,
			maxChars: 100,
			exp:      `__count_distinct_sketch__(mac,{foo="bar"}[1d]) by (version)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			MaxCharsPerLine = c.maxChars
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			labelAgg, ok := expr.(*LabelAggregationExpr)
			require.True(t, ok)
			require.Equal(t, c.exp, Prettify(NewCountDistinctSketchFromLabelAggregation(labelAgg)))
		})
	}
}

func TestCountDistinctSketchExpr_String(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "omitted grouping",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d])`,
			want: `__count_distinct_sketch__(mac,{foo="bar"}[1d])`,
		},
		{
			name: "empty by",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d]) by ()`,
			want: `__count_distinct_sketch__(mac,{foo="bar"}[1d]) by ()`,
		},
		{
			name: "by labels",
			in:   `approx_count_distinct(mac, {foo="bar"}[1d]) by (version)`,
			want: `__count_distinct_sketch__(mac,{foo="bar"}[1d]) by (version)`,
		},
		{
			name: "pipeline offset and multiple labels",
			in:   `approx_count_distinct(mac, {foo="bar"} | json [1h] offset 5m) by (version, region)`,
			want: `__count_distinct_sketch__(mac,{foo="bar"} | json[1h] offset 5m0s) by (version,region)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			labelAgg, ok := expr.(*LabelAggregationExpr)
			require.True(t, ok)
			require.Equal(t, c.want, NewCountDistinctSketchFromLabelAggregation(labelAgg).String())
		})
	}
}

func TestFormat_LabelReplace(t *testing.T) {
	MaxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "label_replace",
			in:   `label_replace(rate({job="api-server",service="a:c"}|= "err" [5m]), "foo", "$1", "service", "(.*):.*")`,
			exp: `label_replace(
  rate(
    {job="api-server", service="a:c"}
      |= "err" [5m]
  ),
  "foo",
  "$1",
  "service",
  "(.*):.*"
)`,
		},
		{
			name: "label_replace_nested",
			in:   `label_replace(label_replace(rate({job="api-server",service="a:c"}|= "err" [5m]), "foo", "$1", "service", "(.*):.*"), "foo", "$1", "service", "(.*):.*")`,
			exp: `label_replace(
  label_replace(
    rate(
      {job="api-server", service="a:c"}
        |= "err" [5m]
    ),
    "foo",
    "$1",
    "service",
    "(.*):.*"
  ),
  "foo",
  "$1",
  "service",
  "(.*):.*"
)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestFormat_BinOp(t *testing.T) {
	MaxCharsPerLine = 20

	cases := []struct {
		name string
		in   string
		exp  string
	}{
		{
			name: "single binop",
			in:   `sum(rate({job="loki", namespace="loki-prod", instance="localhost"}[5m]))/sum(count_over_time({job="loki", namespace="loki-prod", instance="localhost"}[5m]))`,
			exp: `  sum(
    rate(
      {job="loki", namespace="loki-prod", instance="localhost"} [5m]
    )
  )
/
  sum(
    count_over_time(
      {job="loki", namespace="loki-prod", instance="localhost"} [5m]
    )
  )`,
		},
		{
			name: "multiple binops",
			in:   `sum(rate({job="loki"}[5m])) + sum(rate({job="loki-dev"}[5m])) / sum(rate({job="loki-prod"}[5m]))`,
			exp: `  sum(
    rate(
      {job="loki"} [5m]
    )
  )
+
    sum(
      rate(
        {job="loki-dev"} [5m]
      )
    )
  /
    sum(
      rate(
        {job="loki-prod"} [5m]
      )
    )`,
		},
		// NOTE: LogQL binary arithmetic ops have following precedences rules
		// 1. * / % - higher priority
		// 2. + -  - lower priority.
		// 3. Between same priority ops, whichever comes first takes precedence.
		// Following `_precedence*` tests makes sure LogQL formatter respects that.
		{
			name: "multiple binops check precedence",
			in:   `sum(rate({job="loki"}[5m])) / sum(rate({job="loki-dev"}[5m])) + sum(rate({job="loki-prod"}[5m]))`,
			exp: `    sum(
      rate(
        {job="loki"} [5m]
      )
    )
  /
    sum(
      rate(
        {job="loki-dev"} [5m]
      )
    )
+
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
		{
			name: "multiple binops check precedence2",
			in:   `sum(rate({job="loki"}[5m])) - sum(rate({job="loki-stage"}[5m])) / sum(rate({job="loki-dev"}[5m])) + sum(rate({job="loki-prod"}[5m]))`,
			exp: `    sum(
      rate(
        {job="loki"} [5m]
      )
    )
  -
      sum(
        rate(
          {job="loki-stage"} [5m]
        )
      )
    /
      sum(
        rate(
          {job="loki-dev"} [5m]
        )
      )
+
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
		{
			name: "multiple binops check precedence3",
			in:   `sum(rate({job="loki"}[5m])) - sum(rate({job="loki-stage"}[5m])) % sum(rate({job="loki-dev"}[5m])) + sum(rate({job="loki-prod"}[5m]))`,
			exp: `    sum(
      rate(
        {job="loki"} [5m]
      )
    )
  -
      sum(
        rate(
          {job="loki-stage"} [5m]
        )
      )
    %
      sum(
        rate(
          {job="loki-dev"} [5m]
        )
      )
+
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
		{
			name: "multiple binops check precedence4",
			in:   `sum(rate({job="loki"}[5m])) / sum(rate({job="loki-stage"}[5m])) % sum(rate({job="loki-dev"}[5m])) + sum(rate({job="loki-prod"}[5m]))`,
			exp: `      sum(
        rate(
          {job="loki"} [5m]
        )
      )
    /
      sum(
        rate(
          {job="loki-stage"} [5m]
        )
      )
  %
    sum(
      rate(
        {job="loki-dev"} [5m]
      )
    )
+
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
		{
			name: "multiple binops check precedence5",
			in:   `sum(rate({job="loki"}[5m])) / sum(rate({job="loki-stage"}[5m])) % sum(rate({job="loki-dev"}[5m])) * sum(rate({job="loki-prod"}[5m]))`,
			exp: `      sum(
        rate(
          {job="loki"} [5m]
        )
      )
    /
      sum(
        rate(
          {job="loki-stage"} [5m]
        )
      )
  %
    sum(
      rate(
        {job="loki-dev"} [5m]
      )
    )
*
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
		{
			name: "binops with options", // options - on, ignoring, group_left, group_right
			in:   `sum(rate({job="loki"}[5m])) * on(instance, job) group_left (node) sum(rate({job="loki-prod"}[5m]))`,
			exp: `  sum(
    rate(
      {job="loki"} [5m]
    )
  )
* on (instance, job) group_left (node)
  sum(
    rate(
      {job="loki-prod"} [5m]
    )
  )`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expr, err := ParseExpr(c.in)
			require.NoError(t, err)
			got := Prettify(expr)
			assert.Equal(t, c.exp, got)
		})
	}
}
