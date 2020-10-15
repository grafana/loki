package log

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_jsonParser_Parse(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		lbs  Labels
		want Labels
	}{
		{
			"multi depth",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			Labels{},
			Labels{
				"app":                "foo",
				"namespace":          "prod",
				"pod_uuid":           "foo",
				"pod_deployment_ref": "foobar",
			},
		},
		{
			"numeric",
			[]byte(`{"counter":1, "price": {"_net_":5.56909}}`),
			Labels{},
			Labels{
				"counter":     "1",
				"price__net_": "5.56909",
			},
		},
		{
			"skip arrays",
			[]byte(`{"counter":1, "price": {"net_":["10","20"]}}`),
			Labels{},
			Labels{
				"counter": "1",
			},
		},
		{
			"bad key replaced",
			[]byte(`{"cou-nter":1}`),
			Labels{},
			Labels{
				"cou_nter": "1",
			},
		},
		{
			"errors",
			[]byte(`{n}`),
			Labels{},
			Labels{
				ErrorLabel: errJSON,
			},
		},
		{
			"duplicate extraction",
			[]byte(`{"app":"foo","namespace":"prod","pod":{"uuid":"foo","deployment":{"ref":"foobar"}}}`),
			Labels{
				"app": "bar",
			},
			Labels{
				"app":                "bar",
				"app_extracted":      "foo",
				"namespace":          "prod",
				"pod_uuid":           "foo",
				"pod_deployment_ref": "foobar",
			},
		},
	}
	for _, tt := range tests {
		j := NewJSONParser()
		t.Run(tt.name, func(t *testing.T) {
			_, _ = j.Process(tt.line, tt.lbs)
			require.Equal(t, tt.want, tt.lbs)
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
		lbs    Labels
		want   Labels
	}{
		{
			"no matches",
			mustNewRegexParser("(?P<foo>foo|bar)buzz"),
			[]byte("blah"),
			Labels{
				"app": "foo",
			},
			Labels{
				"app": "foo",
			},
		},
		{
			"double matches",
			mustNewRegexParser("(?P<foo>.*)buzz"),
			[]byte("matchebuzz barbuzz"),
			Labels{
				"app": "bar",
			},
			Labels{
				"app": "bar",
				"foo": "matchebuzz bar",
			},
		},
		{
			"duplicate labels",
			mustNewRegexParser("(?P<bar>bar)buzz"),
			[]byte("barbuzz"),
			Labels{
				"bar": "foo",
			},
			Labels{
				"bar":           "foo",
				"bar_extracted": "bar",
			},
		},
		{
			"multiple labels extracted",
			mustNewRegexParser("status=(?P<status>\\w+),latency=(?P<latency>\\w+)(ms|ns)"),
			[]byte("status=200,latency=500ms"),
			Labels{
				"app": "foo",
			},
			Labels{
				"app":     "foo",
				"status":  "200",
				"latency": "500",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = tt.parser.Process(tt.line, tt.lbs)
			require.Equal(t, tt.want, tt.lbs)
		})
	}
}

func Test_logfmtParser_Parse(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		lbs  Labels
		want Labels
	}{
		{
			"not logfmt",
			[]byte("foobar====wqe=sdad1r"),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":      "bar",
				ErrorLabel: errLogfmt,
			},
		},
		{
			"key alone logfmt",
			[]byte("buzz bar=foo"),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":  "bar",
				"bar":  "foo",
				"buzz": "",
			},
		},
		{
			"quoted logfmt",
			[]byte(`foobar="foo bar"`),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":    "bar",
				"foobar": "foo bar",
			},
		},
		{
			"double property logfmt",
			[]byte(`foobar="foo bar" latency=10ms`),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":     "bar",
				"foobar":  "foo bar",
				"latency": "10ms",
			},
		},
		{
			"duplicate from line property",
			[]byte(`foobar="foo bar" foobar=10ms`),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":    "bar",
				"foobar": "foo bar",
			},
		},
		{
			"duplicate property",
			[]byte(`foo="foo bar" foobar=10ms`),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":           "bar",
				"foo_extracted": "foo bar",
				"foobar":        "10ms",
			},
		},
		{
			"invalid key names",
			[]byte(`foo="foo bar" foo.bar=10ms test-dash=foo`),
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo":           "bar",
				"foo_extracted": "foo bar",
				"foo_bar":       "10ms",
				"test_dash":     "foo",
			},
		},
		{
			"nil",
			nil,
			Labels{
				"foo": "bar",
			},
			Labels{
				"foo": "bar",
			},
		},
	}
	p := NewLogfmtParser()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = p.Process(tt.line, tt.lbs)
			require.Equal(t, tt.want, tt.lbs)
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
