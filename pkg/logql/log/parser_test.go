package log

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
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
				{Name: ErrorLabel, Value: errJSON},
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

	jsonLine := `{"proxy_protocol_addr": "","remote_addr": "3.112.221.14","remote_user": "","upstream_addr": "10.12.15.234:5000","the_real_ip": "3.112.221.14","timestamp": "2020-12-11T16:20:07+00:00","protocol": "HTTP/1.1","upstream_name": "hosted-grafana-hosted-grafana-api-80","request": {"id": "c8eacb6053552c0cd1ae443bc660e140","time": "0.001","method" : "GET","host": "hg-api-qa-us-central1.grafana.net","uri": "/","size" : "128","user_agent": "worldping-api","referer": ""},"response": {"status": 200,"upstream_status": "200","size": "1155","size_sent": "265","latency_seconds": "0.001"}}`
	logfmtLine := `level=info ts=2020-12-14T21:25:20.947307459Z caller=metrics.go:83 org_id=29 traceID=c80e691e8db08e2 latency=fast query="sum by (object_name) (rate(({container=\"metrictank\", cluster=\"hm-us-east2\"} |= \"PANIC\")[5m]))" query_type=metric range_type=range length=5m0s step=15s duration=322.623724ms status=200 throughput=1.2GB total_bytes=375MB`
	nginxline := `10.1.0.88 - - [14/Dec/2020:22:56:24 +0000] "GET /static/img/about/bob.jpg HTTP/1.1" 200 60755 "https://grafana.com/go/observabilitycon/grafana-the-open-and-composable-observability-platform/?tech=ggl-o&pg=oss-graf&plcmt=hero-txt" "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0.1 Safari/605.1.15" "123.123.123.123, 35.35.122.223" "TLSv1.3"`

	for _, tt := range []struct {
		name            string
		line            string
		s               Stage
		LabelParseHints []string //  hints to reduce label extractions.
	}{
		{"json", jsonLine, NewJSONParser(), []string{"response_latency_seconds"}},
		{"logfmt", logfmtLine, NewLogfmtParser(), []string{"info", "throughput", "org_id"}},
		{"regex greedy", nginxline, mustNewRegexParser(`GET (?P<path>.*?)/\?`), []string{"path"}},
		{"regex status digits", nginxline, mustNewRegexParser(`HTTP/1.1" (?P<statuscode>\d{3}) `), []string{"statuscode"}},
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
				builder.parserKeyHints = tt.LabelParseHints
				for n := 0; n < b.N; n++ {
					builder.Reset()
					_, _ = tt.s.Process(line, builder)
				}
			})
		})
	}

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
		parser *RegexpParser
		line   []byte
		lbs    labels.Labels
		want   labels.Labels
	}{
		{
			"no matches",
			mustNewRegexParser("(?P<foo>foo|bar)buzz"),
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
			mustNewRegexParser("(?P<foo>.*)buzz"),
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
			mustNewRegexParser("(?P<bar>bar)buzz"),
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
			mustNewRegexParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)"),
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
				{Name: ErrorLabel, Value: errLogfmt},
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
