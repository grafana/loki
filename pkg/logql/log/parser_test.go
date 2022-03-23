package log

import (
	"fmt"
	"sort"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func Test_jsonParser_Parse(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		lbs  labels.Labels
		want labels.Labels
	}{
		{
			"multi depth",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			labels.Labels{},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
				{Name: "pod_deployment_ref", Value: "foobar"},
			},
		},
		{
			"numeric",
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			labels.Labels{},
			labels.Labels{
				{Name: "counter", Value: "1"},
				{Name: "price__net_", Value: "5.56909"},
			},
		},
		{
			"escaped",
			[]byte(`{"counter":1,"foo":"foo\\\"bar", "price": {"_net_":5.56909}}`),
			labels.Labels{},
			labels.Labels{
				{Name: "counter", Value: "1"},
				{Name: "price__net_", Value: "5.56909"},
				{Name: "foo", Value: `foo\"bar`},
			},
		},
		{
			"utf8 error rune",
			[]byte(`{"counter":1,"foo":"�", "price": {"_net_":5.56909}}`),
			labels.Labels{},
			labels.Labels{
				{Name: "counter", Value: "1"},
				{Name: "price__net_", Value: "5.56909"},
				{Name: "foo", Value: ""},
			},
		},
		{
			"skip arrays",
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			labels.Labels{},
			labels.Labels{
				{Name: "counter", Value: "1"},
			},
		},
		{
			"bad key replaced",
			[]byte(`{"cou-nter":1}`),
			labels.Labels{},
			labels.Labels{
				{Name: "cou_nter", Value: "1"},
			},
		},
		{
			"errors",
			[]byte(`{n}`),
			labels.Labels{},
			labels.Labels{
				{Name: logqlmodel.ErrorLabel, Value: errJSON},
			},
		},
		{
			"duplicate extraction",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}},"next":{"err":false}}`),
			labels.Labels{
				{Name: "app", Value: "bar"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "app_extracted", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
				{Name: "next_err", Value: "false"},
				{Name: "pod_deployment_ref", Value: "foobar"},
			},
		},
	}
	for _, tt := range tests {
		j := NewJSONParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = j.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}

func TestJSONExpressionParser(t *testing.T) {
	testLine := []byte(`{"app":"foo","field with space":"value","field with ÜFT8👌":"value","null_field":null,"bool_field":false,"namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar", "params": [1,2,3]}}}`)

	tests := []struct {
		name        string
		line        []byte
		expressions []JSONExpression
		lbs         labels.Labels
		want        labels.Labels
	}{
		{
			"single field",
			testLine,
			[]JSONExpression{
				NewJSONExpr("app", "app"),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "app", Value: "foo"},
			},
		},
		{
			"alternate syntax",
			testLine,
			[]JSONExpression{
				NewJSONExpr("test", `["field with space"]`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "test", Value: "value"},
			},
		},
		{
			"multiple fields",
			testLine,
			[]JSONExpression{
				NewJSONExpr("app", "app"),
				NewJSONExpr("namespace", "namespace"),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
			},
		},
		{
			"utf8",
			testLine,
			[]JSONExpression{
				NewJSONExpr("utf8", `["field with ÜFT8👌"]`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "utf8", Value: "value"},
			},
		},
		{
			"nested field",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", "pod.uuid"),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "uuid", Value: "foo"},
			},
		},
		{
			"nested field alternate syntax",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", `pod["uuid"]`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "uuid", Value: "foo"},
			},
		},
		{
			"nested field alternate syntax 2",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", `["pod"]["uuid"]`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "uuid", Value: "foo"},
			},
		},
		{
			"nested field alternate syntax 3",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", `["pod"].uuid`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "uuid", Value: "foo"},
			},
		},
		{
			"array element",
			testLine,
			[]JSONExpression{
				NewJSONExpr("param", `pod.deployment.params[0]`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "param", Value: "1"},
			},
		},
		{
			"full array",
			testLine,
			[]JSONExpression{
				NewJSONExpr("params", `pod.deployment.params`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "params", Value: "[1,2,3]"},
			},
		},
		{
			"full object",
			testLine,
			[]JSONExpression{
				NewJSONExpr("deployment", `pod.deployment`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "deployment", Value: `{"ref":"foobar", "params": [1,2,3]}`},
			},
		},
		{
			"expression matching nothing",
			testLine,
			[]JSONExpression{
				NewJSONExpr("nope", `pod.nope`),
			},
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "nope", Value: ""},
			},
		},
		{
			"null field",
			testLine,
			[]JSONExpression{
				NewJSONExpr("nf", `null_field`),
			},
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "nf", Value: ""}, // null is coerced to an empty string
			},
		},
		{
			"boolean field",
			testLine,
			[]JSONExpression{
				NewJSONExpr("bool", `bool_field`),
			},
			labels.Labels{},
			labels.Labels{
				{Name: "bool", Value: `false`},
			},
		},
		{
			"label override",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", `pod.uuid`),
			},
			labels.Labels{
				{Name: "uuid", Value: "bar"},
			},
			labels.Labels{
				{Name: "uuid", Value: "bar"},
				{Name: "uuid_extracted", Value: "foo"},
			},
		},
		{
			"non-matching expression",
			testLine,
			[]JSONExpression{
				NewJSONExpr("request_size", `request.size.invalid`),
			},
			labels.Labels{
				{Name: "uuid", Value: "bar"},
			},
			labels.Labels{
				{Name: "uuid", Value: "bar"},
				{Name: "request_size", Value: ""},
			},
		},
		{
			"empty line",
			[]byte("{}"),
			[]JSONExpression{
				NewJSONExpr("uuid", `pod.uuid`),
			},
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "uuid", Value: ""},
			},
		},
		{
			"existing labels are not affected",
			testLine,
			[]JSONExpression{
				NewJSONExpr("uuid", `will.not.work`),
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "uuid", Value: ""},
			},
		},
		{
			"invalid JSON line",
			[]byte(`invalid json`),
			[]JSONExpression{
				NewJSONExpr("uuid", `will.not.work`),
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: logqlmodel.ErrorLabel, Value: errJSON},
			},
		},
	}
	for _, tt := range tests {
		j, err := NewJSONExpressionParser(tt.expressions)
		if err != nil {
			t.Fatalf("cannot create JSON expression parser: %s", err.Error())
		}

		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = j.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}

func TestJSONExpressionParserFailures(t *testing.T) {
	tests := []struct {
		name       string
		expression JSONExpression
		error      string
	}{
		{
			"invalid field name",
			NewJSONExpr("app", `field with space`),
			"unexpected FIELD",
		},
		{
			"missing opening square bracket",
			NewJSONExpr("app", `"pod"]`),
			"unexpected STRING, expecting LSB or FIELD",
		},
		{
			"missing closing square bracket",
			NewJSONExpr("app", `["pod"`),
			"unexpected $end, expecting RSB",
		},
		{
			"missing closing square bracket",
			NewJSONExpr("app", `["pod""uuid"]`),
			"unexpected STRING, expecting RSB",
		},
		{
			"invalid nesting",
			NewJSONExpr("app", `pod..uuid`),
			"unexpected DOT, expecting FIELD",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJSONExpressionParser([]JSONExpression{tt.expression})

			require.NotNil(t, err)
			require.Equal(t, err.Error(), fmt.Sprintf("cannot parse expression [%s]: syntax error: %s", tt.expression.Expression, tt.error))
		})
	}
}

func Benchmark_Parser(b *testing.B) {
	lbs := labels.Labels{
		{Name: "cluster", Value: "qa-us-central1"},
		{Name: "namespace", Value: "qa"},
		{Name: "filename", Value: "/var/log/pods/ingress-nginx_nginx-ingress-controller-7745855568-blq6t_1f8962ef-f858-4188-a573-ba276a3cacc3/ingress-nginx/0.log"},
		{Name: "job", Value: "ingress-nginx/nginx-ingress-controller"},
		{Name: "name", Value: "nginx-ingress-controller"},
		{Name: "pod", Value: "nginx-ingress-controller-7745855568-blq6t"},
		{Name: "pod_template_hash", Value: "7745855568"},
		{Name: "stream", Value: "stdout"},
	}

	jsonLine := `{"invalid":"a\\xc5z","proxy_protocol_addr": "","remote_addr": "3.112.221.14","remote_user": "","upstream_addr": "10.12.15.234:5000","the_real_ip": "3.112.221.14","timestamp": "2020-12-11T16:20:07+00:00","protocol": "HTTP/1.1","upstream_name": "hosted-grafana-hosted-grafana-api-80","request": {"id": "c8eacb6053552c0cd1ae443bc660e140","time": "0.001","method" : "GET","host": "hg-api-qa-us-central1.grafana.net","uri": "/","size" : "128","user_agent": "worldping-api-","referer": ""},"response": {"status": 200,"upstream_status": "200","size": "1155","size_sent": "265","latency_seconds": "0.001"}}`
	logfmtLine := `level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 latency=fast query="sum by (object_name) (rate(({container=\"metrictank\", cluster=\"hm-us-east2\"} |= \"PANIC\")[5m]))" query_type=metric range_type=range length=5m0s step=15s duration=322.623724ms status=200 throughput=1.2GB total_bytes=375MB`
	nginxline := `10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1" 200 60755 "https://grafana.com/go/observabilitycon/grafana-the-open-and-composable-observability-platform/?tech=ggl-o&pg=oss-graf&plcmt=hero-txt" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.1 Safari/605.1.15" "123.123.123.123, 35.35.122.223" "TLSv1.3"`
	packedLike := `{"job":"123","pod":"someuid123","app":"foo","_entry":"10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1"}`

	for _, tt := range []struct {
		name            string
		line            string
		s               Stage
		LabelParseHints []string //  hints to reduce label extractions.
	}{
		{"json", jsonLine, NewJSONParser(), []string{"response_latency_seconds"}},
		{"jsonParser-not json line", nginxline, NewJSONParser(), []string{"response_latency_seconds"}},
		{"unpack", packedLike, NewUnpackParser(), []string{"pod"}},
		{"unpack-not json line", nginxline, NewUnpackParser(), []string{"pod"}},
		{"logfmt", logfmtLine, NewLogfmtParser(), []string{"info", "throughput", "org_id"}},
		{"regex greedy", nginxline, mustStage(NewRegexpParser(`GET (?P<path>.*?)/\?`)), []string{"path"}},
		{"regex status digits", nginxline, mustStage(NewRegexpParser(`HTTP/1.1" (?P<statuscode>\d{3}) `)), []string{"statuscode"}},
		{"pattern", nginxline, mustStage(NewPatternParser(`<_> "<method> <path> <_>"<_>`)), []string{"path"}},
	} {
		b.Run(tt.name, func(b *testing.B) {
			line := []byte(tt.line)
			b.Run("no labels hints", func(b *testing.B) {
				builder := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(line, builder)
				}
			})

			b.Run("labels hints", func(b *testing.B) {
				builder := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
				builder.parserKeyHints = newParserHint(tt.LabelParseHints, tt.LabelParseHints, false, false, "")
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(line, builder)
				}
			})
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
			labels.Labels{
				{Name: "app", Value: "foo"},
			},
			labels.Labels{
				{Name: "app", Value: "foo"},
			},
		},
		{
			"double matches",
			mustStage(NewRegexpParser("(?P<foo>.*)buzz")),
			[]byte("matchebuzz barbuzz"),
			labels.Labels{
				{Name: "app", Value: "bar"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "foo", Value: "matchebuzz bar"},
			},
		},
		{
			"duplicate labels",
			mustStage(NewRegexpParser("(?P<bar>bar)buzz")),
			[]byte("barbuzz"),
			labels.Labels{
				{Name: "bar", Value: "foo"},
			},
			labels.Labels{
				{Name: "bar", Value: "foo"},
				{Name: "bar_extracted", Value: "bar"},
			},
		},
		{
			"multiple labels extracted",
			mustStage(NewRegexpParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)")),
			[]byte("status=200,latency=500ms"),
			labels.Labels{
				{Name: "app", Value: "foo"},
			},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "status", Value: "200"},
				{Name: "latency", Value: "500"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = tt.parser.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}

func Test_logfmtParser_Parse(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		lbs  labels.Labels
		want labels.Labels
	}{
		{
			"not logfmt",
			[]byte("foobar====wqe=sdad1r"),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: logqlmodel.ErrorLabel, Value: errLogfmt},
			},
		},
		{
			"utf8 error rune",
			[]byte(`buzz=foo bar=�f`),
			labels.Labels{},
			labels.Labels{
				{Name: "buzz", Value: "foo"},
				{Name: "bar", Value: ""},
			},
		},
		{
			"key alone logfmt",
			[]byte("buzz bar=foo"),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: ""},
			},
		},
		{
			"quoted logfmt",
			[]byte(`foobar="foo bar"`),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "foobar", Value: "foo bar"},
			},
		},
		{
			"escaped control chars in logfmt",
			[]byte(`foobar="foo\nbar\tbaz"`),
			labels.Labels{
				{Name: "a", Value: "b"},
			},
			labels.Labels{
				{Name: "a", Value: "b"},
				{Name: "foobar", Value: "foo\nbar\tbaz"},
			},
		},
		{
			"literal control chars in logfmt",
			[]byte("foobar=\"foo\nbar\tbaz\""),
			labels.Labels{
				{Name: "a", Value: "b"},
			},
			labels.Labels{
				{Name: "a", Value: "b"},
				{Name: "foobar", Value: "foo\nbar\tbaz"},
			},
		},
		{
			"escaped slash logfmt",
			[]byte(`foobar="foo ba\\r baz"`),
			labels.Labels{
				{Name: "a", Value: "b"},
			},
			labels.Labels{
				{Name: "a", Value: "b"},
				{Name: "foobar", Value: `foo ba\r baz`},
			},
		},
		{
			"literal newline and escaped slash logfmt",
			[]byte("foobar=\"foo bar\nb\\\\az\""),
			labels.Labels{
				{Name: "a", Value: "b"},
			},
			labels.Labels{
				{Name: "a", Value: "b"},
				{Name: "foobar", Value: "foo bar\nb\\az"},
			},
		},
		{
			"double property logfmt",
			[]byte(`foobar="foo bar" latency=10ms`),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "foobar", Value: "foo bar"},
				{Name: "latency", Value: "10ms"},
			},
		},
		{
			"duplicate from line property",
			[]byte(`foobar="foo bar" foobar=10ms`),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "foobar", Value: "10ms"},
			},
		},
		{
			"duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "foo_extracted", Value: "foo bar"},
				{Name: "foobar", Value: "10ms"},
			},
		},
		{
			"invalid key names",
			[]byte(`foo="foo bar" foo.bar=10ms test-dash=foo`),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "foo_extracted", Value: "foo bar"},
				{Name: "foo_bar", Value: "10ms"},
				{Name: "test_dash", Value: "foo"},
			},
		},
		{
			"nil",
			nil,
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
		},
	}
	p := NewLogfmtParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = p.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}

func Test_syslogParser_Parse(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		lbs  labels.Labels
		want labels.Labels
	}{
		{
			"not syslog",
			[]byte("not a syslog"),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
		},
		{
			"invalid syslog",
			[]byte("2020-01-01T05:10:20.841485+01:00 localhost loki 4321 id1234 - messsage"),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: logqlmodel.ErrorLabel, Value: errSyslog},
			},
		},
		{
			"syslog with invalid structured data",
			[]byte("<14>1 2020-01-01T05:10:20.841485+01:00 localhost loki 5252 id12345 [meta label=foo] This is an interesting message"),
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: logqlmodel.ErrorLabel, Value: errSyslog},
			},
		},
		{
			"syslog without structured data",
			[]byte("<14>1 2020-01-01T05:10:20.841485+01:00 localhost loki 4321 id1234 - This is an interesting message"),
			labels.Labels{},
			labels.Labels{
				{Name: syslogAppname, Value: "loki"},
				{Name: syslogFacilityLevel, Value: "user"},
				{Name: syslogHostname, Value: "localhost"},
				{Name: syslogMessageID, Value: "id1234"},
				{Name: syslogProcID, Value: "4321"},
				{Name: syslogSeverityLevel, Value: "informational"},
				{Name: syslogMessage, Value: "This is an interesting message"},
			},
		},
		{
			"syslog with structured data",
			[]byte("<14>1 2020-01-01T05:10:20.841485+01:00 localhost loki 4321 id1234 [meta source=\"stdout\"][myid@5678 level=\"warn\"] This is an interesting message"),
			labels.Labels{},
			labels.Labels{
				{Name: syslogAppname, Value: "loki"},
				{Name: syslogFacilityLevel, Value: "user"},
				{Name: syslogHostname, Value: "localhost"},
				{Name: syslogMessageID, Value: "id1234"},
				{Name: syslogProcID, Value: "4321"},
				{Name: syslogSeverityLevel, Value: "informational"},
				{Name: "sd_meta_source", Value: "stdout"},
				{Name: "sd_myid_5678_level", Value: "warn"},
				{Name: syslogMessage, Value: "This is an interesting message"},
			},
		},
	}
	p := NewSyslogParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, _ = p.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
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
	}{
		{
			"should extract only map[string]string",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}}`),
			labels.Labels{{Name: "cluster", Value: "us-central1"}},
			labels.Labels{
				{Name: "app", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "cluster", Value: "us-central1"},
			},
			[]byte(`some message`),
		},
		{
			"wrong json",
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
			labels.Labels{},
			labels.Labels{
				{Name: "__error__", Value: "JSONParserErr"},
			},
			[]byte(`"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}`),
		},
		{
			"not a map",
			[]byte(`["foo","bar"]`),
			labels.Labels{{Name: "cluster", Value: "us-central1"}},
			labels.Labels{
				{Name: "__error__", Value: "JSONParserErr"},
				{Name: "cluster", Value: "us-central1"},
			},
			[]byte(`["foo","bar"]`),
		},
		{
			"should rename",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","_entry":"some message","pod":{"uid":"1"}}`),
			labels.Labels{
				{Name: "cluster", Value: "us-central1"},
				{Name: "app", Value: "bar"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "app_extracted", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "cluster", Value: "us-central1"},
			},
			[]byte(`some message`),
		},
		{
			"should not change log and labels if no packed entry",
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","pod":{"uid":"1"}}`),
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "cluster", Value: "us-central1"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "cluster", Value: "us-central1"},
			},
			[]byte(`{"bar":1,"app":"foo","namespace":"prod","pod":{"uid":"1"}}`),
		},
		{
			"non json with escaped quotes",
			[]byte(`{"_entry":"I0303 17:49:45.976518    1526 kubelet_getters.go:178] \"Pod status updated\" pod=\"openshift-etcd/etcd-ip-10-0-150-50.us-east-2.compute.internal\" status=Running"}`),
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "cluster", Value: "us-central1"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "cluster", Value: "us-central1"},
			},
			[]byte(`I0303 17:49:45.976518    1526 kubelet_getters.go:178] "Pod status updated" pod="openshift-etcd/etcd-ip-10-0-150-50.us-east-2.compute.internal" status=Running`),
		},
	}
	for _, tt := range tests {
		j := NewUnpackParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			copy := string(tt.line)
			l, _ := j.Process(tt.line, b)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.Labels())
			require.Equal(t, tt.wantLine, l)
			require.Equal(t, string(tt.wantLine), string(l))
			require.Equal(t, copy, string(tt.line), "the original log line should not be mutated")
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
			labels.Labels{
				{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				{Name: "foo", Value: "bar"},
				{Name: "ip", Value: "127.0.0.1"},
				{Name: "userid", Value: "user-identifier"},
				{Name: "user", Value: "frank"},
				{Name: "method", Value: "GET"},
				{Name: "path", Value: "/apache_pb.gif"},
				{Name: "status", Value: "200"},
				{Name: "size", Value: "2326"},
			},
		},
		{
			`<_> msg="<method> <path> (<status>) <duration>"`,
			[]byte(`level=debug ts=2021-05-19T07:54:26.864644382Z caller=logging.go:66 traceID=7fbb92fd0eb9c65d msg="POST /loki/api/v1/push (204) 1.238734ms"`),
			labels.Labels{
				{Name: "method", Value: "bar"},
			},
			labels.Labels{
				{Name: "method", Value: "bar"},
				{Name: "method_extracted", Value: "POST"},
				{Name: "path", Value: "/loki/api/v1/push"},
				{Name: "status", Value: "204"},
				{Name: "duration", Value: "1.238734ms"},
			},
		},
		{
			`foo <f>"`,
			[]byte(`bar`),
			labels.Labels{
				{Name: "method", Value: "bar"},
			},
			labels.Labels{
				{Name: "method", Value: "bar"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			pp, err := NewPatternParser(tt.pattern)
			require.NoError(t, err)
			_, _ = pp.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}
