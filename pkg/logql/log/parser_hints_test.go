// uses log_test package to avoid circular dependency between log and logql package.
package log_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	jsonLine = []byte(`{
	"remote_user": "foo",
	"upstream_addr": "10.0.0.1:80",
	"protocol": "HTTP/2.0",
    "cluster": "us-east-west",
	"request": {
		"time": "30.001",
		"method": "POST",
		"host": "foo.grafana.net",
		"uri": "/rpc/v2/stage",
		"size": "101"
	},
	"response": {
		"status": 204,
		"latency_seconds": "30.001"
	},
  "message": {
    "message": "foo",
  }
}`)

	packedLine = []byte(`{
		"remote_user": "foo",
		"upstream_addr": "10.0.0.1:80",
		"protocol": "HTTP/2.0",
		"cluster": "us-east-west",
		"_entry":"foo"
	}`)

	logfmtLine = []byte(`ts=2021-02-02T14:35:05.983992774Z caller=spanlogger.go:79 org_id=3677 traceID=2e5c7234b8640997 Ingester.TotalReached=15 Ingester.TotalChunksMatched=0 Ingester.TotalBatches=0`)
)

func Test_ParserHints(t *testing.T) {
	lbs := labels.FromStrings("app", "nginx", "cluster", "us-central-west")

	t.Parallel()
	for _, tt := range []struct {
		expr      string
		line      []byte
		expectOk  bool
		expectVal float64
		expectLbs string
	}{
		{
			`rate({app="nginx"} | json | response_status = 204 [1m])`,
			jsonLine,
			true,
			1.0,
			"{app=\"nginx\", cluster=\"us-central-west\", cluster_extracted=\"us-east-west\", message_message=\"foo\", protocol=\"HTTP/2.0\", remote_user=\"foo\", request_host=\"foo.grafana.net\", request_method=\"POST\", request_size=\"101\", request_time=\"30.001\", request_uri=\"/rpc/v2/stage\", response_latency_seconds=\"30.001\", response_status=\"204\", upstream_addr=\"10.0.0.1:80\"}",
		},
		{
			`sum without (request_host,app,cluster) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			"{cluster_extracted=\"us-east-west\", message_message=\"foo\", protocol=\"HTTP/2.0\", remote_user=\"foo\", request_method=\"POST\", request_size=\"101\", request_time=\"30.001\", request_uri=\"/rpc/v2/stage\", response_latency_seconds=\"30.001\", response_status=\"204\", upstream_addr=\"10.0.0.1:80\"}",
		},
		{
			`sum by (request_host,app) (rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			"{app=\"nginx\", request_host=\"foo.grafana.net\"}",
		},
		{
			`sum(rate({app="nginx"} | json | __error__="" | response_status = 204 [1m]))`,
			jsonLine,
			true,
			1.0,
			"{}",
		},
		{
			`sum(rate({app="nginx"} | json [1m]))`,
			jsonLine,
			true,
			1.0,
			"{}",
		},
		{
			`sum(rate({app="nginx"} | json | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			"{}",
		},
		{
			`sum(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			"{}",
		},
		{
			`sum by (request_host,app)(rate({app="nginx"} | json | response_status = 204 and  remote_user = "foo" | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{app="nginx", request_host="foo.grafana.net"}`,
		},
		{
			`rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m])`,
			jsonLine,
			true,
			30.001,
			"{app=\"nginx\", cluster=\"us-central-west\", cluster_extracted=\"us-east-west\", message_message=\"foo\", protocol=\"HTTP/2.0\", remote_user=\"foo\", request_host=\"foo.grafana.net\", request_method=\"POST\", request_size=\"101\", request_time=\"30.001\", request_uri=\"/rpc/v2/stage\", response_status=\"204\", upstream_addr=\"10.0.0.1:80\"}",
		},
		{
			`sum without (request_host,app,cluster)(rate({app="nginx"} | json | response_status = 204 | unwrap response_latency_seconds [1m]))`,
			jsonLine,
			true,
			30.001,
			`{cluster_extracted="us-east-west", message_message="foo", protocol="HTTP/2.0", remote_user="foo", request_method="POST", request_size="101", request_time="30.001", request_uri="/rpc/v2/stage", response_status="204", upstream_addr="10.0.0.1:80"}`,
		},
		{
			`sum(rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			"{}",
		},
		{
			`sum by (org_id,app) (rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			"{app=\"nginx\", org_id=\"3677\"}",
		},
		{
			`rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m])`,
			logfmtLine,
			true,
			15.0,
			"{Ingester_TotalBatches=\"0\", Ingester_TotalChunksMatched=\"0\", app=\"nginx\", caller=\"spanlogger.go:79\", cluster=\"us-central-west\", org_id=\"3677\", traceID=\"2e5c7234b8640997\", ts=\"2021-02-02T14:35:05.983992774Z\"}",
		},
		{
			`sum without (org_id,app,cluster)(rate({app="nginx"} | logfmt | org_id=3677 | unwrap Ingester_TotalReached[1m]))`,
			logfmtLine,
			true,
			15.0,
			"{Ingester_TotalBatches=\"0\", Ingester_TotalChunksMatched=\"0\", caller=\"spanlogger.go:79\", traceID=\"2e5c7234b8640997\", ts=\"2021-02-02T14:35:05.983992774Z\"}",
		},
		{
			`sum(rate({app="nginx"} | json | remote_user="foo" [1m]))`,
			jsonLine,
			true,
			1.0,
			"{}",
		},
		{
			`sum(rate({app="nginx"} | json | nonexistant_field="foo" [1m]))`,
			jsonLine,
			false,
			0,
			"",
		},
		{
			`absent_over_time({app="nginx"} | json [1m])`,
			jsonLine,
			true,
			1.0,
			"{}",
		},
		{
			`absent_over_time({app="nginx"} | json | nonexistant_field="foo" [1m])`,
			jsonLine,
			false,
			0,
			"",
		},
		{
			`absent_over_time({app="nginx"} | json | remote_user="foo" [1m])`,
			jsonLine,
			true,
			1.0,
			"{}",
		},
		{
			`sum by (cluster_extracted)(count_over_time({app="nginx"} | json | cluster_extracted="us-east-west" [1m]))`,
			jsonLine,
			true,
			1.0,
			"{cluster_extracted=\"us-east-west\"}",
		},
		{
			`sum by (cluster_extracted)(count_over_time({app="nginx"} | unpack | cluster_extracted="us-east-west" [1m]))`,
			packedLine,
			true,
			1.0,
			`{cluster_extracted="us-east-west"}`,
		},
		{
			`sum by (cluster_extracted)(count_over_time({app="nginx"} | unpack[1m]))`,
			packedLine,
			true,
			1.0,
			`{cluster_extracted="us-east-west"}`,
		},
		{
			`sum(rate({app="nginx"} | unpack | nonexistant_field="foo" [1m]))`,
			packedLine,
			false,
			0,
			"",
		},
		{
			`sum by (message_message,app)(count_over_time({app="nginx"} | json | response_status = 204 and  remote_user = "foo"[1m]))`,
			jsonLine,
			true,
			1.0,
			"{app=\"nginx\", message_message=\"foo\"}",
		},
	} {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()
			expr, err := syntax.ParseSampleExpr(tt.expr)
			require.NoError(t, err)

			ex, err := expr.Extractor()
			require.NoError(t, err)

			sample, ok := ex.ForStream(lbs).Process(0, append([]byte{}, tt.line...), labels.EmptyLabels())
			require.Equal(t, tt.expectOk, ok)

			var lbsResString string
			if sample.Labels != nil {
				lbsResString = sample.Labels.String()
			}
			require.Equal(t, tt.expectVal, sample.Value)
			require.Equal(t, tt.expectLbs, lbsResString)
		})
	}
}

func TestRecordingExtractedLabels(t *testing.T) {
	p := log.NewParserHint([]string{"1", "2", "3"}, nil, false, true, "")
	p.RecordExtracted("1")
	p.RecordExtracted("2")

	require.False(t, p.AllRequiredExtracted())
	require.False(t, p.NoLabels())

	p.RecordExtracted("3")

	require.True(t, p.AllRequiredExtracted())
	require.True(t, p.NoLabels())

	p.Reset()
	require.False(t, p.AllRequiredExtracted())
	require.False(t, p.NoLabels())
}

// stagesFor parses a plain log-selector query and returns its stages list
func stagesFor(t *testing.T, query string) []log.Stage {
	t.Helper()
	expr, err := syntax.ParseLogSelector(query, true)
	require.NoError(t, err)

	p, err := expr.Pipeline()
	require.NoError(t, err)

	ap, ok := p.(log.AnalyzablePipeline)
	if !ok {
		return nil // e.g. a bare matcher, or NewNoopPipeline() for an empty stage list
	}
	return ap.Stages()
}

// TestNewLabelFilterHints_Boundary tests which logql expressions
// NewLabelFilterHints considers "safe" to produce a hint for, and which it
// rejects.
func TestNewLabelFilterHints_Boundary(t *testing.T) {
	t.Run("optimization applied", func(t *testing.T) {
		for _, query := range []string{
			`{app="foo"} | logfmt | foo="bar"`,
			`{app="foo"} |= "hello" | logfmt | foo="bar"`,
			`{app="foo"} | logfmt |= "hello" | foo="bar"`,
			`{app="foo"} | logfmt | decolorize | foo="bar"`,
			`{app="foo"} | logfmt | foo="bar" | baz="qux"`,
			`{app="foo"} | logfmt | foo="bar" and baz="qux" | quux="corge"`,
			`{app="foo"} | unpack | foo="bar"`,
			`{app="foo"} | json | foo="bar"`,
			`{app="foo"} | json | foo="bar" | drop foo`, // foo="bar" can still be optimized b/c it comes before the label modifying stage
			`{app="foo"} | pattern "<foo> bar" | foo="bar"`,
			`{app="foo"} | regexp "(?P<foo>.*)" | foo="bar"`,
			`{app="foo"} | logfmt | foo > 5`,
			`{app="foo"} | logfmt | foo > 5MB`,
			`{app="foo"} | logfmt | foo > 5s`,
			`{app="foo"} | logfmt | foo > 5 or foo < 1`,
			`{app="foo"} | logfmt | ipaddr=ip("1.2.3.4")`,
			`{app="foo"} | logfmt foo="bar_field" | foo="bar"`,
			`{app="foo"} | json foo="a.b" | foo="bar"`,
		} {
			t.Run(query, func(t *testing.T) {
				h := log.NewLabelFilterHints(stagesFor(t, query))
				require.NotEqual(t, log.NoLabelFilterHints(), h, "expected the optimization to be enabled for %q", query)
			})
		}
	})

	t.Run("optimization dropped", func(t *testing.T) {
		for _, query := range []string{
			// no parser or no label filter
			`{app="foo"} | foo="bar"`,
			`{app="foo"} |= "hello" | foo="bar"`,
			`{app="foo"} | logfmt`,

			// label filter positioned before a parser
			`{app="foo"} | foo="bar" | logfmt`,
			`{app="foo"} | foo="" | logfmt | bar="baz"`,

			// label modifying stage before any label filters
			`{app="foo"} | logfmt | label_format foo="bar" | foo="bar"`,
			`{app="foo"} | logfmt | drop foo | foo=""`,
			`{app="foo"} | logfmt | label_format foo="bar"`,
			`{app="foo"} | logfmt | keep bar | foo=""`,
			`{app="foo"} | logfmt | json | foo="bar"`,
			`{app="foo"} | logfmt | line_format "{{.foo}}" | foo="bar"`,
			`{app="foo"} | unpack | label_format foo="bar" | foo="bar"`,

			// multiple parsers
			`{app="foo"} | logfmt | logfmt | foo="bar"`,
			`{app="foo"} | logfmt | regexp "(?P<baz>.*)" | foo="bar"`,
			`{app="foo"} | logfmt | pattern "<baz>" | foo="bar"`,
			`{app="foo"} | logfmt | unpack | foo="bar"`,

			// multi-label filters are not supported
			`{app="foo"} | logfmt | foo > 5 and bar="baz"`,
			`{app="foo"} | logfmt | foo > 5 and bar > 3`,
			`{app="foo"} | logfmt | foo > 5 or bar < 3`,
		} {
			t.Run(query, func(t *testing.T) {
				h := log.NewLabelFilterHints(stagesFor(t, query))
				require.Equal(t, log.NoLabelFilterHints(), h, "expected the optimization to be disabled for %q", query)
			})
		}
	})
}

// TestNewLabelFilterHintsMatchers tests NewLabelFilterHints with a given logql
// query and a set of matchers that should or should not continue parsing the line
// LogQL shapes are narrow on purpose. see above TestNewLabelFilterHints_Boundary
func TestNewLabelFilterHintsMatchers(t *testing.T) {
	type check struct {
		label               string
		lbs                 labels.Labels
		continueParsingLine bool
	}

	for _, tt := range []struct {
		name   string
		query  string
		checks []check
	}{
		{
			name:  "logfmt then a single filter",
			query: `{app="foo"} | logfmt | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
				{"other", labels.FromStrings("other", "anything"), true}, // no matcher for this label
			},
		},
		{
			name:  "a no-impact line filter ahead of the parser",
			query: `{app="foo"} |= "hello" | logfmt | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "a no-impact line filter after the parser",
			query: `{app="foo"} | logfmt |= "hello" | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "decolorize after the parser has no label impact",
			query: `{app="foo"} | logfmt | decolorize | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "two independent single-label filters",
			query: `{app="foo"} | logfmt | foo="bar" | baz="qux"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
				{"baz", labels.FromStrings("baz", "qux"), true},
				{"baz", labels.FromStrings("baz", "nope"), false},
			},
		},
		{
			name:  "a binary filter is tolerated but not collected, later filters still are",
			query: `{app="foo"} | logfmt | foo="bar" and baz="qux" | quux="corge"`,
			checks: []check{
				{"quux", labels.FromStrings("quux", "corge"), true},
				{"quux", labels.FromStrings("quux", "nope"), false},
				// foo/baz were only ever seen inside the (ignored) binary filter, so
				// there's no hint for them individually: always true.
				{"foo", labels.FromStrings("foo", "nope"), true},
				{"baz", labels.FromStrings("baz", "nope"), true},
			},
		},
		{
			name:  "unpack as the sole parser",
			query: `{app="foo"} | unpack | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "json as the sole parser",
			query: `{app="foo"} | json | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "pattern as the sole parser",
			query: `{app="foo"} | pattern "<foo> bar" | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			name:  "regexp as the sole parser",
			query: `{app="foo"} | regexp "(?P<foo>.*)" | foo="bar"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "bar"), true},
				{"foo", labels.FromStrings("foo", "nope"), false},
			},
		},
		{
			// The first mutator itself, a numeric filter, is the hint. A parse
			// failure is tolerated (returns true, tagging __error__ instead of
			// dropping), matching NumericLabelFilter.Process's own semantics.
			name:  "the first mutator is a numeric filter",
			query: `{app="foo"} | logfmt | foo > 5`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10"), true},
				{"foo", labels.FromStrings("foo", "3"), false},
				{"foo", labels.FromStrings("foo", "notanumber"), true},
			},
		},
		{
			name:  "the first mutator is a bytes filter",
			query: `{app="foo"} | logfmt | foo > 5MB`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10MB"), true},
				{"foo", labels.FromStrings("foo", "3MB"), false},
				{"foo", labels.FromStrings("foo", "notabyte"), true},
			},
		},
		{
			name:  "the first mutator is a duration filter",
			query: `{app="foo"} | logfmt | foo > 5s`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10s"), true},
				{"foo", labels.FromStrings("foo", "3s"), false},
				{"foo", labels.FromStrings("foo", "notaduration"), true},
			},
		},
		{
			// Both legs of the binary filter mutate, but they're on the same
			// single label, so the combinator itself is still eligible as the
			// first mutator.
			name:  "the first mutator is a same-label binary filter",
			query: `{app="foo"} | logfmt | foo > 5 or foo < 1`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10"), true}, // > 5
				{"foo", labels.FromStrings("foo", "0"), true},  // < 1
				{"foo", labels.FromStrings("foo", "3"), false}, // neither
			},
		},
		{
			// Only the first mutator (foo > 5) is ever collected: bar="baz" comes
			// after it and is never even examined, so it gets no hint of its own --
			// it still runs for real, just without the early-exit speedup.
			name:  "a filter after the first mutator gets no hint of its own",
			query: `{app="foo"} | logfmt | foo > 5 | bar="baz"`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10"), true},
				{"foo", labels.FromStrings("foo", "3"), false},
				{"bar", labels.FromStrings("bar", "anything"), true},
			},
		},
		{
			// The historically dangerous shape: foo > 5 can set __error__, and a
			// trailing __error__ filter reading a value that stage produces would
			// be unsound to collect. It isn't collected -- only "foo" is.
			name:  "a trailing __error__ filter after the first mutator gets no hint",
			query: `{app="foo"} | logfmt | foo > 5 | __error__!=""`,
			checks: []check{
				{"foo", labels.FromStrings("foo", "10"), true},
				{"foo", labels.FromStrings("foo", "3"), false},
				{"__error__", labels.FromStrings("__error__", "anything"), true},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stages := stagesFor(t, tt.query)
			h := log.NewLabelFilterHints(stages)
			require.NotEqual(t, log.NoLabelFilterHints(), h, "expected the optimization to be enabled for %q", tt.query)

			for _, c := range tt.checks {
				lb := log.NewBaseLabelsBuilder().ForLabels(c.lbs, 0)
				require.Equal(t, c.continueParsingLine, h.ShouldContinueParsingLine(c.label, lb), "label=%s lbs=%s", c.label, c.lbs)
			}
		})
	}
}
