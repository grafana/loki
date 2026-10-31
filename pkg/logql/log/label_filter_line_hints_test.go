package log_test

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
)

func TestLabelFilterHints_ShouldParseLine(t *testing.T) {
	stream := labels.FromStrings("app", "foo")

	for _, tc := range []struct {
		name     string
		query    string
		stream   labels.Labels
		metadata labels.Labels
		line     string
		want     bool
	}{
		// json
		{name: "json: the line has the key and the value", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"tenantId":"abc123"}`, want: true},
		{name: "json: the line does not have the value", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"tenantId":"zzz"}`, want: false},
		{name: "json: the line does not have the key", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"other":"abc123"}`, want: false},
		{name: "json: the value is escaped", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"tenantId":"\u0061bc123"}`, want: true},
		{name: "json: the key is escaped", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"tenant\u0049d":"abc123"}`, want: true},
		{name: "json: an escape that cannot decode into the value", query: `{app="foo"} | json | tenantId="abc123"`, line: `{"tenantId":"abc\n123"}`, want: false},
		{name: "json: a slash is escaped", query: `{app="foo"} | json | path="/api/v1"`, line: `{"path":"\/api\/v1"}`, want: true},
		{name: "json: nested keys", query: `{app="foo"} | json | tenant_id="abc123"`, line: `{"tenant":{"id":"abc123"}}`, want: true},
		{name: "json: the key has a dash", query: `{app="foo"} | json | tenant_id="abc123"`, line: `{"tenant-id":"abc123"}`, want: true},
		{name: "json: the key has spaces around it", query: `{app="foo"} | json | tenantId="abc123"`, line: `{" tenantId ":"abc123"}`, want: true},
		{name: "json: invalid UTF-8 becomes a space", query: `{app="foo"} | json | foo="a b"`, line: "{\"foo\":\"a\xffb\"}", want: true},
		{name: "json: U+FFFD becomes a space", query: `{app="foo"} | json | foo="a b"`, line: `{"foo":"a�b"}`, want: true},
		{name: "json: a part of a value with a space is missing", query: `{app="foo"} | json | foo="a b"`, line: `{"foo":"a-c"}`, want: false},
		{name: "json: a value with a quote only needs the key", query: `{app="foo"} | json | foo="say \"hi\""`, line: `{"foo":"x"}`, want: true},
		{name: "json: a value with a quote still needs the key", query: `{app="foo"} | json | foo="say \"hi\""`, line: `{"bar":"x"}`, want: false},
		{name: "json: a value with a control character only needs the key", query: `{app="foo"} | json | foo="a\tb"`, line: `{"foo":"x"}`, want: true},

		// The label is set before the parser runs.
		{name: "json: the stream has the label", query: `{app="foo"} | json | tenantId="abc123"`, stream: labels.FromStrings("app", "foo", "tenantId", "abc123"), line: `{"tenantId":"zzz"}`, want: true},
		{name: "json: the structured metadata has the label", query: `{app="foo"} | json | tenantId="abc123"`, metadata: labels.FromStrings("tenantId", "abc123"), line: `{"tenantId":"zzz"}`, want: true},
		{name: "json: label_format sets the label", query: `{app="foo"} | label_format tenantId="abc123" | json | tenantId="abc123"`, line: `{}`, want: true},
		{name: "json: drop removes the stream label", query: `{app="foo"} | drop tenantId | json | tenantId="abc123"`, stream: labels.FromStrings("app", "foo", "tenantId", "abc123"), line: `{"tenantId":"zzz"}`, want: false},
		{name: "json: the name has the duplicate suffix", query: `{app="foo"} | json | tenantId_extracted="abc123"`, stream: labels.FromStrings("app", "foo", "tenantId", "zzz"), line: `{"tenantId":"abc123"}`, want: true},
		{name: "json: the name has the duplicate suffix and the value is missing", query: `{app="foo"} | json | tenantId_extracted="abc123"`, stream: labels.FromStrings("app", "foo", "tenantId", "abc123"), line: `{"tenantId":"zzz"}`, want: false},

		// Regular expressions
		{name: "regexp filter: a case-insensitive prefix", query: `{app="foo"} | json | tenantId=~"(?i)abc.*"`, line: `{"tenantId":"ABCDEF"}`, want: true},
		{name: "regexp filter: a case-insensitive prefix is missing", query: `{app="foo"} | json | tenantId=~"(?i)abc.*"`, line: `{"tenantId":"xyz"}`, want: false},
		{name: "regexp filter: a case-insensitive literal", query: `{app="foo"} | json | tenantId=~"(?i)abc"`, line: `{"tenantId":"aBc"}`, want: true},
		{name: "regexp filter: one side of an alternation", query: `{app="foo"} | json | tenantId=~"abc|def"`, line: `{"tenantId":"def"}`, want: true},
		{name: "regexp filter: no side of an alternation", query: `{app="foo"} | json | tenantId=~"abc|def"`, line: `{"tenantId":"ghi"}`, want: false},
		{name: "regexp filter: a character class only needs the key", query: `{app="foo"} | json | tenantId=~"a[bc]d"`, line: `{"tenantId":"zzz"}`, want: true},
		{name: "regexp filter: a character class still needs the key", query: `{app="foo"} | json | tenantId=~"a[bc]d"`, line: `{"other":"abd"}`, want: false},

		// Filters that require nothing of the line
		{name: "a filter that accepts the empty value", query: `{app="foo"} | json | tenantId=~"(abc)?"`, line: `{}`, want: true},
		{name: "an equality with the empty value", query: `{app="foo"} | json | tenantId=""`, line: `{}`, want: true},
		{name: "a negative filter", query: `{app="foo"} | json | tenantId!="abc"`, line: `{}`, want: true},
		{name: "an or filter", query: `{app="foo"} | json | tenantId="abc" or level="error"`, line: `{}`, want: true},
		{name: "a numeric filter", query: `{app="foo"} | json | status > 5`, line: `{}`, want: true},
		{name: "a reserved label", query: `{app="foo"} | json | __error__="JSONParserErr"`, line: `{}`, want: true},
		{name: "a filter after a stage that modifies labels", query: `{app="foo"} | json | label_format x="y" | tenantId="abc"`, line: `{}`, want: true},
		{name: "a filter after a second parser", query: `{app="foo"} | json | logfmt | tenantId="abc"`, line: `{}`, want: true},
		{name: "unpack", query: `{app="foo"} | unpack | tenantId="abc"`, line: `{}`, want: true},
		{name: "a json expression parser", query: `{app="foo"} | json tenantId="a.b" | tenantId="abc"`, line: `{}`, want: true},
		{name: "a logfmt expression parser", query: `{app="foo"} | logfmt tenantId="a" | tenantId="abc"`, line: `a=b`, want: true},

		// and
		{name: "and: one side is missing", query: `{app="foo"} | json | foo="abc" and bar="def"`, line: `{"foo":"abc"}`, want: false},
		{name: "and: both sides", query: `{app="foo"} | json | foo="abc" and bar="def"`, line: `{"foo":"abc","bar":"def"}`, want: true},
		{name: "two filters: the second is missing", query: `{app="foo"} | json | foo="abc" | bar="def"`, line: `{"foo":"abc"}`, want: false},

		// logfmt
		{name: "logfmt: the line has the key and the value", query: `{app="foo"} | logfmt | foo="abc"`, line: `foo=abc`, want: true},
		{name: "logfmt: the line does not have the value", query: `{app="foo"} | logfmt | foo="abc"`, line: `foo=zzz`, want: false},
		{name: "logfmt: the value is escaped", query: `{app="foo"} | logfmt | foo="abc"`, line: `foo="\u0061bc"`, want: true},
		{name: "logfmt: an apostrophe is escaped", query: `{app="foo"} | logfmt | name="o'brien"`, line: `name="o\'brien"`, want: true},
		{name: "logfmt: the stream has the label", query: `{app="foo"} | logfmt | foo="abc"`, stream: labels.FromStrings("app", "foo", "foo", "abc"), line: `foo=zzz`, want: true},

		// regexp and pattern take the value from the line as it is, and the label name from the query.
		{name: "regexp: the line has the value", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\w+)" | tenant="abc123"`, line: `tenant=abc123`, want: true},
		{name: "regexp: the line does not have the value", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\w+)" | tenant="abc123"`, line: `tenant=zzz`, want: false},
		{name: "regexp: an escape is not decoded", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\S+)" | tenant="abc123"`, line: `tenant=\u0061bc123`, want: false},
		{name: "regexp: the key is not needed", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\w+)" | tenant="abc123"`, line: `x=abc123`, want: true},
		{name: "regexp: a value with a quote", query: `{app="foo"} | regexp "msg=(?P<msg>.*)" | msg="say \"hi\""`, line: `msg=say "hi"`, want: true},
		{name: "regexp: a value with a quote is missing", query: `{app="foo"} | regexp "msg=(?P<msg>.*)" | msg="say \"hi\""`, line: `msg=say hi`, want: false},
		{name: "regexp: the stream has the label", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\w+)" | tenant="abc123"`, stream: labels.FromStrings("app", "foo", "tenant", "abc123"), line: `tenant=zzz`, want: true},
		{name: "regexp: the structured metadata has the label", query: `{app="foo"} | regexp "tenant=(?P<tenant>\\w+)" | tenant="abc123"`, metadata: labels.FromStrings("tenant", "abc123"), line: `tenant=zzz`, want: true},
		{name: "pattern: the line has the value", query: `{app="foo"} | pattern "<_> tenant=<tenant>" | tenant="abc123"`, line: `GET tenant=abc123`, want: true},
		{name: "pattern: the line does not have the value", query: `{app="foo"} | pattern "<_> tenant=<tenant>" | tenant="abc123"`, line: `GET tenant=zzz`, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.stream
			if s.IsEmpty() {
				s = stream
			}
			require.Equal(t, tc.want, shouldParseLine(t, tc.query, s, tc.metadata, tc.line))
		})
	}
}

// shouldParseLine runs the stages of the query before the parser on the line, then returns whether
// the hints let the parser parse it.
func shouldParseLine(t *testing.T, query string, stream, metadata labels.Labels, line string) bool {
	t.Helper()

	stages := stagesFor(t, query)
	hints := log.NewLabelFilterHints(stages)
	lb := log.NewBaseLabelsBuilder().ForLabels(stream, stream.Hash())
	lb.Add(log.StructuredMetadataLabel, metadata)

	l := []byte(line)
	for _, s := range stages {
		switch s.(type) {
		case *log.JSONParser, *log.LogfmtParser, *log.RegexpParser, *log.PatternParser, *log.UnpackParser,
			*log.JSONExpressionParser, *log.LogfmtExpressionParser:
			return hints.ShouldParseLine(l, lb)
		}
		var ok bool
		l, ok = s.Process(0, l, lb)
		require.True(t, ok, "a stage before the parser dropped the line")
	}
	require.FailNow(t, "the query has no parser")
	return false
}

// TestLabelFilterHints_SameResults processes random lines with and without the hints, and requires
// the same results.
func TestLabelFilterHints_SameResults(t *testing.T) {
	queries := []string{
		`{app="foo"} | json | tenantId="abc123"`,
		`{app="foo"} | json | tenantId=~"(?i)abc.*"`,
		`{app="foo"} | json | tenantId=~"abc123|zzz"`,
		`{app="foo"} | json | tenantId=~"a[bc]c123"`,
		`{app="foo"} | json | tenant_id="abc123"`,
		`{app="foo"} | json | tenantId_extracted="abc123"`,
		`{app="foo"} | json | foo="abc 123"`,
		`{app="foo"} | json | path="/api/v1"`,
		`{app="foo"} | json | msg="say \"hi\""`,
		`{app="foo"} | json | tenantId="abc123" and foo="abc123"`,
		`{app="foo"} | json | tenantId="abc123" or foo="abc123"`,
		`{app="foo"} | json | tenantId="abc123" | __error__=""`,
		`{app="foo"} | json | tenantId="abc123" | __error__!=""`,
		`{app="foo"} | json | __error__="" | tenantId="abc123"`,
		`{app="foo"} | json | tenantId="abc123" | label_format foo="x" | foo="x"`,
		`{app="foo"} | label_format tenantId="abc123" | json | tenantId="abc123"`,
		`{app="foo"} | drop tenantId | json | tenantId="abc123"`,
		`{app="foo"} | keep app | json | tenantId="abc123"`,
		`{app="foo"} |= "abc" | json | tenantId="abc123"`,
		`{app="foo"} | logfmt | tenantId="abc123"`,
		`{app="foo"} | logfmt | tenantId=~"(?i)abc.*"`,
		`{app="foo"} | logfmt | foo="abc 123"`,
		`{app="foo"} | logfmt | name="o'brien"`,
		`{app="foo"} | logfmt | tenantId_extracted="abc123"`,
		`{app="foo"} | logfmt --strict | tenantId="abc123"`,
		`{app="foo"} | logfmt --keep-empty | tenantId="abc123"`,
		`{app="foo"} | regexp "tenantId=(?P<tenantId>\\S+)" | tenantId="abc123"`,
		`{app="foo"} | regexp "(?P<tenant>abc\\S*)" | tenant=~"(?i)abc.*"`,
		`{app="foo"} | pattern "<_>tenantId=<tenantId> <_>" | tenantId="abc123"`,
		`{app="foo"} | pattern "<_>foo=<foo>" | foo="abc 123"`,
	}
	streams := []labels.Labels{
		labels.FromStrings("app", "foo"),
		labels.FromStrings("app", "foo", "tenantId", "abc123"),
		labels.FromStrings("app", "foo", "tenantId", "zzz"),
		labels.FromStrings("app", "foo", "tenant", "abc123"),
	}
	metadata := []labels.Labels{
		labels.EmptyLabels(),
		labels.FromStrings("tenantId", "abc123"),
		labels.FromStrings("tenant", "abc123"),
		labels.FromStrings("foo", "abc 123"),
	}

	rnd := rand.New(rand.NewSource(1))
	lines := make([]string, 0, 1000)
	for range 500 {
		lines = append(lines, randomJSONLine(rnd), randomLogfmtLine(rnd))
	}

	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			for _, s := range streams {
				hinted := pipelineFor(t, query).ForStream(s)
				plain := log.NewStreamPipeline(stagesFor(t, query), log.NewBaseLabelsBuilder().ForLabels(s, s.Hash()))
				for _, m := range metadata {
					for _, line := range lines {
						wantLine, wantLabels, wantOK := plain.Process(0, []byte(line), m)
						gotLine, gotLabels, gotOK := hinted.Process(0, []byte(line), m)
						msg := fmt.Sprintf("stream=%s metadata=%s line=%q", s, m, line)
						require.Equal(t, wantOK, gotOK, msg)
						if !wantOK {
							continue
						}
						require.Equal(t, string(wantLine), string(gotLine), msg)
						require.Equal(t, wantLabels.String(), gotLabels.String(), msg)
					}
				}
			}
		})
	}
}

func pipelineFor(t testing.TB, query string) log.Pipeline {
	t.Helper()
	stages := stagesFor(t, query)
	require.NotEmpty(t, stages)
	return log.NewPipeline(stages)
}

func BenchmarkLabelFilterHints_ShouldParseLine(b *testing.B) {
	const (
		tenant = "6f1c2a9e-3b7d-4e58-9a0f-2d4c8b1e7f35"
		other  = "0b8e5d3c-9a14-4f27-b6e1-7c2d9f0a4e18"
	)

	// The lines are about 5 KB, and tenantId is the last field. extra is the value of one field.
	jsonLine := func(tenantID, extra string) []byte {
		var b strings.Builder
		b.WriteByte('{')
		for i := range 100 {
			fmt.Fprintf(&b, `"field_%02d":"some text in the value of field %02d",`, i, i)
		}
		fmt.Fprintf(&b, `"extra":"%s","tenantId":"%s"}`, extra, tenantID)
		return []byte(b.String())
	}
	logfmtLine := func(tenantID, extra string) []byte {
		var b strings.Builder
		for i := range 100 {
			fmt.Fprintf(&b, `field_%02d="some text in the value of field %02d" `, i, i)
		}
		fmt.Fprintf(&b, `extra="%s" tenantId=%s`, extra, tenantID)
		return []byte(b.String())
	}

	type lineCase struct {
		name string
		line []byte
	}
	jsonLines := []lineCase{
		{"absent", jsonLine(other, "x")},
		{"present", jsonLine(tenant, "x")},
		{"absent-with-escape", jsonLine(other, `caf\u00e9`)},
	}
	logfmtLines := []lineCase{
		{"absent", logfmtLine(other, "x")},
		{"present", logfmtLine(tenant, "x")},
		{"absent-with-escape", logfmtLine(other, `caf\u00e9`)},
	}

	stream := labels.FromStrings("app", "foo")
	for _, q := range []struct {
		name  string
		query string
		lines []lineCase
	}{
		{"json/equal", `{app="foo"} | json | tenantId="` + tenant + `"`, jsonLines},
		{"json/regexp", `{app="foo"} | json | tenantId=~"(?i)6F1C2A9E.*"`, jsonLines},
		{"logfmt/equal", `{app="foo"} | logfmt | tenantId="` + tenant + `"`, logfmtLines},
		{"logfmt/regexp", `{app="foo"} | logfmt | tenantId=~"(?i)6F1C2A9E.*"`, logfmtLines},
		{"regexp/equal", `{app="foo"} | regexp "tenantId=(?P<tenantId>\\S+)" | tenantId="` + tenant + `"`, logfmtLines[:2]},
		{"pattern/equal", `{app="foo"} | pattern "<_>tenantId=<tenantId>" | tenantId="` + tenant + `"`, logfmtLines[:2]},
	} {
		for _, l := range q.lines {
			b.Run(q.name+"/"+l.name, func(b *testing.B) {
				p := pipelineFor(b, q.query).ForStream(stream)
				b.ReportAllocs()
				for b.Loop() {
					p.Process(0, l.line, labels.EmptyLabels())
				}
			})
		}
	}
}

var (
	randomKeys = []string{`tenantId`, `tenant\u0049d`, ` tenantId `, `tenant_id`, `tenant-id`, `foo`, `path`, `msg`, `name`, `x`}

	// randomValues hold escapes that json and logfmt decode, invalid UTF-8 and U+FFFD, which they
	// replace with a space, and values that only contain the strings a filter needs.
	randomValues = []string{
		`abc123`, `\u0061bc123`, `ABC123`, `abc12`, `zzz`, `abc 123`, "abc\xff123", `abc\ufffd123`, "abc\uFFFD123",
		`abc\n123`, `/api/v1`, `\/api\/v1`, `o'brien`, `o\'brien`, `say \"hi\"`, ``, `abc123 extra`, `tenantId=abc123`,
	}
)

func randomJSONLine(rnd *rand.Rand) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := range 1 + rnd.Intn(4) {
		if i > 0 {
			b.WriteByte(',')
		}
		key, value := randomKeys[rnd.Intn(len(randomKeys))], randomValues[rnd.Intn(len(randomValues))]
		if rnd.Intn(5) == 0 {
			fmt.Fprintf(&b, `"tenant":{"id":"%s"}`, value)
			continue
		}
		fmt.Fprintf(&b, `"%s":"%s"`, key, value)
	}
	b.WriteByte('}')

	line := b.String()
	if rnd.Intn(10) == 0 {
		line = line[:rnd.Intn(len(line))] // malformed
	}
	return line
}

func randomLogfmtLine(rnd *rand.Rand) string {
	var b strings.Builder
	for i := range 1 + rnd.Intn(4) {
		if i > 0 {
			b.WriteByte(' ')
		}
		key, value := randomKeys[rnd.Intn(len(randomKeys))], randomValues[rnd.Intn(len(randomValues))]
		key = strings.TrimSpace(key)
		if rnd.Intn(2) == 0 || strings.ContainsAny(value, ` "\`) {
			fmt.Fprintf(&b, `%s="%s"`, key, value)
			continue
		}
		fmt.Fprintf(&b, `%s=%s`, key, value)
	}
	return b.String()
}
