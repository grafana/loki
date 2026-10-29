package log

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/logqlmodel"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_jsonParser_Parse(t *testing.T) {
	tests := []struct {
		name               string
		line               []byte
		lbs                labels.Labels
		want               labels.Labels
		wantJSONPath       map[string][]string
		hints              ParserHint
		structuredMetadata map[string]string
	}{
		{
			"multi depth",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
				"pod_deployment_ref", "foobar",
			),
			map[string][]string{
				"app":                {"app"},
				"namespace":          {"namespace"},
				"pod_uuid":           {"pod", "uuid"},
				"pod_deployment_ref": {"pod", "deployment", "ref"},
			},
			NoParserHints(),
			nil,
		},
		{
			"multi depth with duplicates",
			[]byte(`{"app":"bar","namespace":"prod","pod":{"uuid":"bar","deployment":{"ref":"foobar"}}}`),
			labels.FromStrings("app", "foo",
				"pod_uuid", "foo",
			),
			labels.FromStrings("app", "foo",
				"app_extracted", "bar",
				"namespace", "prod",
				"pod_uuid", "foo",
				"pod_uuid_extracted", "bar",
				"pod_deployment_ref", "foobar",
			),
			map[string][]string{
				"app_extracted":      {"app"},
				"namespace":          {"namespace"},
				"pod_uuid_extracted": {"pod", "uuid"},
				"pod_deployment_ref": {"pod", "deployment", "ref"},
			},
			NoParserHints(),
			nil,
		},
		{
			"numeric",
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
			),
			map[string][]string{
				"counter":     {"counter"},
				"price__net_": {"price", "_net_"},
			},
			NoParserHints(),
			nil,
		},
		{
			"whitespace key value",
			[]byte(`{" ": {"foo":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("foo", "bar"),
			map[string][]string{
				"foo": {" ", "foo"},
			},
			NoParserHints(),
			nil,
		},
		{
			"whitespace key and whitespace subkey values",
			[]byte(`{" ": {" ":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			map[string][]string{
				"": {" ", " "},
			},
			NoParserHints(),
			nil,
		},
		{
			"whitespace key and empty subkey values",
			[]byte(`{" ": {"":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			map[string][]string{
				"": {" ", ""},
			},
			NoParserHints(),
			nil,
		},
		{
			"empty key and empty subkey values",
			[]byte(`{"": {"":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			map[string][]string{
				"": {"", ""},
			},
			NoParserHints(),
			nil,
		},
		{
			"escaped",
			[]byte(`{"counter":1,"foo":"foo\\\"bar", "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
				"foo", `foo\"bar`,
			),
			map[string][]string{
				"counter":     {"counter"},
				"price__net_": {"price", "_net_"},
				"foo":         {"foo"},
			},
			NoParserHints(),
			nil,
		},
		{
			"utf8 error rune",
			[]byte(`{"counter":1,"foo":"�", "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
				"foo", " ",
			),
			map[string][]string{
				"counter":     {"counter"},
				"price__net_": {"price", "_net_"},
				"foo":         {"foo"},
			},
			NoParserHints(),
			nil,
		},
		{
			"skip arrays",
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1"),
			map[string][]string{
				"counter": {"counter"},
			},
			NoParserHints(),
			nil,
		},
		{
			"bad key replaced",
			[]byte(`{"cou-nter":1}`),
			labels.EmptyLabels(),
			labels.FromStrings("cou_nter", "1"),
			map[string][]string{
				"cou_nter": {"cou-nter"},
			},
			NoParserHints(),
			nil,
		},
		{
			"nested bad key replaced",
			[]byte(`{"foo":{"cou-nter":1}}"`),
			labels.EmptyLabels(),
			labels.FromStrings("foo_cou_nter", "1"),
			map[string][]string{
				"foo_cou_nter": {"foo", "cou-nter"},
			},
			NoParserHints(),
			nil,
		},
		{
			"errors",
			[]byte(`{n}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "Value looks like object, but can't find closing '}' symbol",
			),
			map[string][]string{},
			NoParserHints(),
			nil,
		},
		{
			"errors hints",
			[]byte(`{n}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "Value looks like object, but can't find closing '}' symbol",
			),
			map[string][]string{},
			parserHintsRequiring("__error__"),
			nil,
		},
		{
			"duplicate extraction",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}},"next":{"err":false}}`),
			labels.FromStrings("app", "bar"),
			labels.FromStrings("app", "bar",
				"app_extracted", "foo",
				"namespace", "prod",
				"pod_uuid", "foo",
				"next_err", "false",
				"pod_deployment_ref", "foobar",
			),
			map[string][]string{
				"app_extracted":      {"app"},
				"namespace":          {"namespace"},
				"pod_uuid":           {"pod", "uuid"},
				"next_err":           {"next", "err"},
				"pod_deployment_ref": {"pod", "deployment", "ref"},
			},
			NoParserHints(),
			nil,
		},
		{
			"duplicate conflict with structured metadata",
			[]byte(`{"app":"foo","namespace":"prod","pod":"pod_parsed"}`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"app", "foo",
				"namespace", "prod",
				"pod", "pod_sm",
				"pod_extracted", "pod_parsed",
			),
			map[string][]string{
				"app":           {"app"},
				"namespace":     {"namespace"},
				"pod_extracted": {"pod"},
			},
			NoParserHints(),
			map[string]string{"pod": "pod_sm"},
		},
	}
	for _, tt := range tests {
		j := NewJSONParser(true)
		t.Run(tt.name, func(t *testing.T) {
			origLine := string(tt.line)

			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()

			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}

			_, _ = j.Process(0, tt.line, b)
			labels := b.LabelsResult().Labels()
			require.Equal(t, origLine, string(tt.line), "original log line was modified")
			require.Equal(t, tt.want, labels)

			// Check JSON paths if provided
			if len(tt.wantJSONPath) > 0 {
				for k, parts := range tt.wantJSONPath {
					require.Equal(t, parts, b.GetJSONPath(k), "incorrect json path parts for key %s", k)
				}
			}
		})
	}
}

func TestKeyShortCircuit(t *testing.T) {
	jsonLine := []byte(`{"invalid":"a\\xc5z","proxy_protocol_addr": "","remote_addr": "3.112.221.14","remote_user": "","upstream_addr": "10.12.15.234:5000","the_real_ip": "3.112.221.14","timestamp": "2020-12-11T16:20:07+00:00","protocol": "HTTP/1.1","upstream_name": "hosted-grafana-hosted-grafana-api-80","request": {"id": "c8eacb6053552c0cd1ae443bc660e140","time": "0.001","method" : "GET","host": "hg-api-qa-us-central1.grafana.net","uri": "/","size" : "128","user_agent": "worldping-api-","referer": ""},"response": {"status": 200,"upstream_status": "200","size": "1155","size_sent": "265","latency_seconds": "0.001"}}`)
	logfmtLine := []byte(`level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 latency=fast query="sum by (object_name) (rate(({container=\"metrictank\", cluster=\"hm-us-east2\"} |= \"PANIC\")[5m]))" query_type=metric range_type=range length=5m0s step=15s duration=322.623724ms status=200 throughput=1.2GB total_bytes=375MB`)
	nginxline := []byte(`10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1" 200 60755 "https://grafana.com/go/observabilitycon/grafana-the-open-and-composable-observability-platform/?tech=ggl-o&pg=oss-graf&plcmt=hero-txt" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.1 Safari/605.1.15" "123.123.123.123, 35.35.122.223" "TLSv1.3"`)
	packedLike := []byte(`{"job":"123","pod":"someuid123","app":"foo","_entry":"10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] GET /static/img/about/bob.jpg HTTP/1.1"}`)

	lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	hints := newFakeParserHints()
	hints.extractAll = true
	hints.keepGoing = false

	lbs.parserKeyHints = hints
	lbs.labelFilterHints = hints

	for _, tt := range []struct {
		name                 string
		line                 []byte
		p                    Stage
		LabelFilterParseHint *labels.Matcher
	}{
		{"json", jsonLine, NewJSONParser(false), labels.MustNewMatcher(labels.MatchEqual, "response_latency_seconds", "nope")},
		{"unpack", packedLike, NewUnpackParser(), labels.MustNewMatcher(labels.MatchEqual, "pod", "nope")},
		{"logfmt", logfmtLine, NewLogfmtParser(false, false), labels.MustNewMatcher(labels.MatchEqual, "info", "nope")},
		{"regex greedy", nginxline, mustStage(NewRegexpParser(`GET (?P<path>.*?)/\?`)), labels.MustNewMatcher(labels.MatchEqual, "path", "nope")},
		{"pattern", nginxline, mustStage(NewPatternParser(`<_> "<method> <path> <_>"<_>`)), labels.MustNewMatcher(labels.MatchEqual, "method", "nope")},
	} {
		lbs.Reset()
		t.Run(tt.name, func(t *testing.T) {
			_, result = tt.p.Process(0, tt.line, lbs)

			require.Equal(t, 1, lbs.LabelsResult().Labels().Len())
			require.False(t, result)
		})
	}
}

func TestLabelShortCircuit(t *testing.T) {
	simpleJsn := []byte(`{
      "data": "Click Here",
      "size": 36,
      "style": "bold",
      "name": "text1",
      "name": "duplicate",
      "hOffset": 250,
      "vOffset": 100,
      "alignment": "center",
      "onMouseUp": "sun1.opacity = (sun1.opacity / 100) * 90;"
    }`)
	logFmt := []byte(`data="ClickHere" size=36 style=bold name=text1 name=duplicate hOffset=250 vOffset=100 alignment=center onMouseUp="sun1.opacity = (sun1.opacity / 100) * 90;"`)

	hints := newFakeParserHints()
	hints.label = "name"

	lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	lbs.parserKeyHints = hints
	lbs.labelFilterHints = hints

	tests := []struct {
		name string
		p    Stage
		line []byte
	}{
		{"json", NewJSONParser(false), simpleJsn},
		{"logfmt", NewLogfmtParser(false, false), logFmt},
		{"logfmt-expression", mustStage(NewLogfmtExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("name", "name")}, false)), logFmt},
	}
	for _, tt := range tests {
		lbs.Reset()
		t.Run(tt.name, func(t *testing.T) {
			_, result = tt.p.Process(0, tt.line, lbs)

			require.Equal(t, 1, lbs.LabelsResult().Labels().Len())
			name, category, ok := lbs.GetWithCategory("name")
			require.True(t, ok)
			require.Equal(t, ParsedLabel, category)
			require.Contains(t, name, "text1")
		})
	}
}

func newFakeParserHints() *fakeParseHints {
	return &fakeParseHints{
		keepGoing: true,
	}
}

type fakeParseHints struct {
	label      string
	checkCount int
	count      int
	keepGoing  bool
	extractAll bool
	extracted  map[string]struct{}
}

func (p *fakeParseHints) Extracted(key string) bool {
	_, ok := p.extracted[key]
	return ok
}

func (p *fakeParseHints) ShouldExtract(key string) bool {
	p.checkCount++
	return key == p.label || p.extractAll
}

func (p *fakeParseHints) ShouldExtractPrefix(prefix string) bool {
	return prefix == p.label || p.extractAll
}

func (p *fakeParseHints) NoLabels() bool {
	return false
}

func (p *fakeParseHints) RecordExtracted(key string) {
	p.count++
	if p.extracted == nil {
		p.extracted = map[string]struct{}{}
	}
	p.extracted[key] = struct{}{}
}

func (p *fakeParseHints) AllRequiredExtracted() bool {
	return !p.extractAll && p.count == 1
}

func (p *fakeParseHints) Reset() {
	p.checkCount = 0
	p.count = 0
	clear(p.extracted)
}

func (p *fakeParseHints) ShouldContinueParsingLine(_ string, _ *LabelsBuilder) bool {
	return p.keepGoing
}

func TestJSONExpressionParser(t *testing.T) {
	testLine := []byte(`{"app":"foo","field with space":"value","field with ÜFT8👌":"value","null_field":null,"bool_field":false,"namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar", "params": [1,2,3,"string_value"]}}}`)

	tests := []struct {
		name               string
		line               []byte
		expressions        []LabelExtractionExpr
		lbs                labels.Labels
		want               labels.Labels
		hints              ParserHint
		structuredMetadata map[string]string
	}{
		{
			"single field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo"),
			NoParserHints(),
			nil,
		},
		{
			"alternate syntax",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("test", `["field with space"]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("test", "value"),
			NoParserHints(),
			nil,
		},
		{
			"multiple fields",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
				NewLabelExtractionExpr("namespace", "namespace"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
			),
			NoParserHints(),
			nil,
		},
		{
			"utf8",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("utf8", `["field with ÜFT8👌"]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("utf8", "value"),
			NoParserHints(),
			nil,
		},
		{
			"nested field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", "pod.uuid"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("uuid", "foo"),
			NoParserHints(),
			nil,
		},
		{
			"nested field alternate syntax",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `pod["uuid"]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("uuid", "foo"),
			NoParserHints(),
			nil,
		},
		{
			"nested field alternate syntax 2",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `["pod"]["uuid"]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("uuid", "foo"),
			NoParserHints(),
			nil,
		},
		{
			"nested field alternate syntax 3",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `["pod"].uuid`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("uuid", "foo"),
			NoParserHints(),
			nil,
		},
		{
			"array element",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("param", `pod.deployment.params[0]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("param", "1"),
			NoParserHints(),
			nil,
		},
		{
			"object element not present",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("undefined", `pod[""]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("undefined", ""),
			NoParserHints(),
			nil,
		},
		{
			"accessing invalid array index",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("param", `pod.deployment.params[""]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("param", ""),
			NoParserHints(),
			nil,
		},
		{
			"array string element",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("param", `pod.deployment.params[3]`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("param", "string_value"),
			NoParserHints(),
			nil,
		},
		{
			"full array",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("params", `pod.deployment.params`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("params", `[1,2,3,"string_value"]`),
			NoParserHints(),
			nil,
		},
		{
			"full object",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("deployment", `pod.deployment`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("deployment", `{"ref":"foobar", "params": [1,2,3,"string_value"]}`),
			NoParserHints(),
			nil,
		},
		{
			"expression matching nothing",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("nope", `pod.nope`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("nope", ""),
			NoParserHints(),
			nil,
		},
		{
			"null field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("nf", `null_field`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("nf", ""), // null is coerced to an empty string

			NoParserHints(),
			nil,
		},
		{
			"boolean field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("bool", `bool_field`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("bool", `false`),
			NoParserHints(),
			nil,
		},
		{
			"label override",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `pod.uuid`),
			},
			labels.FromStrings("uuid", "bar"),
			labels.FromStrings("uuid", "bar",
				"uuid_extracted", "foo",
			),
			NoParserHints(),
			nil,
		},
		{
			"non-matching expression",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("request_size", `request.size.invalid`),
			},
			labels.FromStrings("uuid", "bar"),
			labels.FromStrings("uuid", "bar",
				"request_size", "",
			),
			NoParserHints(),
			nil,
		},
		{
			"empty line",
			[]byte("{}"),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `pod.uuid`),
			},
			labels.EmptyLabels(),
			labels.FromStrings("uuid", ""),
			NoParserHints(),
			nil,
		},
		{
			"existing labels are not affected",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `will.not.work`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"uuid", "",
			),
			NoParserHints(),
			nil,
		},
		{
			"invalid JSON line",
			[]byte(`invalid json`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `will.not.work`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				logqlmodel.ErrorLabel, errJSON,
			),
			NoParserHints(),
			nil,
		},
		{
			"invalid JSON line with hints",
			[]byte(`invalid json`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `will.not.work`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				logqlmodel.ErrorLabel, errJSON,
			),
			parserHintsRequiring("__error__"),
			nil,
		},
		{
			"empty line",
			[]byte(``),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("uuid", `will.not.work`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			NoParserHints(),
			nil,
		},
		{
			"nested escaped object",
			[]byte(`{"app":"{ \"key\": \"value\", \"key2\":\"value2\"}"}`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", `app`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"app", `{ "key": "value", "key2":"value2"}`,
			),
			NoParserHints(),
			nil,
		},
		{
			"nested object with escaped value",
			[]byte(`{"app":{"name":"great \"loki\""}`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", `app`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"app", `{"name":"great \"loki\""}`,
			),
			NoParserHints(),
			nil,
		},
		{
			"field with escaped value inside the json string",
			[]byte(`{"app":"{\"name\":\"great \\\"loki\\\"\"}"}`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", `app`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"app", `{"name":"great \"loki\""}`,
			),
			NoParserHints(),
			nil,
		},
		{
			"duplicate field name takes first value",
			[]byte(`{"app":"foo","app":"duplicate"}`),
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", `app`),
			},
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"app", "foo",
			),
			NoParserHints(),
			nil,
		},
		{
			"duplicate conflict with structured metadata",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "app_sm",
				"app_extracted", "foo",
			),
			NoParserHints(),
			map[string]string{"app": "app_sm"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := NewJSONExpressionParser(tt.expressions)
			require.NoError(t, err, "cannot create JSON expression parser")
			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()
			// Set structured metadata if specified
			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}
			_, _ = j.Process(0, tt.line, b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}
}

func TestJSONExpressionParserFailures(t *testing.T) {
	tests := []struct {
		name       string
		expression LabelExtractionExpr
		error      string
	}{
		{
			"invalid field name",
			NewLabelExtractionExpr("app", `field with space`),
			"unexpected FIELD",
		},
		{
			"missing opening square bracket",
			NewLabelExtractionExpr("app", `"pod"]`),
			"unexpected STRING, expecting LSB or FIELD",
		},
		{
			"missing closing square bracket",
			NewLabelExtractionExpr("app", `["pod"`),
			"unexpected $end, expecting RSB",
		},
		{
			"missing closing square bracket",
			NewLabelExtractionExpr("app", `["pod""uuid"]`),
			"unexpected STRING, expecting RSB",
		},
		{
			"invalid nesting",
			NewLabelExtractionExpr("app", `pod..uuid`),
			"unexpected DOT, expecting FIELD",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJSONExpressionParser([]LabelExtractionExpr{tt.expression})

			require.NotNil(t, err)
			require.Equal(t, err.Error(), fmt.Sprintf("cannot parse expression [%s]: syntax error: %s", tt.expression.Expression, tt.error))
		})
	}
}

func Benchmark_Parser(b *testing.B) {
	lbs := labels.FromStrings("cluster", "qa-us-central1",
		"namespace", "qa",
		"filename", "/var/log/pods/ingress-nginx_nginx-ingress-controller-7745855568-blq6t_1f8962ef-f858-4188-a573-ba276a3cacc3/ingress-nginx/0.log",
		"job", "ingress-nginx/nginx-ingress-controller",
		"name", "nginx-ingress-controller",
		"pod", "nginx-ingress-controller-7745855568-blq6t",
		"pod_template_hash", "7745855568",
		"stream", "stdout",
	)

	jsonLine := `{"invalid":"a\\xc5z","proxy_protocol_addr": "","remote_addr": "3.112.221.14","remote_user": "","upstream_addr": "10.12.15.234:5000","the_real_ip": "3.112.221.14","timestamp": "2020-12-11T16:20:07+00:00","protocol": "HTTP/1.1","upstream_name": "hosted-grafana-hosted-grafana-api-80","request": {"id": "c8eacb6053552c0cd1ae443bc660e140","time": "0.001","method" : "GET","host": "hg-api-qa-us-central1.grafana.net","uri": "/","size" : "128","user_agent": "worldping-api-","referer": ""},"response": {"status": 200,"upstream_status": "200","size": "1155","size_sent": "265","latency_seconds": "0.001"}}`
	logfmtLine := `level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 latency=fast query="sum by (object_name) (rate(({container=\"metrictank\", cluster=\"hm-us-east2\"} |= \"PANIC\")[5m]))" query_type=metric range_type=range length=5m0s step=15s duration=322.623724ms status=200 throughput=1.2GB total_bytes=375MB`
	nginxline := `10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1" 200 60755 "https://grafana.com/go/observabilitycon/grafana-the-open-and-composable-observability-platform/?tech=ggl-o&pg=oss-graf&plcmt=hero-txt" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.1 Safari/605.1.15" "123.123.123.123, 35.35.122.223" "TLSv1.3"`
	packedLike := `{"job":"123","pod":"someuid123","app":"foo","_entry":"10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1"}`

	for _, tt := range []struct {
		name                 string
		line                 string
		s                    Stage
		LabelParseHints      []string //  hints to reduce label extractions.
		LabelFilterParseHint *labels.Matcher
	}{
		{"json", jsonLine, NewJSONParser(false), []string{"response_latency_seconds"}, labels.MustNewMatcher(labels.MatchEqual, "the_real_ip", "nope")},
		{"jsonParser-not json line", nginxline, NewJSONParser(false), []string{"response_latency_seconds"}, labels.MustNewMatcher(labels.MatchEqual, "the_real_ip", "nope")},
		{"unpack", packedLike, NewUnpackParser(), []string{"pod"}, labels.MustNewMatcher(labels.MatchEqual, "app", "nope")},
		{"unpack-not json line", nginxline, NewUnpackParser(), []string{"pod"}, labels.MustNewMatcher(labels.MatchEqual, "app", "nope")},
		{"logfmt", logfmtLine, NewLogfmtParser(false, false), []string{"info", "throughput", "org_id"}, labels.MustNewMatcher(labels.MatchEqual, "latency", "nope")},
		{"regex greedy", nginxline, mustStage(NewRegexpParser(`GET (?P<path>.*?)/\?`)), []string{"path"}, labels.MustNewMatcher(labels.MatchEqual, "path", "nope")},
		{"regex status digits", nginxline, mustStage(NewRegexpParser(`HTTP/1.1" (?P<statuscode>\d{3}) `)), []string{"statuscode"}, labels.MustNewMatcher(labels.MatchEqual, "status_code", "nope")},
		{"pattern", nginxline, mustStage(NewPatternParser(`<_> "<method> <path> <_>"<_>`)), []string{"path"}, labels.MustNewMatcher(labels.MatchEqual, "method", "nope")},
	} {
		b.Run(tt.name, func(b *testing.B) {
			line := []byte(tt.line)
			b.Run("no labels hints", func(b *testing.B) {
				b.ReportAllocs()
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
			})

			b.Run("labels hints", func(b *testing.B) {
				b.ReportAllocs()
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				builder.parserKeyHints = NewParserHint(nil, tt.LabelParseHints, false, false, "")

				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
			})

			b.Run("inline stages", func(b *testing.B) {
				b.ReportAllocs()
				stages := []Stage{NewStringLabelFilter(tt.LabelFilterParseHint)}
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				builder.parserKeyHints = NewParserHint(nil, nil, false, false, ", nil")
				builder.labelFilterHints = NewLabelFilterHints(stages)
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
			})
		})
	}
}

func Benchmark_Parser_JSONPath(b *testing.B) {
	lbs := labels.FromStrings("cluster", "qa-us-central1",
		"namespace", "qa",
		"filename", "/var/log/pods/ingress-nginx_nginx-ingress-controller-7745855568-blq6t_1f8962ef-f858-4188-a573-ba276a3cacc3/ingress-nginx/0.log",
		"job", "ingress-nginx/nginx-ingress-controller",
		"name", "nginx-ingress-controller",
		"pod", "nginx-ingress-controller-7745855568-blq6t",
		"pod_template_hash", "7745855568",
		"stream", "stdout",
	)

	jsonLine := `{
    "invalid": "a\\xc5z",
    "proxy_protocol_addr": "",
    "remote_addr": "3.112.221.14",
    "remote_user": "",
    "upstream_addr": "10.12.15.234:5000",
    "the_real_ip": "3.112.221.14",
    "timestamp": "2020-12-11T16:20:07+00:00",
    "protocol": "HTTP/1.1",
    "upstream_name": "hosted-grafana-hosted-grafana-api-80",
    "request": {
      "id": "c8eacb6053552c0cd1ae443bc660e140",
      "time": "0.001",
      "method": "GET",
      "host": "hg-api-qa-us-central1.grafana.net",
      "uri": "/",
      "size" : "128",
      "user_agent":"worldping-api-",
      "referer": ""
    },
    "response": {
      "status": 200,
      "upstream_status": "200",
      "size": "1155",
      "size_sent": "265",
      "latency_seconds": "0.001"
    }
  }`
	for _, tt := range []struct {
		name                 string
		line                 string
		s                    Stage
		LabelParseHints      []string //  hints to reduce label extractions.
		LabelFilterParseHint *labels.Matcher
	}{
		{"json", jsonLine, NewJSONParser(true), []string{"response_latency_seconds"}, labels.MustNewMatcher(labels.MatchEqual, "the_real_ip", "nope")},
	} {
		b.Run(tt.name, func(b *testing.B) {
			line := []byte(tt.line)
			b.Run("no labels hints", func(b *testing.B) {
				b.ReportAllocs()
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
				expectedJSONPath := map[string][]string{
					"invalid":                  {"invalid"},
					"proxy_protocol_addr":      {"proxy_protocol_addr"},
					"remote_addr":              {"remote_addr"},
					"remote_user":              {"remote_user"},
					"upstream_addr":            {"upstream_addr"},
					"the_real_ip":              {"the_real_ip"},
					"timestamp":                {"timestamp"},
					"protocol":                 {"protocol"},
					"upstream_name":            {"upstream_name"},
					"request_id":               {"request", "id"},
					"request_time":             {"request", "time"},
					"request_method":           {"request", "method"},
					"request_host":             {"request", "host"},
					"request_uri":              {"request", "uri"},
					"request_size":             {"request", "size"},
					"request_user_agent":       {"request", "user_agent"},
					"request_referer":          {"request", "referer"},
					"response_status":          {"response", "status"},
					"response_upstream_status": {"response", "upstream_status"},
					"response_size":            {"response", "size"},
					"response_size_sent":       {"response", "size_sent"},
					"response_latency_seconds": {"response", "latency_seconds"},
				}

				for k, parts := range expectedJSONPath {
					require.Equal(b, parts, builder.GetJSONPath(k), "incorrect json path parts for key %s", k)
				}
			})

			b.Run("labels hints", func(b *testing.B) {
				b.ReportAllocs()
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				builder.parserKeyHints = NewParserHint(nil, tt.LabelParseHints, false, false, "")

				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}

				expectedJSONPath := map[string][]string{
					"proxy_protocol_addr":      {"proxy_protocol_addr"},
					"remote_addr":              {"remote_addr"},
					"remote_user":              {"remote_user"},
					"upstream_addr":            {"upstream_addr"},
					"the_real_ip":              {"the_real_ip"},
					"protocol":                 {"protocol"},
					"upstream_name":            {"upstream_name"},
					"response_status":          {"response", "status"},
					"invalid":                  {"invalid"},
					"timestamp":                {"timestamp"},
					"response_upstream_status": {"response", "upstream_status"},
					"response_size":            {"response", "size"},
					"response_size_sent":       {"response", "size_sent"},
					"response_latency_seconds": {"response", "latency_seconds"},
				}

				for k, parts := range expectedJSONPath {
					require.Equal(b, parts, builder.GetJSONPath(k), "incorrect json path parts for key %s", k)
				}
			})

			b.Run("inline stages", func(b *testing.B) {
				b.ReportAllocs()
				stages := []Stage{NewStringLabelFilter(tt.LabelFilterParseHint)}
				builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
				builder.parserKeyHints = NewParserHint(nil, nil, false, false, ", nil")
				builder.labelFilterHints = NewLabelFilterHints(stages)
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}

				expectedJSONPath := map[string][]string{
					"invalid":             {"invalid"},
					"proxy_protocol_addr": {"proxy_protocol_addr"},
					"remote_addr":         {"remote_addr"},
					"remote_user":         {"remote_user"},
					"upstream_addr":       {"upstream_addr"},
					"the_real_ip":         {"the_real_ip"},
				}

				for k, parts := range expectedJSONPath {
					require.Equal(b, parts, builder.GetJSONPath(k), "incorrect json path parts for key %s", k)
				}
			})
		})
	}
}

func BenchmarkKeyExtraction(b *testing.B) {
	simpleJsn := []byte(`{
      "data": "Click Here",
      "size": 36,
      "style": "bold",
      "name": "text1",
      "hOffset": 250,
      "vOffset": 100,
      "alignment": "center",
      "onMouseUp": "sun1.opacity = (sun1.opacity / 100) * 90;"
    }`)
	logFmt := []byte(`data="Click Here" size=36 style=bold name=text1 hOffset=250 vOffset=100 alignment=center onMouseUp="sun1.opacity = (sun1.opacity / 100) * 90;"`)

	lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	lbs.parserKeyHints = parserHintsRequiring("name")

	benchmarks := []struct {
		name string
		p    Stage
		line []byte
	}{
		{"json", NewJSONParser(false), simpleJsn},
		{"logfmt", NewLogfmtParser(false, false), logFmt},
		{"logfmt-expression", mustStage(NewLogfmtExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("name", "name")}, false)), logFmt},
	}
	for _, bb := range benchmarks {
		b.Run(bb.name, func(b *testing.B) {
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				lbs.Reset()
				_, result = bb.p.Process(0, bb.line, lbs)
			}
		})
	}
}

func mustStage(s Stage, err error) Stage {
	if err != nil {
		panic(err)
	}
	return s
}

func TestNewRegexpParser(t *testing.T) {
	tests := []struct {
		name    string
		re      string
		wantErr bool
	}{
		{"no sub", "w.*", true},
		{"sub but not named", "f(.*) (foo|bar|buzz)", true},
		{"named and unamed", "blah (.*) (?P<foo>)", false},
		{"named", "blah (.*) (?P<foo>foo)(?P<bar>barr)", false},
		{"invalid name", "blah (.*) (?P<foo$>foo)(?P<bar>barr)", true},
		{"duplicate", "blah (.*) (?P<foo>foo)(?P<foo>barr)", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegexpParser(tt.re)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRegexpParser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_regexpParser_Parse(t *testing.T) {
	tests := []struct {
		name               string
		parser             Stage
		line               []byte
		lbs                labels.Labels
		want               labels.Labels
		structuredMetadata map[string]string
	}{
		{
			"no matches",
			mustStage(NewRegexpParser("(?P<foo>foo|bar)buzz")),
			[]byte("blah"),
			labels.FromStrings("app", "foo"),
			labels.FromStrings("app", "foo"),
			nil,
		},
		{
			"double matches",
			mustStage(NewRegexpParser("(?P<foo>.*)buzz")),
			[]byte("matchebuzz barbuzz"),
			labels.FromStrings("app", "bar"),
			labels.FromStrings("app", "bar",
				"foo", "matchebuzz bar",
			),
			nil,
		},
		{
			"duplicate labels",
			mustStage(NewRegexpParser("(?P<bar>bar)buzz")),
			[]byte("barbuzz"),
			labels.FromStrings("bar", "foo"),
			labels.FromStrings("bar", "foo",
				"bar_extracted", "bar",
			),
			nil,
		},
		{
			"multiple labels extracted",
			mustStage(NewRegexpParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)")),
			[]byte("status=200,latency=500ms"),
			labels.FromStrings("app", "foo"),
			labels.FromStrings("app", "foo",
				"status", "200",
				"latency", "500",
			),
			nil,
		},
		{
			"duplicate conflict with structured metadata",
			mustStage(NewRegexpParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)")),
			[]byte("status=200,latency=500ms"),
			labels.EmptyLabels(),
			labels.FromStrings("status", "400",
				"status_extracted", "200",
				"latency", "500",
			),
			map[string]string{"status": "400"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()
			// Set structured metadata if specified
			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}
			_, ok := tt.parser.Process(0, tt.line, b)
			require.True(t, ok)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}

	t.Run("extracts nothing when no stage needs a label", func(t *testing.T) {
		lbs := labels.FromStrings("app", "foo")
		hints := NewParserHint(nil, nil, false, true, "")
		b := NewBaseLabelsBuilderWithGrouping(nil, hints, false, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()

		_, ok := mustStage(NewRegexpParser("status=(?P<status>\\w+)")).Process(0, []byte("status=200"), b)
		require.True(t, ok)
		require.Equal(t, lbs, b.LabelsResult().Labels())
	})
}

func TestLogfmtParser_parse(t *testing.T) {
	tests := []struct {
		name               string
		line               []byte
		lbs                labels.Labels
		want               labels.Labels
		wantStrict         labels.Labels
		hints              ParserHint
		structuredMetadata map[string]string
	}{
		{
			"not logfmt",
			[]byte("foobar====wqe=sdad1r"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", "logfmt syntax error at pos 8 : unexpected '='",
			),
			NoParserHints(),
			nil,
		},
		{
			"not logfmt with hints",
			[]byte("foobar====wqe=sdad1r"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", "logfmt syntax error at pos 8 : unexpected '='",
			),
			parserHintsRequiring("__error__"),
			nil,
		},
		{
			"utf8 error rune",
			[]byte(`buzz=foo bar=�f`),
			labels.EmptyLabels(),
			labels.FromStrings("bar", " f", "buzz", "foo"),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"key alone logfmt",
			[]byte("buzz bar=foo"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"bar", "foo"),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"quoted logfmt",
			[]byte(`foobar="foo bar"`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "foo bar",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"escaped control chars in logfmt",
			[]byte(`foobar="foo\nbar\tbaz"`),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo\nbar\tbaz",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"literal control chars in logfmt",
			[]byte("foobar=\"foo\nbar\tbaz\""),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo\nbar\tbaz",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"escaped slash logfmt",
			[]byte(`foobar="foo ba\\r baz"`),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", `foo ba\r baz`,
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"literal newline and escaped slash logfmt",
			[]byte("foobar=\"foo bar\nb\\\\az\""),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo bar\nb\\az",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"double property logfmt",
			[]byte(`foobar="foo bar" latency=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "foo bar",
				"latency", "10ms",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"duplicate from line property",
			[]byte(`foobar="foo bar" foobar=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "foo bar",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"foobar", "10ms",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		// string interning should not cache conflicts across streams
		{
			"no duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			labels.EmptyLabels(),
			labels.FromStrings("foo", "foo bar",
				"foobar", "10ms",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"invalid key names",
			[]byte(`foo="foo bar" foo.bar=10ms test-dash=foo`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"foo_bar", "10ms",
				"test_dash", "foo",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"nil",
			nil,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			labels.EmptyLabels(),
			NoParserHints(),
			nil,
		},
		{
			"empty key",
			[]byte(`foo="foo bar" =notkey bar=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"bar", "10ms",
			),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", "logfmt syntax error at pos 15 : unexpected '='",
			),
			NoParserHints(),
			nil,
		},
		{
			"error rune in key",
			[]byte(`foo="foo bar" b�r=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
			),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", "logfmt syntax error at pos 20 : invalid key",
			),
			NoParserHints(),
			nil,
		},
		{
			"double quote in key",
			[]byte(`foo="foo bar" bu"zz=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
			),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", `logfmt syntax error at pos 17 : unexpected '"'`,
			),
			NoParserHints(),
			nil,
		},
		{
			"= in value",
			[]byte(`bar=bu=zz foo="foo bar"`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
			),
			labels.FromStrings("foo", "bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", `logfmt syntax error at pos 7 : unexpected '='`,
			),
			NoParserHints(),
			nil,
		},
		{
			"duplicate conflict with structured metadata",
			[]byte(`app=foo level=error`),
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo_sm",
				"app_extracted", "foo",
				"level", "error",
			),
			labels.EmptyLabels(),
			NoParserHints(),
			map[string]string{"app": "foo_sm"},
		},
	}

	{
		p := NewLogfmtParser(false, false)
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
				b.Reset()
				// Set structured metadata if specified
				for key, value := range tt.structuredMetadata {
					b.Set(StructuredMetadataLabel, key, value)
				}
				_, _ = p.Process(0, tt.line, b)
				require.Equal(t, tt.want, b.LabelsResult().Labels())
			})
		}
	}

	t.Run("strict", func(t *testing.T) {
		p := NewLogfmtParser(true, false)
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
				b.Reset()
				// Set structured metadata if specified
				for key, value := range tt.structuredMetadata {
					b.Set(StructuredMetadataLabel, key, value)
				}
				_, _ = p.Process(0, tt.line, b)

				want := tt.want
				if !tt.wantStrict.IsEmpty() {
					want = tt.wantStrict
				}
				require.Equal(t, want, b.LabelsResult().Labels())
			})
		}
	})
}

func TestLogfmtParser_keepEmpty(t *testing.T) {
	tests := []struct {
		name      string
		line      []byte
		keepEmpty bool
		lbs       labels.Labels
		want      labels.Labels
	}{
		{
			"without keep empty",
			[]byte("foo bar=buzz"),
			false,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"bar", "buzz"),
		},
		{
			"with keep empty",
			[]byte("foo bar=buzz"),
			true,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "",
				"bar", "buzz"),
		},
		{
			"utf8 error rune without keep empty",
			[]byte("foo=b�r bar=buzz"),
			false,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"bar", "buzz", "foo_extracted", "b r"),
		},
		{
			"utf8 error rune with keep empty",
			[]byte("foo=b�r bar=buzz"),
			true,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "b r",
				"bar", "buzz"),
		},
	}

	for _, strict := range []bool{false, true} {
		name := "strict"
		if !strict {
			name = "not " + name
		}

		t.Run(name, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					b := NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
					b.Reset()

					p := NewLogfmtParser(strict, tt.keepEmpty)
					_, _ = p.Process(0, tt.line, b)

					want := tt.want
					require.Equal(t, want, b.LabelsResult().Labels())
				})
			}
		})
	}
}

func TestLogfmtConsistentPrecedence(t *testing.T) {
	line := `app=lowkey level=error ts=2021-02-12T19:18:10.037940878Z msg="hello world"`

	t.Run("sturctured metadata first", func(t *testing.T) {
		var (
			metadataStream = NewBaseLabelsBuilder().
					ForLabels(labels.FromStrings("foo", "bar"), 0).
					Set(StructuredMetadataLabel, "app", "loki")

			basicStream = NewBaseLabelsBuilder().
					ForLabels(labels.FromStrings("foo", "baz"), 0)
		)

		parser := NewLogfmtParser(true, true)

		_, ok := parser.Process(0, []byte(line), metadataStream)
		require.True(t, ok)

		_, ok = parser.Process(0, []byte(line), basicStream)
		require.True(t, ok)

		// structured metadata is preserved and parsed labels get _extracted suffix
		res, cat, ok := metadataStream.GetWithCategory("app")
		require.Equal(t, "loki", res)
		require.Equal(t, StructuredMetadataLabel, cat)
		require.True(t, ok)

		res, cat, ok = metadataStream.GetWithCategory("app_extracted")
		require.Equal(t, "lowkey", res)
		require.Equal(t, ParsedLabel, cat)
		require.True(t, ok)

		res, cat, ok = basicStream.GetWithCategory("app")
		require.Equal(t, "lowkey", res)
		require.Equal(t, ParsedLabel, cat)
		require.True(t, ok)
	})

	t.Run("parsed labels first", func(t *testing.T) {
		var (
			metadataStream = NewBaseLabelsBuilder().
					ForLabels(labels.FromStrings("foo", "bar"), 0).
					Set(StructuredMetadataLabel, "app", "loki")

			basicStream = NewBaseLabelsBuilder().
					ForLabels(labels.FromStrings("foo", "baz"), 0)
		)

		parser := NewLogfmtParser(true, true)

		_, ok := parser.Process(0, []byte(line), basicStream)
		require.True(t, ok)

		_, ok = parser.Process(0, []byte(line), metadataStream)
		require.True(t, ok)

		// structured metadata is preserved and parsed labels get _extracted suffix
		res, cat, ok := metadataStream.GetWithCategory("app")
		require.Equal(t, "loki", res)
		require.Equal(t, StructuredMetadataLabel, cat)
		require.True(t, ok)

		res, cat, ok = metadataStream.GetWithCategory("app_extracted")
		require.Equal(t, "lowkey", res)
		require.Equal(t, ParsedLabel, cat)
		require.True(t, ok)

		res, cat, ok = basicStream.GetWithCategory("app")
		require.Equal(t, "lowkey", res)
		require.Equal(t, ParsedLabel, cat)
		require.True(t, ok)
	})
}

func TestLogfmtExpressionParser(t *testing.T) {
	testLine := []byte(`app=foo level=error spaces="value with ÜFT8👌" ts=2021-02-12T19:18:10.037940878Z`)

	tests := []struct {
		name               string
		line               []byte
		expressions        []LabelExtractionExpr
		lbs                labels.Labels
		want               labels.Labels
		structuredMetadata map[string]string
	}{
		{
			"single field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo"),
			nil,
		},
		{
			"multiple fields",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
				NewLabelExtractionExpr("level", "level"),
				NewLabelExtractionExpr("ts", "ts"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo",
				"level", "error",
				"ts", "2021-02-12T19:18:10.037940878Z",
			),
			nil,
		},
		{
			"label renaming",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("test", "level"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("test", "error"),
			nil,
		},
		{
			"multiple fields with label renaming",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
				NewLabelExtractionExpr("lvl", "level"),
				NewLabelExtractionExpr("timestamp", "ts"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo",
				"lvl", "error",
				"timestamp", "2021-02-12T19:18:10.037940878Z",
			),
			nil,
		},
		{
			"value with spaces and ÜFT8👌",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("spaces", "spaces"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("spaces", "value with ÜFT8👌"),
			nil,
		},
		{
			"expression matching nothing",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("nope", "nope"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("nope", ""),
			nil,
		},
		{
			"double property logfmt",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.FromStrings("ap", "bar"),
			labels.FromStrings("ap", "bar",
				"app", "foo",
			),
			nil,
		},
		{
			"label override",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.FromStrings("app", "bar"),
			labels.FromStrings("app", "bar",
				"app_extracted", "foo",
			),
			nil,
		},
		{
			"label override 2",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("lvl", "level"),
			},
			labels.FromStrings("level", "debug"),
			labels.FromStrings("level", "debug",
				"lvl", "error",
			),
			nil,
		},
		{
			"structured metadata conflict",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo_sm",
				"app_extracted", "foo",
			),
			map[string]string{"app": "foo_sm"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := NewLogfmtExpressionParser(tt.expressions, false)
			if err != nil {
				t.Fatalf("cannot create logfmt expression parser: %s", err.Error())
			}
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()
			// Set structured metadata if specified
			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}
			_, _ = l.Process(0, tt.line, b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}
}

func TestXExpressionParserFailures(t *testing.T) {
	tests := []struct {
		name       string
		expression LabelExtractionExpr
		error      string
	}{
		{
			"invalid field name",
			NewLabelExtractionExpr("app", `field with space`),
			"unexpected KEY",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLogfmtExpressionParser([]LabelExtractionExpr{tt.expression}, false)

			require.NotNil(t, err)
			require.Equal(t, err.Error(), fmt.Sprintf("cannot parse expression [%s]: syntax error: %s", tt.expression.Expression, tt.error))
		})
	}
}

func Test_unpackParser_Parse(t *testing.T) {
	tests := []struct {
		name               string
		line               []byte
		lbs                labels.Labels
		wantLbs            labels.Labels
		wantLine           []byte
		hints              ParserHint
		structuredMetadata map[string]string
	}{
		{
			"should extract only map[string]string",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}}`),
			labels.FromStrings("cluster", "us-central1"),
			labels.FromStrings("app", "foo",
				"namespace", "prod",
				"cluster", "us-central1",
			),
			[]byte(`some message`),
			NoParserHints(),
			nil,
		},
		{
			"wrong json",
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "expecting json object(6), but it is not",
			),
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			NoParserHints(),
			nil,
		},
		{
			"empty line",
			[]byte(``),
			labels.FromStrings("cluster", "us-central1"),
			labels.FromStrings("cluster", "us-central1"),
			[]byte(``),
			NoParserHints(),
			nil,
		},
		{
			"wrong json with hints",
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "expecting json object(6), but it is not",
			),
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			parserHintsRequiring("__error__"),
			nil,
		},
		{
			"not a map",
			[]byte(`["foo","bar"]`),
			labels.FromStrings("cluster", "us-central1"),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "expecting json object(6), but it is not",
				"cluster", "us-central1",
			),
			[]byte(`["foo","bar"]`),
			NoParserHints(),
			nil,
		},
		{
			"should rename",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}}`),
			labels.FromStrings("cluster", "us-central1",
				"app", "bar",
			),
			labels.FromStrings("app", "bar",
				"app_extracted", "foo",
				"namespace", "prod",
				"cluster", "us-central1",
			),
			[]byte(`some message`),
			NoParserHints(),
			nil,
		},
		{
			"should not change log and labels if no packed entry",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","pod":{"uid":"1"}}`),
			labels.FromStrings("app", "bar",
				"cluster", "us-central1",
			),
			labels.FromStrings("app", "bar",
				"cluster", "us-central1",
			),
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","pod":{"uid":"1"}}`),
			NoParserHints(),
			nil,
		},
		{
			"non json with escaped quotes",
			[]byte(`{"_entry":"I0303 17:49:45.976518    1526 kubelet_getters.go:178] \"Pod status updated\" pod=\"openshift-etcd/etcd-ip-10-0-150-50.us-east-2.compute.internal\" status=Running"}`),
			labels.FromStrings("app", "bar",
				"cluster", "us-central1",
			),
			labels.FromStrings("app", "bar",
				"cluster", "us-central1",
			),
			[]byte(`I0303 17:49:45.976518    1526 kubelet_getters.go:178] "Pod status updated" pod="openshift-etcd/etcd-ip-10-0-150-50.us-east-2.compute.internal" status=Running`),
			NoParserHints(),
			nil,
		},
		{
			"invalid key names",
			[]byte(`{"foo.bar":"10ms","test-dash":"foo","_entry":"some message"}`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_bar", "10ms",
				"test_dash", "foo",
			),
			[]byte(`some message`),
			NoParserHints(),
			nil,
		},
		{
			"duplicate conflict with structured metadata",
			[]byte(`{"app":"foo","namespace":"prod","_entry":"some message"}`),
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo_sm",
				"app_extracted", "foo",
				"namespace", "prod",
			),
			[]byte(`some message`),
			NoParserHints(),
			map[string]string{"app": "foo_sm"},
		},
	}
	for _, tt := range tests {
		j := NewUnpackParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()
			// Set structured metadata if specified
			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}
			cp := string(tt.line)
			l, _ := j.Process(0, tt.line, b)
			require.Equal(t, tt.wantLbs, b.LabelsResult().Labels())
			require.Equal(t, string(tt.wantLine), string(l))
			require.Equal(t, tt.wantLine, l)
			require.Equal(t, cp, string(tt.line), "the original log line should not be mutated")
		})
	}
}

func Test_PatternParser(t *testing.T) {
	tests := []struct {
		pattern            string
		line               []byte
		lbs                labels.Labels
		want               labels.Labels
		structuredMetadata map[string]string
	}{
		{
			`<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
			[]byte(`127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"ip", "127.0.0.1",
				"userid", "user-identifier",
				"user", "frank",
				"method", "GET",
				"path", "/apache_pb.gif",
				"status", "200",
				"size", "2326",
			),
			nil,
		},
		{
			`<_> msg="<method> <path> (<status>) <duration>"`,
			[]byte(`level=debug ts=2021-05-19T07:54:26.864644382Z caller=logging.go:66 traceID=7fbb92fd0eb9c65d msg="POST /loki/api/v1/push (204) 1.238734ms"`),
			labels.FromStrings("method", "bar"),
			labels.FromStrings("method", "bar",
				"method_extracted", "POST",
				"path", "/loki/api/v1/push",
				"status", "204",
				"duration", "1.238734ms",
			),
			nil,
		},
		{
			`foo <f>"`,
			[]byte(`bar`),
			labels.FromStrings("method", "bar"),
			labels.FromStrings("method", "bar"),
			nil,
		},
		{
			`<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
			[]byte(`127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`),
			labels.EmptyLabels(),
			labels.FromStrings(
				"ip", "127.0.0.1",
				"userid", "user-identifier",
				"user", "not_frank",
				"user_extracted", "frank",
				"method", "GET",
				"path", "/apache_pb.gif",
				"status", "200",
				"size", "2326",
			),
			map[string]string{"user": "not_frank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, labels.StableHash(tt.lbs))
			b.Reset()
			// Set structured metadata if specified
			for key, value := range tt.structuredMetadata {
				b.Set(StructuredMetadataLabel, key, value)
			}
			pp, err := NewPatternParser(tt.pattern)
			require.NoError(t, err)
			_, _ = pp.Process(0, tt.line, b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}
}

func BenchmarkJsonExpressionParser(b *testing.B) {
	simpleJsn := []byte(`{
      "data": "Click Here",
      "size": 36,
      "style": "bold",
      "name": "text1",
      "hOffset": 250,
      "vOffset": 100,
      "alignment": "center",
      "onMouseUp": "sun1.opacity = (sun1.opacity / 100) * 90;"
    }`)
	lbs := NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)

	benchmarks := []struct {
		name string
		p    Stage
		line []byte
	}{
		{"json-expression", mustStage(NewJSONExpressionParser([]LabelExtractionExpr{
			NewLabelExtractionExpr("data", "data"),
			NewLabelExtractionExpr("size", "size"),
			NewLabelExtractionExpr("style", "style"),
			NewLabelExtractionExpr("name", "name"),
			NewLabelExtractionExpr("hOffset", "hOffset"),
			NewLabelExtractionExpr("vOffset", "vOffset"),
			NewLabelExtractionExpr("alignment", "alignment"),
			NewLabelExtractionExpr("onMouseUp", "onMouseUp"),
		})), simpleJsn},
	}
	for _, bb := range benchmarks {
		b.Run(bb.name, func(b *testing.B) {
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				lbs.Reset()
				_, result = bb.p.Process(0, bb.line, lbs)
			}
		})
	}
}

// BenchmarkLogfmtExpressionParserNames measures the paths of the logfmt expression parser that depend on
// the names that it asks for: a name that the line has, a key that is renamed, a name that the line lacks,
// and a grouping on every name.
func BenchmarkLogfmtExpressionParserNames(b *testing.B) {
	line := []byte(`level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 ` +
		`latency=fast query="sum by (object_name) (rate(({container=\"metrics-generator\"})[1m]))" query_type=metric ` +
		`range_type=range length=1h0m0s step=1m0s duration=6.246061ms status=200 limit=1000 returned_lines=0 throughput=3.0MB`)
	names := []string{"level", "caller", "org_id", "latency"}

	exprs := func(names []string) []LabelExtractionExpr {
		out := make([]LabelExtractionExpr, 0, len(names))
		for _, n := range names {
			out = append(out, NewLabelExtractionExpr(n, n))
		}
		return out
	}

	benchmarks := []struct {
		name   string
		stream labels.Labels
		names  []string
		hints  ParserHint
	}{
		{"names that the line has", labels.FromStrings("cluster", "qa"), names, nil},
		// Two names clash with a stream label, so the parser renames their keys.
		{"names that clash with a stream label", labels.FromStrings("cluster", "qa", "level", "stream", "caller", "stream"), names, nil},
		// The line has none of the names, so the parser writes an empty label for each.
		{"names that the line lacks", labels.FromStrings("cluster", "qa"), []string{"missing_a", "missing_b", "missing_c", "missing_d"}, nil},
		// A grouping on every name lets the parser stop as soon as it has read them all.
		{"grouping on every name", labels.FromStrings("cluster", "qa"), names, NewParserHint(nil, names, false, false, "")},
		// The grouping does not need the renamed name, so the parser stops at the first key.
		{"grouping that does not need the renamed name", labels.FromStrings("cluster", "qa", "level", "stream"), []string{"level"}, NewParserHint(nil, []string{"level"}, false, false, "")},
	}
	for _, bb := range benchmarks {
		b.Run(bb.name, func(b *testing.B) {
			p := mustStage(NewLogfmtExpressionParser(exprs(bb.names), false))
			builder := NewBaseLabelsBuilder().ForLabels(bb.stream, labels.StableHash(bb.stream))
			if bb.hints != nil {
				builder.parserKeyHints = bb.hints
			}
			b.ReportAllocs()
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				builder.Reset()
				_, result = p.Process(0, line, builder)
			}
		})
	}
}

// renameLiteralKeyCases are lines that hold both a key that is renamed to foo_extracted (the stream
// has a label foo) and a literal foo_extracted key, in both orders, for every parser.
func renameLiteralKeyCases(t *testing.T) []struct {
	name   string
	parser Stage
	line   string
} {
	mustRegexp := func(re string) Stage {
		p, err := NewRegexpParser(re)
		require.NoError(t, err)
		return p
	}
	mustPattern := func(pn string) Stage {
		p, err := NewPatternParser(pn)
		require.NoError(t, err)
		return p
	}

	mustJSONExpr := func() Stage {
		p, err := NewJSONExpressionParser([]LabelExtractionExpr{
			NewLabelExtractionExpr("foo", "foo"),
			NewLabelExtractionExpr("foo_extracted", "foo_extracted"),
		})
		require.NoError(t, err)
		return p
	}
	mustLogfmtExpr := func() Stage {
		p, err := NewLogfmtExpressionParser([]LabelExtractionExpr{
			NewLabelExtractionExpr("foo", "foo"),
			NewLabelExtractionExpr("foo_extracted", "foo_extracted"),
		}, false)
		require.NoError(t, err)
		return p
	}

	return []struct {
		name   string
		parser Stage
		line   string
	}{
		{"json renamed first", NewJSONParser(false), `{"foo":"p","foo_extracted":"lit"}`},
		{"json literal first", NewJSONParser(false), `{"foo_extracted":"lit","foo":"p"}`},
		{"logfmt renamed first", NewLogfmtParser(false, false), `foo=p foo_extracted=lit`},
		{"logfmt literal first", NewLogfmtParser(false, false), `foo_extracted=lit foo=p`},
		{"regexp renamed first", mustRegexp(`(?P<foo>\w+)\|(?P<foo_extracted>\w+)`), `p|lit`},
		{"regexp literal first", mustRegexp(`(?P<foo_extracted>\w+)\|(?P<foo>\w+)`), `lit|p`},
		{"pattern renamed first", mustPattern(`<foo> <foo_extracted>`), `p lit`},
		{"pattern literal first", mustPattern(`<foo_extracted> <foo>`), `lit p`},
		{"unpack renamed first", NewUnpackParser(), `{"foo":"p","foo_extracted":"lit","_entry":"msg"}`},
		{"unpack literal first", NewUnpackParser(), `{"foo_extracted":"lit","foo":"p","_entry":"msg"}`},
		{"json expression renamed first", mustJSONExpr(), `{"foo":"p","foo_extracted":"lit"}`},
		{"json expression literal first", mustJSONExpr(), `{"foo_extracted":"lit","foo":"p"}`},
		{"logfmt expression renamed first", mustLogfmtExpr(), `foo=p foo_extracted=lit`},
		{"logfmt expression literal first", mustLogfmtExpr(), `foo_extracted=lit foo=p`},
	}
}

func TestParserRenameLiteralKeyPrecedence(t *testing.T) {
	// When a line contains both a key that collides with a stream label (and
	// is therefore renamed to key_extracted) and a literal key_extracted, both
	// target the same final label name. JSON does not define key order, so the
	// outcome must not depend on it: within a parser stage the literal key
	// always wins and the renamed key is dropped.
	base := labels.FromStrings("foo", "l")
	want := labels.FromStrings("foo", "l", "foo_extracted", "lit")

	for _, tt := range renameLiteralKeyCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilderWithGrouping(nil, NoParserHints(), false, false).
				ForLabels(base, labels.StableHash(base))
			b.Reset()
			_, _ = tt.parser.Process(0, []byte(tt.line), b)
			require.Equal(t, want, b.LabelsResult().Labels())
		})
	}
}

// A label filter after the parser may stop the parser on a line early. That check must not judge the
// value of a renamed key, because a literal key later in the line can replace it.
func TestParserRenameLiteralKeyPrecedenceWithLabelFilterHint(t *testing.T) {
	base := labels.FromStrings("foo", "l")
	want := labels.FromStrings("foo", "l", "foo_extracted", "lit")
	filter := NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo_extracted", "lit"))

	for _, tt := range renameLiteralKeyCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilderWithGrouping(nil, NoParserHints(), false, false).
				WithLabelFilterHints(NewLabelFilterHints(Stages{tt.parser, filter})).
				ForLabels(base, labels.StableHash(base))
			b.Reset()
			_, ok := tt.parser.Process(0, []byte(tt.line), b)
			require.True(t, ok, "the parser stopped on a line whose final value passes the filter")
			require.Equal(t, want, b.LabelsResult().Labels())
		})
	}
}

func TestLogfmtExpressionParserRenamedKeys(t *testing.T) {
	exprs := []LabelExtractionExpr{
		NewLabelExtractionExpr("foo", "foo"),
		NewLabelExtractionExpr("foo_extracted", "foo_extracted"),
		NewLabelExtractionExpr("bar", "bar"),
	}
	base := labels.FromStrings("foo", "l")

	for _, tt := range []struct {
		name string
		line string
		want labels.Labels
	}{
		{
			// The renamed key finds the name taken by the literal key later in the line. It must not
			// stop the parser: the key after the pair is still needed.
			name: "renamed key before the literal key",
			line: `foo=p foo_extracted=lit bar=b`,
			want: labels.FromStrings("foo", "l", "foo_extracted", "lit", "bar", "b"),
		},
		{
			name: "literal key before the renamed key",
			line: `foo_extracted=lit foo=p bar=b`,
			want: labels.FromStrings("foo", "l", "foo_extracted", "lit", "bar", "b"),
		},
		{
			// The name `foo_extracted` is asked for, but the line has no literal key. The empty label
			// that stands for a missing key must not take the place of the renamed value.
			name: "renamed key only",
			line: `foo=p bar=b`,
			want: labels.FromStrings("foo", "l", "foo_extracted", "p", "bar", "b"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parser, err := NewLogfmtExpressionParser(exprs, false)
			require.NoError(t, err)

			b := NewBaseLabelsBuilderWithGrouping(nil, NoParserHints(), false, false).
				ForLabels(base, labels.StableHash(base))
			b.Reset()
			_, _ = parser.Process(0, []byte(tt.line), b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}
}

// With a grouping on every name of the expression, the parser stops once all of them are extracted.
// The empty label that stands for a missing key must not count as extracted, or the parser stops
// after the first name.
// A name that the line lacks gets an empty label, but not when an earlier stage extracted the name.
func TestLogfmtExpressionParserKeepsTheValueOfAnEarlierStage(t *testing.T) {
	parser, err := NewLogfmtExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("foo", "foo")}, false)
	require.NoError(t, err)

	base := labels.FromStrings("app", "x")
	b := NewBaseLabelsBuilder().ForLabels(base, labels.StableHash(base))
	b.Reset()
	_, _ = NewJSONParser(false).Process(0, []byte(`{"foo":"first"}`), b)
	_, _ = parser.Process(0, []byte(`other=x`), b)
	require.Equal(t, labels.FromStrings("app", "x", "foo", "first"), b.LabelsResult().Labels())
}

func TestLogfmtExpressionParserReadsEveryRequiredName(t *testing.T) {
	parser, err := NewLogfmtExpressionParser([]LabelExtractionExpr{
		NewLabelExtractionExpr("a", "a"),
		NewLabelExtractionExpr("b", "b"),
	}, false)
	require.NoError(t, err)

	base := labels.FromStrings("app", "x")
	b := NewBaseLabelsBuilder().ForLabels(base, labels.StableHash(base))
	b.parserKeyHints = NewParserHint(nil, []string{"a", "b"}, false, false, "")
	b.Reset()
	_, _ = parser.Process(0, []byte(`a=1 b=2`), b)
	require.Equal(t, labels.FromStrings("app", "x", "a", "1", "b", "2"), b.LabelsResult().Labels())
}

// askedHints records the names that a parser asks the hints about.
type askedHints struct {
	ParserHint
	asked []string
}

func (h *askedHints) ShouldExtract(key string) bool {
	h.asked = append(h.asked, key)
	return h.ParserHint.ShouldExtract(key)
}

// readFillers reports whether the parser asked about a key that comes after the keys of the query.
func (h *askedHints) readFillers() bool {
	for _, k := range h.asked {
		if strings.HasPrefix(k, "filler_") {
			return true
		}
	}
	return false
}

// A key that a stream label holds is renamed. When the grouping does not need the renamed name, the parser
// stops reading the line unless another name of an expression is still wanted.
func TestLogfmtExpressionParserStopsWhenNoNameIsLeft(t *testing.T) {
	var fillers strings.Builder
	for i := 0; i < 29; i++ {
		fmt.Fprintf(&fillers, " filler_%d=v%d", i, i)
	}

	tests := []struct {
		name        string
		names       []string
		required    []string
		stream      labels.Labels // app="x" when empty
		metadata    labels.Labels
		line        string
		want        labels.Labels
		wantFillers bool
	}{
		{
			name:     "the only name clashes and the grouping does not need the renamed one",
			names:    []string{"app"},
			required: []string{"app"},
			line:     "app=y" + fillers.String(),
			want:     labels.FromStrings("app", "x"),
		},
		{
			name:     "no other name is needed",
			names:    []string{"app", "b"},
			required: []string{"app"},
			line:     "app=y" + fillers.String() + " b=1",
			want:     labels.FromStrings("app", "x", "b", ""),
		},
		{
			name:     "the other name comes before the renamed key",
			names:    []string{"app", "b"},
			required: []string{"app", "b"},
			line:     "b=1 app=y" + fillers.String(),
			want:     labels.FromStrings("app", "x", "b", "1"),
		},
		{
			// The metadata value c was renamed to c_extracted, so a parsed c would be dropped.
			name:     "the renamed name of another name is held by a label",
			names:    []string{"app", "c"},
			required: []string{"app", "c_extracted"},
			stream:   labels.FromStrings("app", "x", "c", "z"),
			metadata: labels.FromStrings("c", "m"),
			line:     "app=y" + fillers.String() + " c=1",
			want:     labels.FromStrings("app", "x", "c", "z", "c_extracted", "m"),
		},
		{
			name:        "another name is needed and comes later",
			names:       []string{"app", "b"},
			required:    []string{"app", "b"},
			line:        "app=y" + fillers.String() + " b=1",
			want:        labels.FromStrings("app", "x", "b", "1"),
			wantFillers: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exprs := make([]LabelExtractionExpr, 0, len(tt.names))
			for _, n := range tt.names {
				exprs = append(exprs, NewLabelExtractionExpr(n, n))
			}
			parser, err := NewLogfmtExpressionParser(exprs, false)
			require.NoError(t, err)

			hints := &askedHints{ParserHint: NewParserHint(nil, tt.required, false, false, "")}
			base := tt.stream
			if base.IsEmpty() {
				base = labels.FromStrings("app", "x")
			}
			b := NewBaseLabelsBuilder().ForLabels(base, labels.StableHash(base))
			b.parserKeyHints = hints
			b.Reset()
			b.Add(StructuredMetadataLabel, tt.metadata)
			_, ok := parser.Process(0, []byte(tt.line), b)
			require.True(t, ok)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
			require.Equal(t, tt.wantFillers, hints.readFillers(), "asked: %v", hints.asked)
		})
	}
}

// A parsed value that is renamed to `foo_extracted` must not replace a stream label or a structured
// metadata value that has this name. That includes a metadata value that was itself renamed to
// `foo_extracted` because its own name `foo` was taken (the three-way clash): the parsed value is
// dropped.
func TestParsedKeyDoesNotReplaceLabelWithTheName(t *testing.T) {
	mustRegexp := func(re string) Stage {
		p, err := NewRegexpParser(re)
		require.NoError(t, err)
		return p
	}
	mustPattern := func(pn string) Stage {
		p, err := NewPatternParser(pn)
		require.NoError(t, err)
		return p
	}
	mustJSONExpr := func() Stage {
		p, err := NewJSONExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("foo", "foo")})
		require.NoError(t, err)
		return p
	}
	mustLogfmtExpr := func() Stage {
		p, err := NewLogfmtExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("foo", "foo")}, false)
		require.NoError(t, err)
		return p
	}

	parsers := []struct {
		name   string
		parser Stage
		line   string
	}{
		{"json", NewJSONParser(false), `{"foo":"p"}`},
		{"logfmt", NewLogfmtParser(false, false), `foo=p`},
		{"regexp", mustRegexp(`(?P<foo>\w+)`), `p`},
		{"pattern", mustPattern(`<foo>`), `p`},
		{"unpack", NewUnpackParser(), `{"foo":"p","_entry":"msg"}`},
		{"json expression", mustJSONExpr(), `{"foo":"p"}`},
		{"logfmt expression", mustLogfmtExpr(), `foo=p`},
	}

	for _, tc := range []struct {
		name         string
		stream       labels.Labels
		metadata     labels.Labels
		wantStream   labels.Labels
		wantMetadata labels.Labels
		wantParsed   labels.Labels
	}{
		{
			name:       "stream label",
			stream:     labels.FromStrings("foo", "l", "foo_extracted", "le"),
			wantStream: labels.FromStrings("foo", "l", "foo_extracted", "le"),
		},
		{
			name:         "metadata key",
			stream:       labels.FromStrings("foo", "l"),
			metadata:     labels.FromStrings("foo_extracted", "me"),
			wantStream:   labels.FromStrings("foo", "l"),
			wantMetadata: labels.FromStrings("foo_extracted", "me"),
		},
		{
			// The metadata key `foo` is renamed to `foo_extracted` when it enters the pipeline. It
			// keeps that name, like a metadata key that is called `foo_extracted`.
			name:         "metadata value renamed to the name",
			stream:       labels.FromStrings("foo", "l"),
			metadata:     labels.FromStrings("foo", "m"),
			wantStream:   labels.FromStrings("foo", "l"),
			wantMetadata: labels.FromStrings("foo_extracted", "m"),
		},
	} {
		for _, p := range parsers {
			t.Run(tc.name+"/"+p.name, func(t *testing.T) {
				b := NewBaseLabelsBuilderWithGrouping(nil, NoParserHints(), false, false).
					ForLabels(tc.stream, labels.StableHash(tc.stream))
				b.Reset()
				b.Add(StructuredMetadataLabel, tc.metadata)
				_, ok := p.parser.Process(0, []byte(p.line), b)
				require.True(t, ok)

				res := b.LabelsResult()
				require.Equal(t, tc.wantStream, res.Stream())
				require.Equal(t, tc.wantMetadata, res.StructuredMetadata())
				require.Equal(t, tc.wantParsed, res.Parsed())
			})
		}
	}
}

// parserHintsRequiring returns hints that ask the parsers to extract names.
func parserHintsRequiring(names ...string) *ParserHints {
	stages := Stages{NewLogfmtParser(false, false)}
	for _, name := range names {
		stages = append(stages, NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, name, "")))
	}
	return NewParserHint(stages, nil, false, true, "")
}
