package log

import (
	"fmt"
	"sort"
	"testing"

	"github.com/grafana/loki/v3/pkg/logqlmodel"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_jsonParser_Parse(t *testing.T) {
	tests := []struct {
		name  string
		line  []byte
		lbs   labels.Labels
		want  labels.Labels
		hints ParserHint
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
			NoParserHints(),
		},
		{
			"numeric",
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
			),
			NoParserHints(),
		},
		{
			"whitespace key value",
			[]byte(`{" ": {"foo":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("foo", "bar"),
			NoParserHints(),
		},
		{
			"whitespace key and whitespace subkey values",
			[]byte(`{" ": {" ":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			NoParserHints(),
		},
		{
			"whitespace key and empty subkey values",
			[]byte(`{" ": {"":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			NoParserHints(),
		},
		{
			"empty key and empty subkey values",
			[]byte(`{"": {"":"bar"}}`),
			labels.EmptyLabels(),
			labels.FromStrings("", "bar"),
			NoParserHints(),
		},
		{
			"escaped",
			[]byte(`{"counter":1,"foo":"foo\\\"bar", "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
				"foo", `foo\"bar`,
			),
			NoParserHints(),
		},
		{
			"utf8 error rune",
			[]byte(`{"counter":1,"foo":"�", "price": {"_net_":5.56909}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1",
				"price__net_", "5.56909",
				"foo", " ",
			),
			NoParserHints(),
		},
		{
			"skip arrays",
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			labels.EmptyLabels(),
			labels.FromStrings("counter", "1"),
			NoParserHints(),
		},
		{
			"bad key replaced",
			[]byte(`{"cou-nter":1}`),
			labels.EmptyLabels(),
			labels.FromStrings("cou_nter", "1"),
			NoParserHints(),
		},
		{
			"errors",
			[]byte(`{n}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "Value looks like object, but can't find closing '}' symbol",
			),
			NoParserHints(),
		},
		{
			"errors hints",
			[]byte(`{n}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "Value looks like object, but can't find closing '}' symbol",
				"__preserve_error__", "true",
			),
			NewParserHint([]string{"__error__"}, nil, false, true, "", nil),
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
			NoParserHints(),
		},
	}
	for _, tt := range tests {
		j := NewJSONParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = j.Process(0, tt.line, b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
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

	for _, tt := range []struct {
		name                 string
		line                 []byte
		p                    Stage
		LabelFilterParseHint *labels.Matcher
	}{
		{"json", jsonLine, NewJSONParser(), labels.MustNewMatcher(labels.MatchEqual, "response_latency_seconds", "nope")},
		{"unpack", packedLike, NewUnpackParser(), labels.MustNewMatcher(labels.MatchEqual, "pod", "nope")},
		{"logfmt", logfmtLine, NewLogfmtParser(false, false), labels.MustNewMatcher(labels.MatchEqual, "info", "nope")},
		{"regex greedy", nginxline, mustStage(NewRegexpParser(`GET (?P<path>.*?)/\?`)), labels.MustNewMatcher(labels.MatchEqual, "path", "nope")},
		{"pattern", nginxline, mustStage(NewPatternParser(`<_> "<method> <path> <_>"<_>`)), labels.MustNewMatcher(labels.MatchEqual, "method", "nope")},
	} {
		lbs.Reset()
		t.Run(tt.name, func(t *testing.T) {
			_, result = tt.p.Process(0, tt.line, lbs)

			require.Len(t, lbs.labels(), 1)
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

	tests := []struct {
		name string
		p    Stage
		line []byte
	}{
		{"json", NewJSONParser(), simpleJsn},
		{"logfmt", NewLogfmtParser(false, false), logFmt},
		{"logfmt-expression", mustStage(NewLogfmtExpressionParser([]LabelExtractionExpr{NewLabelExtractionExpr("name", "name")}, false)), logFmt},
	}
	for _, tt := range tests {
		lbs.Reset()
		t.Run(tt.name, func(t *testing.T) {
			_, result = tt.p.Process(0, tt.line, lbs)

			require.Len(t, lbs.labels(), 1)
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

func (p *fakeParseHints) RecordExtracted(_ string) {
	p.count++
}

func (p *fakeParseHints) AllRequiredExtracted() bool {
	return !p.extractAll && p.count == 1
}

func (p *fakeParseHints) Reset() {
	p.checkCount = 0
	p.count = 0
}

func (p *fakeParseHints) PreserveError() bool {
	return false
}

func (p *fakeParseHints) ShouldContinueParsingLine(_ string, _ *LabelsBuilder) bool {
	return p.keepGoing
}

func TestJSONExpressionParser(t *testing.T) {
	testLine := []byte(`{"app":"foo","field with space":"value","field with ÜFT8👌":"value","null_field":null,"bool_field":false,"namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar", "params": [1,2,3,"string_value"]}}}`)

	tests := []struct {
		name        string
		line        []byte
		expressions []LabelExtractionExpr
		lbs         labels.Labels
		want        labels.Labels
		hints       ParserHint
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
				logqlmodel.PreserveErrorLabel, "true",
			),
			NewParserHint([]string{"__error__"}, nil, false, true, "", nil),
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := NewJSONExpressionParser(tt.expressions)
			require.NoError(t, err, "cannot create JSON expression parser")
			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
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
		{"json", jsonLine, NewJSONParser(), []string{"response_latency_seconds"}, labels.MustNewMatcher(labels.MatchEqual, "the_real_ip", "nope")},
		{"jsonParser-not json line", nginxline, NewJSONParser(), []string{"response_latency_seconds"}, labels.MustNewMatcher(labels.MatchEqual, "the_real_ip", "nope")},
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
				builder := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
			})

			b.Run("labels hints", func(b *testing.B) {
				b.ReportAllocs()
				builder := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
				builder.parserKeyHints = NewParserHint(tt.LabelParseHints, tt.LabelParseHints, false, false, "", nil)

				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
				}
			})

			b.Run("inline stages", func(b *testing.B) {
				b.ReportAllocs()
				stages := []Stage{NewStringLabelFilter(tt.LabelFilterParseHint)}
				builder := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
				builder.parserKeyHints = NewParserHint(nil, nil, false, false, ", nil", stages)
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(0, line, builder)
					builder.LabelsResult()
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
	lbs.parserKeyHints = NewParserHint([]string{"name"}, nil, false, true, "", nil)

	benchmarks := []struct {
		name string
		p    Stage
		line []byte
	}{
		{"json", NewJSONParser(), simpleJsn},
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
		name   string
		parser Stage
		line   []byte
		lbs    labels.Labels
		want   labels.Labels
	}{
		{
			"no matches",
			mustStage(NewRegexpParser("(?P<foo>foo|bar)buzz")),
			[]byte("blah"),
			labels.FromStrings("app", "foo"),
			labels.FromStrings("app", "foo"),
		},
		{
			"double matches",
			mustStage(NewRegexpParser("(?P<foo>.*)buzz")),
			[]byte("matchebuzz barbuzz"),
			labels.FromStrings("app", "bar"),
			labels.FromStrings("app", "bar",
				"foo", "matchebuzz bar",
			),
		},
		{
			"duplicate labels",
			mustStage(NewRegexpParser("(?P<bar>bar)buzz")),
			[]byte("barbuzz"),
			labels.FromStrings("bar", "foo"),
			labels.FromStrings("bar", "foo",
				"bar_extracted", "bar",
			),
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = tt.parser.Process(0, tt.line, b)
			require.Equal(t, tt.want, b.LabelsResult().Labels())
		})
	}
}

func TestLogfmtParser_parse(t *testing.T) {
	tests := []struct {
		name       string
		line       []byte
		lbs        labels.Labels
		want       labels.Labels
		wantStrict labels.Labels
		hints      ParserHint
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
		},
		{
			"not logfmt with hints",
			[]byte("foobar====wqe=sdad1r"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"__error__", "LogfmtParserErr",
				"__error_details__", "logfmt syntax error at pos 8 : unexpected '='",
				"__preserve_error__", "true",
			),
			NewParserHint([]string{"__error__"}, nil, false, true, "", nil),
		},
		{
			"utf8 error rune",
			[]byte(`buzz=foo bar=�f`),
			labels.EmptyLabels(),
			labels.FromStrings("bar", " f", "buzz", "foo"),
			nil,
			NoParserHints(),
		},
		{
			"key alone logfmt",
			[]byte("buzz bar=foo"),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"bar", "foo"),
			nil,
			NoParserHints(),
		},
		{
			"quoted logfmt",
			[]byte(`foobar="foo bar"`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "foo bar",
			),
			nil,
			NoParserHints(),
		},
		{
			"escaped control chars in logfmt",
			[]byte(`foobar="foo\nbar\tbaz"`),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo\nbar\tbaz",
			),
			nil,
			NoParserHints(),
		},
		{
			"literal control chars in logfmt",
			[]byte("foobar=\"foo\nbar\tbaz\""),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo\nbar\tbaz",
			),
			nil,
			NoParserHints(),
		},
		{
			"escaped slash logfmt",
			[]byte(`foobar="foo ba\\r baz"`),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", `foo ba\r baz`,
			),
			nil,
			NoParserHints(),
		},
		{
			"literal newline and escaped slash logfmt",
			[]byte("foobar=\"foo bar\nb\\\\az\""),
			labels.FromStrings("a", "b"),
			labels.FromStrings("a", "b",
				"foobar", "foo bar\nb\\az",
			),
			nil,
			NoParserHints(),
		},
		{
			"double property logfmt",
			[]byte(`foobar="foo bar" latency=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "foo bar",
				"latency", "10ms",
			),
			nil,
			NoParserHints(),
		},
		{
			"duplicate from line property",
			[]byte(`foobar="foo bar" foobar=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foobar", "10ms",
			),
			nil,
			NoParserHints(),
		},
		{
			"duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar",
				"foo_extracted", "foo bar",
				"foobar", "10ms",
			),
			nil,
			NoParserHints(),
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
			nil,
			NoParserHints(),
		},
		{
			"nil",
			nil,
			labels.FromStrings("foo", "bar"),
			labels.FromStrings("foo", "bar"),
			nil,
			NoParserHints(),
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
		},
	}

	{
		p := NewLogfmtParser(false, false)
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
				b.Reset()
				_, _ = p.Process(0, tt.line, b)
				require.Equal(t, tt.want, b.LabelsResult().Labels())
			})
		}
	}

	t.Run("strict", func(t *testing.T) {
		p := NewLogfmtParser(true, false)
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
				b.Reset()
				_, _ = p.Process(0, tt.line, b)

				want := tt.want
				if tt.wantStrict != nil {
					want = tt.wantStrict
				}
				sort.Sort(want)
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
					b := NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
					b.Reset()

					p := NewLogfmtParser(strict, tt.keepEmpty)
					_, _ = p.Process(0, tt.line, b)

					want := tt.want
					sort.Sort(want)
					require.Equal(t, want, b.LabelsResult().Labels())
				})
			}
		})
	}
}

func TestLogfmtExpressionParser(t *testing.T) {
	testLine := []byte(`app=foo level=error spaces="value with ÜFT8👌" ts=2021-02-12T19:18:10.037940878Z`)

	tests := []struct {
		name        string
		line        []byte
		expressions []LabelExtractionExpr
		lbs         labels.Labels
		want        labels.Labels
	}{
		{
			"single field",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("app", "app"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("app", "foo"),
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
		},
		{
			"label renaming",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("test", "level"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("test", "error"),
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
		},
		{
			"value with spaces and ÜFT8👌",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("spaces", "spaces"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("spaces", "value with ÜFT8👌"),
		},
		{
			"expression matching nothing",
			testLine,
			[]LabelExtractionExpr{
				NewLabelExtractionExpr("nope", "nope"),
			},
			labels.EmptyLabels(),
			labels.FromStrings("nope", ""),
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := NewLogfmtExpressionParser(tt.expressions, false)
			if err != nil {
				t.Fatalf("cannot create logfmt expression parser: %s", err.Error())
			}
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
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
		name string
		line []byte
		lbs  labels.Labels

		wantLbs  labels.Labels
		wantLine []byte
		hints    ParserHint
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
		},
		{
			"empty line",
			[]byte(``),
			labels.FromStrings("cluster", "us-central1"),
			labels.FromStrings("cluster", "us-central1"),
			[]byte(``),
			NoParserHints(),
		},
		{
			"wrong json with hints",
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			labels.EmptyLabels(),
			labels.FromStrings("__error__", "JSONParserErr",
				"__error_details__", "expecting json object(6), but it is not",
				"__preserve_error__", "true",
			),
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			NewParserHint([]string{"__error__"}, nil, false, true, "", nil),
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
		},
	}
	for _, tt := range tests {
		j := NewUnpackParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilderWithGrouping(nil, tt.hints, false, false).ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
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
		pattern string
		line    []byte
		lbs     labels.Labels
		want    labels.Labels
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
		},
		{
			`foo <f>"`,
			[]byte(`bar`),
			labels.FromStrings("method", "bar"),
			labels.FromStrings("method", "bar"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
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
