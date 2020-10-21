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
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			labels.Labels{
				{Name: "app", Value: "bar"},
			},
			labels.Labels{
				{Name: "app", Value: "bar"},
				{Name: "app_extracted", Value: "foo"},
				{Name: "namespace", Value: "prod"},
				{Name: "pod_uuid", Value: "foo"},
				{Name: "pod_deployment_ref", Value: "foobar"},
			},
		},
	}
	for _, tt := range tests {
		j := NewJSONParser()
		t.Run(tt.name, func(t *testing.T) {
			b := NewLabelsBuilder()
			b.Reset(tt.lbs)
			_, _ = j.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
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
			b := NewLabelsBuilder()
			b.Reset(tt.lbs)
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
			b := NewLabelsBuilder()
			b.Reset(tt.lbs)
			_, _ = p.Process(tt.line, b)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, b.Labels())
		})
	}
}

func Test_sanitizeKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"1", "_1"},
		{"1 1 1", "_1_1_1"},
		{"abc", "abc"},
		{"$a$bc", "_a_bc"},
		{"$a$bc", "_a_bc"},
		{"   1 1 1  \t", "_1_1_1"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := sanitizeKey(tt.key); got != tt.want {
				t.Errorf("sanitizeKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
