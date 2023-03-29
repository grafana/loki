package log

import (
	"fmt"
	"sort"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func Test_lineFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *LineFormatter
		lbs   labels.Labels
		ts    int64

		want    []byte
		wantLbs labels.Labels
		in      []byte
	}{
		{
			"count",
			newMustLineFormatter(
				`{{.foo | count "abc" }}`,
			),
			labels.Labels{{Name: "foo", Value: "abc abc abc"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("3"),
			labels.Labels{{Name: "foo", Value: "abc abc abc"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"count regex",
			newMustLineFormatter(
				`{{.foo | count "a|b|c" }}`,
			),
			labels.Labels{{Name: "foo", Value: "abc abc abc"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("9"),
			labels.Labels{{Name: "foo", Value: "abc abc abc"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"combining",
			newMustLineFormatter("foo{{.foo}}buzz{{  .bar  }}"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("fooblipbuzzblop"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"Replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ Replace .bar "blop" "bar" -1 }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("fooblipbuzzbar"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ .bar | replace "blop" "bar" }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("fooblipbuzzbar"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"title",
			newMustLineFormatter(`{{.foo | title }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("Blip"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"substr and trunc",
			newMustLineFormatter(
				`{{.foo | substr 1 3 }} {{ .bar  | trunc 1 }} {{ .bar  | trunc 3 }}`,
			),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("li b blo"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"trim",
			newMustLineFormatter(
				`{{.foo | trim }} {{ .bar  | trimAll "op" }} {{ .bar  | trimPrefix "b" }} {{ .bar  | trimSuffix "p" }}`,
			),
			labels.Labels{{Name: "foo", Value: "  blip "}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("blip bl lop blo"),
			labels.Labels{{Name: "foo", Value: "  blip "}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"lower and upper",
			newMustLineFormatter(`{{.foo | lower }} {{ .bar  | upper }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("blip BLOP"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"urlencode",
			newMustLineFormatter(`{{.foo | urlencode }} {{ urlencode .foo }}`), // assert both syntax forms
			labels.Labels{
				{Name: "foo", Value: `/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`},
			},
			0,
			[]byte("%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D %2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D"),
			labels.Labels{{Name: "foo", Value: `/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`}},
			nil,
		},
		{
			"urldecode",
			newMustLineFormatter(`{{.foo | urldecode }} {{ urldecode .foo }}`), // assert both syntax forms
			labels.Labels{
				{Name: "foo", Value: `%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D`},
			},
			0,
			[]byte(`/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"} /loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`),
			labels.Labels{{Name: "foo", Value: `%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D`}},
			nil,
		},
		{
			"repeat",
			newMustLineFormatter(`{{ "foo" | repeat 3 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("foofoofoo"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"indent",
			newMustLineFormatter(`{{ "foo\n bar" | indent 4 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("    foo\n     bar"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"nindent",
			newMustLineFormatter(`{{ "foo" | nindent 2 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("\n  foo"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"contains",
			newMustLineFormatter(`{{ if  .foo | contains "p"}}yes{{end}}-{{ if  .foo | contains "z"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"hasPrefix",
			newMustLineFormatter(`{{ if  .foo | hasPrefix "BL" }}yes{{end}}-{{ if  .foo | hasPrefix "p"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"hasSuffix",
			newMustLineFormatter(`{{ if  .foo | hasSuffix "Ip" }}yes{{end}}-{{ if  .foo | hasSuffix "pw"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"regexReplaceAll",
			newMustLineFormatter(`{{ regexReplaceAll "(p)" .foo "t" }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("BLIt"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"regexReplaceAllLiteral",
			newMustLineFormatter(`{{ regexReplaceAllLiteral "(p)" .foo "${1}" }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("BLI${1}"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"err",
			newMustLineFormatter(`{{.foo Replace "foo"}}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			nil,
			labels.Labels{
				{Name: "__error__", Value: "TemplateFormatErr"},
				{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"},
				{Name: "__error_details__", Value: "template: line:1:2: executing \"line\" at <.foo>: foo is not a method but has arguments"},
			},
			nil,
		},
		{
			"missing",
			newMustLineFormatter("foo {{.foo}}buzz{{  .bar  }}"),
			labels.Labels{{Name: "bar", Value: "blop"}},
			0,
			[]byte("foo buzzblop"),
			labels.Labels{{Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"function",
			newMustLineFormatter("foo {{.foo | ToUpper }} buzz{{  .bar  }}"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			[]byte("foo BLIP buzzblop"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
		},
		{
			"mathint",
			newMustLineFormatter("{{ add .foo 1 | sub .bar | mul .baz | div .bazz}}"),
			labels.Labels{{Name: "foo", Value: "1"}, {Name: "bar", Value: "3"}, {Name: "baz", Value: "10"}, {Name: "bazz", Value: "20"}},
			0,
			[]byte("2"),
			labels.Labels{{Name: "foo", Value: "1"}, {Name: "bar", Value: "3"}, {Name: "baz", Value: "10"}, {Name: "bazz", Value: "20"}},
			nil,
		},
		{
			"mathfloat",
			newMustLineFormatter("{{ addf .foo 1.5 | subf .bar 1.5 | mulf .baz | divf .bazz }}"),
			labels.Labels{{Name: "foo", Value: "1.5"}, {Name: "bar", Value: "5"}, {Name: "baz", Value: "10.5"}, {Name: "bazz", Value: "20.2"}},
			0,
			[]byte("3.8476190476190477"),
			labels.Labels{{Name: "foo", Value: "1.5"}, {Name: "bar", Value: "5"}, {Name: "baz", Value: "10.5"}, {Name: "bazz", Value: "20.2"}},
			nil,
		},
		{
			"mathfloatround",
			newMustLineFormatter("{{ round (addf .foo 1.5 | subf .bar | mulf .baz | divf .bazz) 5 .2}}"),
			labels.Labels{{Name: "foo", Value: "1.5"}, {Name: "bar", Value: "3.5"}, {Name: "baz", Value: "10.5"}, {Name: "bazz", Value: "20.4"}},
			0,
			[]byte("3.88572"),
			labels.Labels{{Name: "foo", Value: "1.5"}, {Name: "bar", Value: "3.5"}, {Name: "baz", Value: "10.5"}, {Name: "bazz", Value: "20.4"}},
			nil,
		},
		{
			"min",
			newMustLineFormatter("min is {{ min .foo .bar .baz }} and max is {{ max .foo .bar .baz }}"),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "10"}, {Name: "baz", Value: "15"}},
			0,
			[]byte("min is 5 and max is 15"),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "10"}, {Name: "baz", Value: "15"}},
			nil,
		},
		{
			"max",
			newMustLineFormatter("minf is {{ minf .foo .bar .baz }} and maxf is {{maxf .foo .bar .baz}}"),
			labels.Labels{{Name: "foo", Value: "5.3"}, {Name: "bar", Value: "10.5"}, {Name: "baz", Value: "15.2"}},
			0,
			[]byte("minf is 5.3 and maxf is 15.2"),
			labels.Labels{{Name: "foo", Value: "5.3"}, {Name: "bar", Value: "10.5"}, {Name: "baz", Value: "15.2"}},
			nil,
		},
		{
			"ceilfloor",
			newMustLineFormatter("ceil is {{ ceil .foo }} and floor is {{floor .foo }}"),
			labels.Labels{{Name: "foo", Value: "5.3"}},
			0,
			[]byte("ceil is 6 and floor is 5"),
			labels.Labels{{Name: "foo", Value: "5.3"}},
			nil,
		},
		{
			"mod",
			newMustLineFormatter("mod is {{ mod .foo 3 }}"),
			labels.Labels{{Name: "foo", Value: "20"}},
			0,
			[]byte("mod is 2"),
			labels.Labels{{Name: "foo", Value: "20"}},
			nil,
		},
		{
			"float64int",
			newMustLineFormatter("{{ \"2.5\" | float64 | int | add 10}}"),
			labels.Labels{{Name: "foo", Value: "2.5"}},
			0,
			[]byte("12"),
			labels.Labels{{Name: "foo", Value: "2.5"}},
			nil,
		},
		{
			"datetime",
			newMustLineFormatter("{{ sub (unixEpoch (toDate \"2006-01-02\" \"2021-11-02\")) (unixEpoch (toDate \"2006-01-02\" \"2021-11-01\")) }}"),
			labels.Labels{},
			0,
			[]byte("86400"),
			labels.Labels{},
			nil,
		},
		{
			"dateformat",
			newMustLineFormatter("{{ date \"2006-01-02\" (toDate \"2006-01-02\" \"2021-11-02\") }}"),
			labels.Labels{},
			0,
			[]byte("2021-11-02"),
			labels.Labels{},
			nil,
		},
		{
			"now",
			newMustLineFormatter("{{ div (unixEpoch now) (unixEpoch now) }}"),
			labels.Labels{},
			0,
			[]byte("1"),
			labels.Labels{},
			nil,
		},
		{
			"line",
			newMustLineFormatter("{{ __line__ }} bar {{ .bar }}"),
			labels.Labels{{Name: "bar", Value: "2"}},
			0,
			[]byte("1 bar 2"),
			labels.Labels{{Name: "bar", Value: "2"}},
			[]byte("1"),
		},
		{
			"default",
			newMustLineFormatter(`{{.foo | default "-" }}{{.bar | default "-"}}{{.unknown | default "-"}}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: ""}},
			0,
			[]byte("blip--"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: ""}},
			nil,
		},
		{
			"timestamp",
			newMustLineFormatter("{{ __timestamp__ | date \"2006-01-02\" }} bar {{ .bar }}"),
			labels.Labels{{Name: "bar", Value: "2"}},
			1656353124120000000,
			[]byte("2022-06-27 bar 2"),
			labels.Labels{{Name: "bar", Value: "2"}},
			[]byte("1"),
		},
		{
			"timestamp_unix",
			newMustLineFormatter("{{ __timestamp__ | unixEpoch }} bar {{ .bar }}"),
			labels.Labels{{Name: "bar", Value: "2"}},
			1656353124120000000,
			[]byte("1656353124 bar 2"),
			labels.Labels{{Name: "bar", Value: "2"}},
			[]byte("1"),
		},
		{
			"template_error",
			newMustLineFormatter("{{.foo | now}}"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			0,
			nil,
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "__error__", Value: "TemplateFormatErr"},
				{Name: "__error_details__", Value: "template: line:1:9: executing \"line\" at <now>: wrong number of args for now: want 0 got 1"},
			},
			nil,
		},
		{
			"bytes 1",
			newMustLineFormatter("{{ .foo | bytes }}"),
			labels.Labels{{Name: "foo", Value: "3 kB"}},
			1656353124120000000,
			[]byte("3000"),
			labels.Labels{{Name: "foo", Value: "3 kB"}},
			[]byte("1"),
		},
		{
			"bytes 2",
			newMustLineFormatter("{{ .foo | bytes }}"),
			labels.Labels{{Name: "foo", Value: "3MB"}},
			1656353124120000000,
			[]byte("3e+06"),
			labels.Labels{{Name: "foo", Value: "3MB"}},
			[]byte("1"),
		},
		{
			"duration 1",
			newMustLineFormatter("{{ .foo | duration }}"),
			labels.Labels{{Name: "foo", Value: "3ms"}},
			1656353124120000000,
			[]byte("0.003"),
			labels.Labels{{Name: "foo", Value: "3ms"}},
			[]byte("1"),
		},
		{
			"duration 2",
			newMustLineFormatter("{{ .foo | duration_seconds }}"),
			labels.Labels{{Name: "foo", Value: "3m10s"}},
			1656353124120000000,
			[]byte("190"),
			labels.Labels{{Name: "foo", Value: "3m10s"}},
			[]byte("1"),
		},
		{
			"toDateInZone",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			1656353124120000000,
			[]byte("1678411960340"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			[]byte("1"),
		},
		{
			"unixEpochMillis",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			1656353124120000000,
			[]byte("1678411960340"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			[]byte("1"),
		},
		{
			"unixEpochNanos",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochNanos }}"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			1656353124120000000,
			[]byte("1678411960340485723"),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}},
			[]byte("1"),
		},
		{
			"base64encode",
			newMustLineFormatter("{{ .foo | b64enc }}"),
			labels.Labels{{Name: "foo", Value: "i'm a string, encode me!"}},
			1656353124120000000,
			[]byte("aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"),
			labels.Labels{{Name: "foo", Value: "i'm a string, encode me!"}},
			[]byte("1"),
		},
		{
			"base64decode",
			newMustLineFormatter("{{ .foo | b64dec }}"),
			labels.Labels{{Name: "foo", Value: "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"}},
			1656353124120000000,
			[]byte("i'm a string, encode me!"),
			labels.Labels{{Name: "foo", Value: "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"}},
			[]byte("1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.lbs)
			sort.Sort(tt.wantLbs)
			builder := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			builder.Reset()
			outLine, _ := tt.fmter.Process(tt.ts, tt.in, builder)
			require.Equal(t, tt.want, outLine)
			require.Equal(t, tt.wantLbs, builder.LabelsResult().Labels())
		})
	}
}

func newMustLineFormatter(tmpl string) *LineFormatter {
	l, err := NewFormatter(tmpl)
	if err != nil {
		panic(err)
	}
	return l
}

func Test_labelsFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *LabelsFormatter

		in   labels.Labels
		want labels.Labels
	}{
		{
			"combined with template",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", "{{.foo}} and {{.bar}}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "foo", Value: "blip and blop"}, {Name: "bar", Value: "blop"}},
		},
		{
			"combined with template and rename",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo}} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "blip", Value: "blip and blop"}, {Name: "bar", Value: "blip"}},
		},
		{
			"fn",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo | ToUpper }} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "blip", Value: "BLIP and blop"}, {Name: "bar", Value: "blip"}},
		},
		{
			"math",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("status", "{{div .status 100 }}")}),
			labels.Labels{{Name: "status", Value: "200"}},
			labels.Labels{{Name: "status", Value: "2"}},
		},
		{
			"default",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", `{{.foo | default "-" }} and {{.bar}}`),
			}),
			labels.Labels{{Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "blip", Value: "- and blop"}, {Name: "bar", Value: "blop"}},
		},
		{
			"template error",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{replace \"test\" .foo}}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "__error__", Value: "TemplateFormatErr"},
				{Name: "__error_details__", Value: "template: label:1:2: executing \"label\" at <replace>: wrong number of args for replace: want 3 got 2"},
			},
		},
		{
			"line",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("line", "{{ __line__ }}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "line", Value: "test line"},
			},
		},
		{
			"timestamp",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("ts", "{{ __timestamp__ | date \"2006-01-02\" }}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "ts", Value: "2022-08-26"},
			},
		},
		{
			"timestamp_unix",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("ts", "{{ __timestamp__ | unixEpoch }}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "ts", Value: "1661518453"},
			},
		},
		{
			"count",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("count", `{{ __line__ | count "test" }}`)}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "blip"},
				{Name: "bar", Value: "blop"},
				{Name: "count", Value: "1"},
			},
		},
		{
			"count regex no matches",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("count", `{{ __line__ | count "notmatching.*" }}`)}),
			labels.Labels{},
			labels.Labels{
				{Name: "count", Value: "0"},
			},
		},
		{
			"bytes 1",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | bytes }}")}),
			labels.Labels{{Name: "foo", Value: "3 kB"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "3 kB"},
				{Name: "bar", Value: "3000"},
			},
		},
		{
			"bytes 2",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | bytes }}")}),
			labels.Labels{{Name: "foo", Value: "3MB"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "3MB"},
				{Name: "bar", Value: "3e+06"},
			},
		},
		{
			"duration 1",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | duration }}")}),
			labels.Labels{{Name: "foo", Value: "3ms"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "3ms"},
				{Name: "bar", Value: "0.003"},
			},
		},
		{
			"duration 2",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | duration }}")}),
			labels.Labels{{Name: "foo", Value: "3m10s"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "3m10s"},
				{Name: "bar", Value: "190"},
			},
		},
		{
			"toDateInZone",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}")}),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"},
				{Name: "bar", Value: "1678411960340"},
			},
		},
		{
			"unixEpochMillis",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}")}),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"},
				{Name: "bar", Value: "1678411960340"},
			},
		},
		{
			"unixEpochNanos",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochNanos }}")}),
			labels.Labels{{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "2023-03-10T01:32:40.340485723Z"},
				{Name: "bar", Value: "1678411960340485723"},
			},
		},
		{
			"base64encode",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | b64enc }}")}),
			labels.Labels{{Name: "foo", Value: "i'm a string, encode me!"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "i'm a string, encode me!"},
				{Name: "bar", Value: "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"},
			},
		},
		{
			"base64decode",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | b64dec }}")}),
			labels.Labels{{Name: "foo", Value: "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"}, {Name: "bar", Value: "blop"}},
			labels.Labels{
				{Name: "foo", Value: "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"},
				{Name: "bar", Value: "i'm a string, encode me!"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBaseLabelsBuilder().ForLabels(tt.in, tt.in.Hash())
			builder.Reset()
			_, _ = tt.fmter.Process(1661518453244672570, []byte("test line"), builder)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, builder.LabelsResult().Labels())
		})
	}
}

func mustNewLabelsFormatter(fmts []LabelFmt) *LabelsFormatter {
	lf, err := NewLabelsFormatter(fmts)
	if err != nil {
		panic(err)
	}
	return lf
}

func Test_InvalidRegex(t *testing.T) {
	t.Run("regexReplaceAll", func(t *testing.T) {
		cntFunc := functionMap["regexReplaceAll"]
		f := cntFunc.(func(string, string, string) (string, error))
		ret, err := f("a|b|\\q", "input", "replacement")
		require.Error(t, err)
		require.Empty(t, ret)
	})
	t.Run("regexReplaceAllLiteral", func(t *testing.T) {
		cntFunc := functionMap["regexReplaceAllLiteral"]
		f := cntFunc.(func(string, string, string) (string, error))
		ret, err := f("\\h", "input", "replacement")
		require.Error(t, err)
		require.Empty(t, ret)
	})
	t.Run("count", func(t *testing.T) {
		cntFunc := functionMap["count"]
		f := cntFunc.(func(string, string) (int, error))
		ret, err := f("a|b|\\K", "input")
		require.Error(t, err)
		require.Empty(t, ret)
	})
}

func Test_validate(t *testing.T) {
	tests := []struct {
		name    string
		fmts    []LabelFmt
		wantErr bool
	}{
		{"no dup", []LabelFmt{NewRenameLabelFmt("foo", "bar"), NewRenameLabelFmt("bar", "foo")}, false},
		{"dup", []LabelFmt{NewRenameLabelFmt("foo", "bar"), NewRenameLabelFmt("foo", "blip")}, true},
		{"no error", []LabelFmt{NewRenameLabelFmt(logqlmodel.ErrorLabel, "bar")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.fmts); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_trunc(t *testing.T) {
	tests := []struct {
		s    string
		c    int
		want string
	}{
		{"Hello, 世界", -1, "界"},
		{"Hello, 世界", 1, "H"},
		{"Hello, 世界", 0, ""},
		{"Hello, 世界", 20, "Hello, 世界"},
		{"Hello, 世界", -20, "Hello, 世界"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s%d", tt.s, tt.c), func(t *testing.T) {
			if got := trunc(tt.c, tt.s); got != tt.want {
				t.Errorf("trunc() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_substring(t *testing.T) {
	tests := []struct {
		start int
		end   int
		s     string
		want  string
	}{
		{1, 8, "Hello, 世界", "ello, 世"},
		{-10, 8, "Hello, 世界", "Hello, 世"},
		{1, 10, "Hello, 世界", "ello, 世界"},
		{-1, 10, "Hello, 世界", "Hello, 世界"},
		{-1, 1, "Hello, 世界", "H"},
		{-1, -1, "Hello, 世界", ""},
		{20, -1, "Hello, 世界", ""},
		{1, 1, "Hello, 世界", ""},
		{5, 1, "Hello, 世界", ""},
		{3, -1, "Hello, 世界", "lo, 世界"},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := substring(tt.start, tt.end, tt.s); got != tt.want {
				t.Errorf("substring() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLineFormatter_RequiredLabelNames(t *testing.T) {
	tests := []struct {
		fmt  string
		want []string
	}{
		{`{{.foo}} and {{.bar}}`, []string{"foo", "bar"}},
		{`{{ .foo | ToUpper | .buzz }} and {{.bar}}`, []string{"foo", "buzz", "bar"}},
		{`{{ regexReplaceAllLiteral "(p)" .foo "${1}" }}`, []string{"foo"}},
		{`{{ if  .foo | hasSuffix "Ip" }} {{.bar}} {{end}}-{{ if  .foo | hasSuffix "pw"}}no{{end}}`, []string{"foo", "bar"}},
		{`{{with .foo}}{{printf "%q" .}} {{end}}`, []string{"foo"}},
		{`{{with .foo}}{{printf "%q" .}} {{else}} {{ .buzz | lower }} {{end}}`, []string{"foo", "buzz"}},
	}
	for _, tt := range tests {
		t.Run(tt.fmt, func(t *testing.T) {
			require.Equal(t, tt.want, newMustLineFormatter(tt.fmt).RequiredLabelNames())
		})
	}
}

func TestLabelFormatter_RequiredLabelNames(t *testing.T) {
	tests := []struct {
		name string
		fmts []LabelFmt
		want []string
	}{
		{"rename", []LabelFmt{NewRenameLabelFmt("foo", "bar")}, []string{"bar"}},
		{"rename and fmt", []LabelFmt{NewRenameLabelFmt("fuzz", "bar"), NewTemplateLabelFmt("1", "{{ .foo | ToUpper | .buzz }} and {{.bar}}")}, []string{"bar", "foo", "buzz"}},
		{"fmt", []LabelFmt{NewTemplateLabelFmt("1", "{{.blip}}"), NewTemplateLabelFmt("2", "{{ .foo | ToUpper | .buzz }} and {{.bar}}")}, []string{"blip", "foo", "buzz", "bar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mustNewLabelsFormatter(tt.fmts).RequiredLabelNames())
		})
	}
}

func TestDecolorizer(t *testing.T) {
	var decolorizer, _ = NewDecolorizer()
	tests := []struct {
		name     string
		src      []byte
		expected []byte
	}{
		{"uncolored text remains the same", []byte("sample text"), []byte("sample text")},
		{"colored text loses color", []byte("\033[0;32mgreen\033[0m \033[0;31mred\033[0m"), []byte("green red")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result, _ = decolorizer.Process(0, tt.src, nil)
			require.Equal(t, tt.expected, result)
		})
	}
}
