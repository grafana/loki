package hintprovider

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestSupportedQuery(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		ngramLength int
		expected    []string
	}{
		{
			name:        "supports single exact line filter",
			query:       `{app="foo"} |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports exact line filter before logfmt parser",
			query:       `{label="value"} |= "matching text" | logfmt`,
			ngramLength: 6,
			expected:    []string{"matching text"},
		},
		{
			name:        "supports exact line filter before logfmt parser and negative label filter",
			query:       `{label="value"} |= "matching text" | logfmt | parsed_label != "excluded text"`,
			ngramLength: 6,
			expected:    []string{"matching text"},
		},
		{
			name:        "supports multiple exact filters",
			query:       `{app="foo"} |= "database timeout" |= "request id"`,
			ngramLength: 6,
			expected:    []string{"database timeout", "request id"},
		},
		{
			name:        "supports exact filter literal when followed by unsupported regex",
			query:       `{app="foo"} |= "database timeout" |~ "error|warning|panic"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports exact filter literal when followed by negative filter",
			query:       `{app="foo"} |= "database timeout" != "debug noise"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports unsupported regex followed by exact filter literal",
			query:       `{app="foo"} |~ "error|warning|panic" |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "rejects too short exact filter",
			query:       `{app="foo"} |= "db"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "uses remaining filter literal when one is too short",
			query:       `{app="foo"} |= "db" |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "rejects regex filter with no mandatory filter literal",
			query:       `{app="foo"} |~ "error.*"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports case-insensitive literal regex filter",
			query:       `{app="foo"} |~ "(?i)database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports multiple literal regex filters",
			query:       `{app="foo"} |~ "(?i)database timeout" |~ "request id"`,
			ngramLength: 6,
			expected:    []string{"database timeout", "request id"},
		},
		{
			name:        "rejects too short case-insensitive literal regex filter",
			query:       `{app="foo"} |~ "(?i)short"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects case-insensitive regex filter with metacharacters",
			query:       `{app="foo"} |~ "(?i)error.*"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects case-insensitive regex filter with character class",
			query:       `{app="foo"} |~ "(?i)hello[world]"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects empty case-insensitive regex filter",
			query:       `{app="foo"} |~ "(?i)"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports mid-pattern case-insensitive flag",
			query:       `{app="foo"} |~ "foo(?i)bar"`,
			ngramLength: 6,
			expected:    []string{"foobar"},
		},
		{
			name:        "supports nested capture groups with mixed case flags",
			query:       `{app="foo"} |~ "abc((def(?i)jhl)(?i)mnop)"`,
			ngramLength: 6,
			expected:    []string{"abcdefjhlmnop"},
		},
		{
			name:        "supports case-sensitive literal regex filter",
			query:       `{app="foo"} |~ "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "rejects case-insensitive regex with anchors",
			query:       `{app="foo"} |~ "(?i)^database$"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects case-insensitive regex with alternation",
			query:       `{app="foo"} |~ "(?i)timeout|error"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects case-insensitive regex with dot wildcard",
			query:       `{app="foo"} |~ "(?i)data.ase"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects positive OR line filter with no mandatory filter literal",
			query:       `{app="foo"} |= "database timeout" or "request id"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports mandatory filter literal before positive OR branch",
			query:       `{app="foo"} |= "seed text" |= "option_a" or "option_b"`,
			ngramLength: 6,
			expected:    []string{"seed text"},
		},
		{
			name:        "rejects not-equal filter",
			query:       `{app="foo"} != "error"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects query with no line filter",
			query:       `{app="foo"}`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "deduplicates repeated exact filter literals",
			query:       `{app="foo"} |= "database timeout" |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports pre-parser exact label filter",
			query:       `{app="foo"} | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports pre-parser case-insensitive regex label filter",
			query:       `{app="foo"} | trace_id=~"(?i)abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports line and pre-parser label filters",
			query:       `{app="foo"} |= "database timeout" | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"database timeout", "abc123def456"},
		},
		{
			name:        "supports and-ed pre-parser label filters",
			query:       `{app="foo"} | foo="abcdef", bar="ghijkl"`,
			ngramLength: 6,
			expected:    []string{"abcdef", "ghijkl"},
		},
		{
			name:        "keeps eligible label filter when and-ed with unsupported regex",
			query:       `{app="foo"} | foo="abcdef", bar=~"error.*"`,
			ngramLength: 6,
			expected:    []string{"abcdef"},
		},
		{
			name:        "supports pre-parser and post-logfmt label filters",
			query:       `{app="foo"} | trace_id="abc123def456" | logfmt | level="errorrr"`,
			ngramLength: 6,
			expected:    []string{"abc123def456", "errorrr"},
		},
		{
			name:        "supports label filter after logfmt parser",
			query:       `{app="foo"} | logfmt | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports label filter after json parser using the full value",
			query:       `{app="foo"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`,
			ngramLength: 6,
			expected:    []string{"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports line filter and post-parser json label filter",
			query:       `{app="foo"} |= "database timeout" | json | level="errorrr"`,
			ngramLength: 6,
			expected:    []string{"database timeout", "errorrr"},
		},
		{
			name:        "rejects or-ed label filters",
			query:       `{app="foo"} | status="success" or status="goodenough"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "keeps line filter when label filter or is present",
			query:       `{app="foo"} |= "seed text" | status="success" or status="goodenough"`,
			ngramLength: 6,
			expected:    []string{"seed text"},
		},
		{
			name:        "rejects not-equal label filter",
			query:       `{app="foo"} | status!="errorrr"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects non-literal regex label filter",
			query:       `{app="foo"} | msg=~"error.*"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects too short label filter",
			query:       `{app="foo"} | id="short"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects stream selector without pipeline filters",
			query:       `{app="foo", trace_id="abc123def456"}`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "deduplicates repeated label filter literals",
			query:       `{app="foo"} | trace_id="abc123def456" | request_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "deduplicates same literal from line and label filters",
			query:       `{app="foo"} |= "abc123def456" | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "rejects numeric label filter",
			query:       `{app="foo"} | status_code > 200`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects bytes label filter",
			query:       `{app="foo"} | bytes > 1KB`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports label filter after regexp parser on the capture",
			query:       `{app="foo"} | regexp "(?P<foo>[a-z0-9]+)" | foo="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports label filter after pattern parser on the capture",
			query:       `{app="foo"} | pattern "<_> <foo>" | foo="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports label filter after json expression parser",
			query:       `{app="foo"} | json dashboardUID="dashboardUID" | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`,
			ngramLength: 6,
			expected:    []string{"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports label filter after json expression parser with a renamed field",
			query:       `{app="foo"} | json uid="dashboardUID" | uid="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`,
			ngramLength: 6,
			expected:    []string{"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports label filter after logfmt expression parser",
			query:       `{app="foo"} | logfmt trace_id="trace_id" | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports label filter after logfmt expression parser with a renamed field",
			query:       `{app="foo"} | logfmt tid="trace_id" | tid="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser case-insensitive regex label filter",
			query:       `{app="foo"} | json | dashboardUID=~"(?i)grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`,
			ngramLength: 6,
			expected:    []string{"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports and-ed post-parser label filters",
			query:       `{app="foo"} | json | foo="abcdef", bar="ghijkl"`,
			ngramLength: 6,
			expected:    []string{"abcdef", "ghijkl"},
		},
		{
			name:        "supports post-parser filters from sequential extractors",
			query:       `{app="foo"} | json | foo="abcdef" | logfmt | bar="ghijkl"`,
			ngramLength: 6,
			expected:    []string{"abcdef", "ghijkl"},
		},
		{
			name:        "supports post-parser filter before label_format; ignores filter after",
			query:       `{app="foo"} | json | foo="abc123def456" | label_format x="y" | bar="xyzxyzxyzxyz"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser filter before keep; ignores filter after",
			query:       `{app="foo"} | json | foo="abc123def456" | keep foo | bar="xyzxyzxyzxyz"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser filter before drop; ignores filter after",
			query:       `{app="foo"} | json | foo="abc123def456" | drop level | bar="xyzxyzxyzxyz"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser filter before unpack; ignores filter after",
			query:       `{app="foo"} | json | foo="abc123def456" | unpack | bar="xyzxyzxyzxyz"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser json filter inside a metric query",
			query:       `count_over_time({app="foo"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec" [1m])`,
			ngramLength: 6,
			expected:    []string{"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports pre-parser label filter and post-parser json label filter",
			query:       `{app="foo"} | trace_id="abc123def456" | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`,
			ngramLength: 6,
			expected:    []string{"abc123def456", "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"},
		},
		{
			name:        "supports nested json expression parser then filter on the extracted label",
			query:       `{app="foo"} | json uid="nested.uid" | uid="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser literal regex label filter",
			query:       `{app="foo"} | json | foo=~"abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser json filter before decolorize; decolorize does not rewrite labels",
			query:       `{app="foo"} | json | decolorize | foo="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser json filter before line_format; line_format does not rewrite labels",
			query:       `{app="foo"} | json | line_format "{{.msg}}" | foo="abc123def456"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "rejects label filter after line_format then json",
			query:       `{app="foo"} | line_format "{{.msg}}" | json | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports post-parser filter before a second json after line_format; ignores filter after",
			query:       `{app="foo"} | json | foo="abc123def456" | line_format "{{.msg}}" | json | bar="xyzxyzxyzxyz"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports post-parser filter before label_format ToUpper",
			query:       `{app="foo"} | json | foo="abc123def456" | label_format foo="{{.foo | ToUpper}}"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "keeps line filter when post-parser filters are or-ed",
			query:       `{app="foo"} |= "database timeout" | json | foo="abc123def456" or bar="ghijkl"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "keeps long post-parser filter when a sibling is too short",
			query:       `{app="foo"} | json | foo="abc123def456" | id="short"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "keeps line filter when post-parser value is not a verbatim line literal",
			query:       `{app="foo"} |= "database timeout" | json | path="hello\\worldxx"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "rejects or-ed post-parser label filters",
			query:       `{app="foo"} | json | foo="abcdef" or bar="ghijkl"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects too short post-parser label filter",
			query:       `{app="foo"} | json | id="short"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects numeric post-parser label filter",
			query:       `{app="foo"} | json | status_code > 200`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser value containing a backslash",
			query:       `{app="foo"} | json | path="hello\\worldxx"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser value containing a quote",
			query:       `{app="foo"} | json | msg="hello\"there"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after unpack parser",
			query:       `{app="foo"} | unpack | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects not-equal post-parser label filter",
			query:       `{app="foo"} | json | foo!="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects non-literal post-parser regex label filter",
			query:       `{app="foo"} | json | foo=~"abc.*def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects wildcard post-parser regex label filter",
			query:       `{app="foo"} | json | foo=~".+"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects duration post-parser label filter",
			query:       `{app="foo"} | json | latency > 1s`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects bytes post-parser label filter",
			query:       `{app="foo"} | json | size > 1KB`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser value containing a newline escape",
			query:       `{app="foo"} | json | msg="hello\nworldxx"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after decolorize then json",
			query:       `{app="foo"} | decolorize | json | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after json, decolorize, then a second json",
			query:       `{app="foo"} | json | decolorize | json | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after json, line_format, then a second json",
			query:       `{app="foo"} | json | line_format "{{.msg}}" | json | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after label_format then json (fail closed even when the line is unchanged)",
			query:       `{app="foo"} | label_format x="derivedvalue" | json | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter that can match a label_format invented value after json",
			query:       `{app="foo"} | label_format foo="derivedvaluexx" | json | foo="derivedvaluexx"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser filter on a label_format ToUpper value",
			query:       `{app="foo"} | json | label_format foo="{{.foo | ToUpper}}" | foo="ABC123DEF456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser filter on a label_format composite value",
			query:       `{app="foo"} | json | label_format foo="{{.a}}{{.b}}" | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser filter on a label_format default value",
			query:       `{app="foo"} | json | label_format foo=` + "`{{.foo | default \"fallbackvaluexx\"}}`" + ` | foo="fallbackvaluexx"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser filter on a label_format __timestamp__ value",
			query:       `{app="foo"} | json | label_format ts="{{__timestamp__}}" | ts="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after label_format",
			query:       `{app="foo"} | label_format foo="barrrr" | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects post-parser filter after keep even when the field was already extracted",
			query:       `{app="foo"} | json | keep foo | foo="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects label filter after drop",
			query:       `{app="foo"} | drop level | trace_id="abc123def456"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "supports pre-parser label filter before label_format",
			query:       `{app="foo"} | trace_id="abc123def456" | label_format foo="barrrr"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "rejects line filter after line_format",
			query:       `{app="foo"} | json | line_format "{{.level}} {{.msg}}" |= "info hello"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "keeps line filter before line_format; ignores filter after",
			query:       `{app="foo"} |= "database timeout" | line_format "{{.msg}}" |= "info hello"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports line filter after json parser; json does not rewrite the line",
			query:       `{app="foo"} | json |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports line filter after json before line_format",
			query:       `{app="foo"} | json |= "database timeout" | line_format "{{.msg}}"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "rejects line filter after unpack",
			query:       `{app="foo"} | unpack |= "info hello"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "rejects line filter after decolorize",
			query:       `{app="foo"} | decolorize |= "info hello"`,
			ngramLength: 6,
			expected:    nil,
		},
		{
			name:        "keeps post-parser label filter; ignores line filter after line_format",
			query:       `{app="foo"} | json | foo="abc123def456" | line_format "{{.msg}}" |= "info hello"`,
			ngramLength: 6,
			expected:    []string{"abc123def456"},
		},
		{
			name:        "supports line filter after keep; keep does not rewrite the line",
			query:       `{app="foo"} | keep foo |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
		{
			name:        "supports line filter after label_format; label_format does not rewrite the line",
			query:       `{app="foo"} | label_format x="y" |= "database timeout"`,
			ngramLength: 6,
			expected:    []string{"database timeout"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			got := SupportedQuery(expr, tc.ngramLength)
			if tc.expected == nil {
				require.Nil(t, got)
				return
			}
			require.ElementsMatch(t, tc.expected, got)
		})
	}
}

func TestCollectLineFilters(t *testing.T) {
	t.Run("collects all line filters when the line is unchanged", func(t *testing.T) {
		expr, err := syntax.ParseExpr(`{app="foo"} |= "alpha" |~ "beta" != "gamma" |= "delta"`)
		require.NoError(t, err)

		got := collectLineFilters(expr)
		require.Len(t, got, 4)

		matches := make([]string, len(got))
		for i, f := range got {
			matches[i] = f.Match
		}
		require.ElementsMatch(t, []string{"alpha", "beta", "gamma", "delta"}, matches)
	})

	t.Run("stops at line_format", func(t *testing.T) {
		expr, err := syntax.ParseExpr(`{app="foo"} |= "alpha" | line_format "{{.msg}}" |= "beta"`)
		require.NoError(t, err)

		got := collectLineFilters(expr)
		require.Len(t, got, 1)
		require.Equal(t, "alpha", got[0].Match)
	})

	t.Run("stops at decolorize", func(t *testing.T) {
		expr, err := syntax.ParseExpr(`{app="foo"} |= "alpha" | decolorize |= "beta"`)
		require.NoError(t, err)

		got := collectLineFilters(expr)
		require.Len(t, got, 1)
		require.Equal(t, "alpha", got[0].Match)
	})

	t.Run("stops at unpack", func(t *testing.T) {
		expr, err := syntax.ParseExpr(`{app="foo"} |= "alpha" | unpack |= "beta"`)
		require.NoError(t, err)

		got := collectLineFilters(expr)
		require.Len(t, got, 1)
		require.Equal(t, "alpha", got[0].Match)
	})

	t.Run("keeps filters after json", func(t *testing.T) {
		expr, err := syntax.ParseExpr(`{app="foo"} | json |= "alpha"`)
		require.NoError(t, err)

		got := collectLineFilters(expr)
		require.Len(t, got, 1)
		require.Equal(t, "alpha", got[0].Match)
	})
}

func TestExtractQueryNgrams(t *testing.T) {
	t.Run("returns nil for too-short query", func(t *testing.T) {
		got, err := ExtractQueryNgrams("abc", 6, "v3")
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("returns sorted uppercase unique terms", func(t *testing.T) {
		got, err := ExtractQueryNgrams("abcdefg", 6, "v3")
		require.NoError(t, err)
		require.Equal(t, []string{"ABCDEF", "BCDEFG"}, got)
	})

	t.Run("de-duplicates repeated ngrams", func(t *testing.T) {
		got, err := ExtractQueryNgrams("aaaaaaa", 3, "v3")
		require.NoError(t, err)
		require.Equal(t, []string{"AAA"}, got)
	})
}
