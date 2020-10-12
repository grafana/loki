package logql

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_jsonParser_Parse(t *testing.T) {

	tests := []struct {
		name string
		j    *jsonParser
		line []byte
		lbs  labels.Labels
		want labels.Labels
	}{
		{
			"multi depth",
			NewJSONParser(),
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "app", Value: "foo"},
				labels.Label{Name: "namespace", Value: "prod"},
				labels.Label{Name: "pod_uuid", Value: "foo"},
				labels.Label{Name: "pod_deployment_ref", Value: "foobar"},
			},
		},
		{
			"numeric",
			NewJSONParser(),
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "counter", Value: "1"},
				labels.Label{Name: "price__net_", Value: "5.56909"},
			},
		},
		{
			"skip arrays",
			NewJSONParser(),
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "counter", Value: "1"},
			},
		},
		{
			"bad key replaced",
			NewJSONParser(),
			[]byte(`{"cou-nter":1}`),
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: "cou_nter", Value: "1"},
			},
		},
		{
			"errors",
			NewJSONParser(),
			[]byte(`{n}`),
			labels.Labels{},
			labels.Labels{
				labels.Label{Name: errorLabel, Value: errJSON},
			},
		},
		{
			"duplicate extraction",
			NewJSONParser(),
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			labels.Labels{
				labels.Label{Name: "app", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "app", Value: "bar"},
				labels.Label{Name: "app_extracted", Value: "foo"},
				labels.Label{Name: "namespace", Value: "prod"},
				labels.Label{Name: "pod_uuid", Value: "foo"},
				labels.Label{Name: "pod_deployment_ref", Value: "foobar"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.want)
			got := tt.j.Parse(tt.line, tt.lbs)
			require.Equal(t, tt.want, got)
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
		parser *regexpParser
		line   []byte
		lbs    labels.Labels
		want   labels.Labels
	}{
		{
			"no matches",
			mustNewRegexParser("(?P<foo>foo|bar)buzz"),
			[]byte("blah"),
			labels.Labels{
				labels.Label{Name: "app", Value: "foo"},
			},
			labels.Labels{
				labels.Label{Name: "app", Value: "foo"},
			},
		},
		{
			"double matches",
			mustNewRegexParser("(?P<foo>.*)buzz"),
			[]byte("matchebuzz barbuzz"),
			labels.Labels{
				labels.Label{Name: "app", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "app", Value: "bar"},
				labels.Label{Name: "foo", Value: "matchebuzz bar"},
			},
		},
		{
			"duplicate labels",
			mustNewRegexParser("(?P<bar>bar)buzz"),
			[]byte("barbuzz"),
			labels.Labels{
				labels.Label{Name: "bar", Value: "foo"},
			},
			labels.Labels{
				labels.Label{Name: "bar", Value: "foo"},
				labels.Label{Name: "bar_extracted", Value: "bar"},
			},
		},
		{
			"multiple labels extracted",
			mustNewRegexParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)"),
			[]byte("status=200,latency=500ms"),
			labels.Labels{
				labels.Label{Name: "app", Value: "foo"},
			},
			labels.Labels{
				labels.Label{Name: "app", Value: "foo"},
				labels.Label{Name: "status", Value: "200"},
				labels.Label{Name: "latency", Value: "500"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.want)
			got := tt.parser.Parse(tt.line, tt.lbs)
			require.Equal(t, tt.want, got)
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
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: errorLabel, Value: errLogfmt},
			},
		},
		{
			"key alone logfmt",
			[]byte("buzz bar=foo"),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "bar", Value: "foo"},
			},
		},
		{
			"quoted logfmt",
			[]byte(`foobar="foo bar"`),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "foobar", Value: "foo bar"},
			},
		},
		{
			"double property logfmt",
			[]byte(`foobar="foo bar" latency=10ms`),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "foobar", Value: "foo bar"},
				labels.Label{Name: "latency", Value: "10ms"},
			},
		},
		{
			"duplicate from line property",
			[]byte(`foobar="foo bar" foobar=10ms`),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "foobar", Value: "10ms"},
			},
		},
		{
			"duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "foo_extracted", Value: "foo bar"},
				labels.Label{Name: "foobar", Value: "10ms"},
			},
		},
		{
			"invalid key names",
			[]byte(`foo="foo bar" foo.bar=10ms test-dash=foo`),
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
			},
			labels.Labels{
				labels.Label{Name: "foo", Value: "bar"},
				labels.Label{Name: "foo_extracted", Value: "foo bar"},
				labels.Label{Name: "foo_bar", Value: "10ms"},
				labels.Label{Name: "test_dash", Value: "foo"},
			},
		},
	}
	p := NewLogfmtParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.want)
			got := p.Parse(tt.line, tt.lbs)
			require.Equal(t, tt.want, got)
		})
	}
}
