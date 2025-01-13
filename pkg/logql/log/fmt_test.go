package log

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
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
			labels.FromStrings("foo", "abc abc abc", "bar", "blop"),
			0,
			[]byte("3"),
			labels.FromStrings("foo", "abc abc abc", "bar", "blop"),
			nil,
		},
		{
			"count regex",
			newMustLineFormatter(
				`{{.foo | count "a|b|c" }}`,
			),
			labels.FromStrings("foo", "abc abc abc", "bar", "blop"),
			0,
			[]byte("9"),
			labels.FromStrings("foo", "abc abc abc", "bar", "blop"),
			nil,
		},
		{
			"combining",
			newMustLineFormatter("foo{{.foo}}buzz{{  .bar  }}"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("fooblipbuzzblop"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"Replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ Replace .bar "blop" "bar" -1 }}`),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("fooblipbuzzbar"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ .bar | replace "blop" "bar" }}`),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("fooblipbuzzbar"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"title",
			newMustLineFormatter(`{{.foo | title }}`),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("Blip"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"substr and trunc",
			newMustLineFormatter(
				`{{.foo | substr 1 3 }} {{ .bar  | trunc 1 }} {{ .bar  | trunc 3 }}`,
			),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("li b blo"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"trim",
			newMustLineFormatter(
				`{{.foo | trim }} {{ .bar  | trimAll "op" }} {{ .bar  | trimPrefix "b" }} {{ .bar  | trimSuffix "p" }}`,
			),
			labels.FromStrings("foo", "  blip ", "bar", "blop"),
			0,
			[]byte("blip bl lop blo"),
			labels.FromStrings("foo", "  blip ", "bar", "blop"),
			nil,
		},
		{
			"lower and upper",
			newMustLineFormatter(`{{.foo | lower }} {{ .bar  | upper }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("blip BLOP"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"urlencode",
			newMustLineFormatter(`{{.foo | urlencode }} {{ urlencode .foo }}`), // assert both syntax forms
			labels.FromStrings("foo", `/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`),
			0,
			[]byte("%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D %2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D"),
			labels.FromStrings("foo", `/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`),
			nil,
		},
		{
			"urldecode",
			newMustLineFormatter(`{{.foo | urldecode }} {{ urldecode .foo }}`), // assert both syntax forms
			labels.FromStrings("foo", `%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D`),
			0,
			[]byte(`/loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"} /loki/api/v1/query?query=sum(count_over_time({stream_filter="some_stream",environment="prod", host=~"someec2.*"}`),
			labels.FromStrings("foo", `%2Floki%2Fapi%2Fv1%2Fquery%3Fquery%3Dsum%28count_over_time%28%7Bstream_filter%3D%22some_stream%22%2Cenvironment%3D%22prod%22%2C+host%3D~%22someec2.%2A%22%7D`),
			nil,
		},
		{
			"repeat",
			newMustLineFormatter(`{{ "foo" | repeat 3 }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("foofoofoo"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"indent",
			newMustLineFormatter(`{{ "foo\n bar" | indent 4 }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("    foo\n     bar"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"nindent",
			newMustLineFormatter(`{{ "foo" | nindent 2 }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("\n  foo"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"contains",
			newMustLineFormatter(`{{ if  .foo | contains "p"}}yes{{end}}-{{ if  .foo | contains "z"}}no{{end}}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("yes-"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"hasPrefix",
			newMustLineFormatter(`{{ if  .foo | hasPrefix "BL" }}yes{{end}}-{{ if  .foo | hasPrefix "p"}}no{{end}}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("yes-"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"hasSuffix",
			newMustLineFormatter(`{{ if  .foo | hasSuffix "Ip" }}yes{{end}}-{{ if  .foo | hasSuffix "pw"}}no{{end}}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("yes-"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"regexReplaceAll",
			newMustLineFormatter(`{{ regexReplaceAll "(p)" .foo "t" }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("BLIt"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"regexReplaceAllLiteral",
			newMustLineFormatter(`{{ regexReplaceAllLiteral "(p)" .foo "${1}" }}`),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			0,
			[]byte("BLI${1}"),
			labels.FromStrings("foo", "BLIp", "bar", "blop"),
			nil,
		},
		{
			"err",
			newMustLineFormatter(`{{.foo Replace "foo"}}`),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			nil,
			labels.FromStrings("__error__", "TemplateFormatErr",
				"foo", "blip", "bar", "blop",
				"__error_details__", "template: line:1:2: executing \"line\" at <.foo>: foo is not a method but has arguments",
			),
			nil,
		},
		{
			"missing",
			newMustLineFormatter("foo {{.foo}}buzz{{  .bar  }}"),
			labels.FromStrings("bar", "blop"),
			0,
			[]byte("foo buzzblop"),
			labels.FromStrings("bar", "blop"),
			nil,
		},
		{
			"function",
			newMustLineFormatter("foo {{.foo | ToUpper }} buzz{{  .bar  }}"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			[]byte("foo BLIP buzzblop"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			nil,
		},
		{
			"mathint",
			newMustLineFormatter("{{ add .foo 1 | sub .bar | mul .baz | div .bazz}}"),
			labels.FromStrings("foo", "1", "bar", "3", "baz", "10", "bazz", "20"),
			0,
			[]byte("2"),
			labels.FromStrings("foo", "1", "bar", "3", "baz", "10", "bazz", "20"),
			nil,
		},
		{
			"mathfloat",
			newMustLineFormatter("{{ addf .foo 1.5 | subf .bar 1.5 | mulf .baz | divf .bazz }}"),
			labels.FromStrings("foo", "1.5", "bar", "5", "baz", "10.5", "bazz", "20.2"),
			0,
			[]byte("3.8476190476190477"),
			labels.FromStrings("foo", "1.5", "bar", "5", "baz", "10.5", "bazz", "20.2"),
			nil,
		},
		{
			"mathfloatround",
			newMustLineFormatter("{{ round (addf .foo 1.5 | subf .bar | mulf .baz | divf .bazz) 5 .2}}"),
			labels.FromStrings("foo", "1.5", "bar", "3.5", "baz", "10.5", "bazz", "20.4"),
			0,
			[]byte("3.88572"),
			labels.FromStrings("foo", "1.5", "bar", "3.5", "baz", "10.5", "bazz", "20.4"),
			nil,
		},
		{
			"min",
			newMustLineFormatter("min is {{ min .foo .bar .baz }} and max is {{ max .foo .bar .baz }}"),
			labels.FromStrings("foo", "5", "bar", "10", "baz", "15"),
			0,
			[]byte("min is 5 and max is 15"),
			labels.FromStrings("foo", "5", "bar", "10", "baz", "15"),
			nil,
		},
		{
			"max",
			newMustLineFormatter("minf is {{ minf .foo .bar .baz }} and maxf is {{maxf .foo .bar .baz}}"),
			labels.FromStrings("foo", "5.3", "bar", "10.5", "baz", "15.2"),
			0,
			[]byte("minf is 5.3 and maxf is 15.2"),
			labels.FromStrings("foo", "5.3", "bar", "10.5", "baz", "15.2"),
			nil,
		},
		{
			"ceilfloor",
			newMustLineFormatter("ceil is {{ ceil .foo }} and floor is {{floor .foo }}"),
			labels.FromStrings("foo", "5.3"),
			0,
			[]byte("ceil is 6 and floor is 5"),
			labels.FromStrings("foo", "5.3"),
			nil,
		},
		{
			"mod",
			newMustLineFormatter("mod is {{ mod .foo 3 }}"),
			labels.FromStrings("foo", "20"),
			0,
			[]byte("mod is 2"),
			labels.FromStrings("foo", "20"),
			nil,
		},
		{
			"float64int",
			newMustLineFormatter("{{ \"2.5\" | float64 | int | add 10}}"),
			labels.FromStrings("foo", "2.5"),
			0,
			[]byte("12"),
			labels.FromStrings("foo", "2.5"),
			nil,
		},
		{
			"datetime",
			newMustLineFormatter("{{ sub (unixEpoch (toDate \"2006-01-02\" \"2021-11-02\")) (unixEpoch (toDate \"2006-01-02\" \"2021-11-01\")) }}"),
			labels.EmptyLabels(),
			0,
			[]byte("86400"),
			labels.EmptyLabels(),
			nil,
		},
		{
			"dateformat",
			newMustLineFormatter("{{ date \"2006-01-02\" (toDate \"2006-01-02\" \"2021-11-02\") }}"),
			labels.EmptyLabels(),
			0,
			[]byte("2021-11-02"),
			labels.EmptyLabels(),
			nil,
		},
		{
			"now",
			newMustLineFormatter("{{ div (unixEpoch now) (unixEpoch now) }}"),
			labels.EmptyLabels(),
			0,
			[]byte("1"),
			labels.EmptyLabels(),
			nil,
		},
		{
			"line",
			newMustLineFormatter("{{ __line__ }} bar {{ .bar }}"),
			labels.FromStrings("bar", "2"),
			0,
			[]byte("1 bar 2"),
			labels.FromStrings("bar", "2"),
			[]byte("1"),
		},
		{
			"default",
			newMustLineFormatter(`{{.foo | default "-" }}{{.bar | default "-"}}{{.unknown | default "-"}}`),
			labels.FromStrings("foo", "blip", "bar", ""),
			0,
			[]byte("blip--"),
			labels.FromStrings("foo", "blip", "bar", ""),
			nil,
		},
		{
			"timestamp",
			newMustLineFormatter("{{ __timestamp__ | date \"2006-01-02\" }} bar {{ .bar }}"),
			labels.FromStrings("bar", "2"),
			1656353124120000000,
			[]byte("2022-06-27 bar 2"),
			labels.FromStrings("bar", "2"),
			[]byte("1"),
		},
		{
			"timestamp_unix",
			newMustLineFormatter("{{ __timestamp__ | unixEpoch }} bar {{ .bar }}"),
			labels.FromStrings("bar", "2"),
			1656353124120000000,
			[]byte("1656353124 bar 2"),
			labels.FromStrings("bar", "2"),
			[]byte("1"),
		},
		{
			"template_error",
			newMustLineFormatter("{{.foo | now}}"),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			0,
			nil,
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"__error__", "TemplateFormatErr",
				"__error_details__", "template: line:1:9: executing \"line\" at <now>: wrong number of args for now: want 0 got 1",
			),
			nil,
		},
		{
			"bytes 1",
			newMustLineFormatter("{{ .foo | bytes }}"),
			labels.FromStrings("foo", "3 kB"),
			1656353124120000000,
			[]byte("3000"),
			labels.FromStrings("foo", "3 kB"),
			[]byte("1"),
		},
		{
			"bytes 2",
			newMustLineFormatter("{{ .foo | bytes }}"),
			labels.FromStrings("foo", "3MB"),
			1656353124120000000,
			[]byte("3e+06"),
			labels.FromStrings("foo", "3MB"),
			[]byte("1"),
		},
		{
			"duration 1",
			newMustLineFormatter("{{ .foo | duration }}"),
			labels.FromStrings("foo", "3ms"),
			1656353124120000000,
			[]byte("0.003"),
			labels.FromStrings("foo", "3ms"),
			[]byte("1"),
		},
		{
			"duration 2",
			newMustLineFormatter("{{ .foo | duration_seconds }}"),
			labels.FromStrings("foo", "3m10s"),
			1656353124120000000,
			[]byte("190"),
			labels.FromStrings("foo", "3m10s"),
			[]byte("1"),
		},
		{
			"toDateInZone",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			1656353124120000000,
			[]byte("1678411960340"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			[]byte("1"),
		},
		{
			"unixEpochMillis",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			1656353124120000000,
			[]byte("1678411960340"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			[]byte("1"),
		},
		{
			"unixEpochNanos",
			newMustLineFormatter("{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochNanos }}"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			1656353124120000000,
			[]byte("1678411960340485723"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z"),
			[]byte("1"),
		},
		{
			"base64encode",
			newMustLineFormatter("{{ .foo | b64enc }}"),
			labels.FromStrings("foo", "i'm a string, encode me!"),
			1656353124120000000,
			[]byte("aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"),
			labels.FromStrings("foo", "i'm a string, encode me!"),
			[]byte("1"),
		},
		{
			"base64decode",
			newMustLineFormatter("{{ .foo | b64dec }}"),
			labels.FromStrings("foo", "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"),
			1656353124120000000,
			[]byte("i'm a string, encode me!"),
			labels.FromStrings("foo", "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh"),
			[]byte("1"),
		},
		{
			"alignLeft",
			newMustLineFormatter("{{ alignLeft 4 .foo }}"),
			labels.FromStrings("foo", "hello"),
			1656353124120000000,
			[]byte("hell"),
			labels.FromStrings("foo", "hello"),
			[]byte("1"),
		},
		{
			"alignRight",
			newMustLineFormatter("{{ alignRight 4 .foo }}"),
			labels.FromStrings("foo", "hello"),
			1656353124120000000,
			[]byte("ello"),
			labels.FromStrings("foo", "hello"),
			[]byte("1"),
		},
		{
			"simple key template",
			newMustLineFormatter("{{.foo}}"),
			labels.FromStrings("foo", "bar"),
			0,
			[]byte("bar"),
			labels.FromStrings("foo", "bar"),
			nil,
		},
		{
			"simple key template with space",
			newMustLineFormatter("{{.foo}}  "),
			labels.FromStrings("foo", "bar"),
			0,
			[]byte("bar  "),
			labels.FromStrings("foo", "bar"),
			nil,
		},
		{
			"simple key template with missing key",
			newMustLineFormatter("{{.missing}}"),
			labels.FromStrings("foo", "bar"),
			0,
			[]byte{},
			labels.FromStrings("foo", "bar"),
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	// These variables are used to test unixToTime.
	// They resolve to the local timezone so it works everywhere.
	epochDay19503 := time.Unix(19503*86400, 0)
	epochSeconds1679577215 := time.Unix(1679577215, 0)
	epochMilliseconds1257894000000 := time.UnixMilli(1257894000000)
	epochMicroseconds1673798889902000 := time.UnixMicro(1673798889902000)
	epochNanoseconds1000000000000000000 := time.Unix(0, 1000000000000000000)

	tests := []struct {
		name  string
		fmter *LabelsFormatter

		in   labels.Labels
		want labels.Labels
	}{
		{
			"rename label",
			mustNewLabelsFormatter([]LabelFmt{
				NewRenameLabelFmt("baz", "foo"),
			}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("bar", "blop", "baz", "blip"),
		},
		{
			"rename and overwrite existing label",
			mustNewLabelsFormatter([]LabelFmt{
				NewRenameLabelFmt("bar", "foo"),
			}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("bar", "blip"),
		},
		{
			"combined with template",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", "{{.foo}} and {{.bar}}")}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip and blop", "bar", "blop"),
		},
		{
			"combined with template and rename",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo}} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("blip", "blip and blop", "bar", "blip"),
		},
		{
			"fn",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo | ToUpper }} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("blip", "BLIP and blop", "bar", "blip"),
		},
		{
			"math",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("status", "{{div .status 100 }}")}),
			labels.FromStrings("status", "200"),
			labels.FromStrings("status", "2"),
		},
		{
			"default",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", `{{.foo | default "-" }} and {{.bar}}`),
			}),
			labels.FromStrings("bar", "blop"),
			labels.FromStrings("blip", "- and blop", "bar", "blop"),
		},
		{
			"template error",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{replace \"test\" .foo}}")}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"__error__", "TemplateFormatErr",
				"__error_details__", "template: label:1:2: executing \"label\" at <replace>: wrong number of args for replace: want 3 got 2",
			),
		},
		{
			"line",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("line", "{{ __line__ }}")}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"line", "test line",
			),
		},
		{
			"timestamp",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("ts", "{{ __timestamp__ | date \"2006-01-02\" }}")}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"ts", "2022-08-26",
			),
		},
		{
			"timestamp_unix",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("ts", "{{ __timestamp__ | unixEpoch }}")}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"ts", "1661518453",
			),
		},
		{
			"count",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("count", `{{ __line__ | count "test" }}`)}),
			labels.FromStrings("foo", "blip", "bar", "blop"),
			labels.FromStrings("foo", "blip",
				"bar", "blop",
				"count", "1",
			),
		},
		{
			"count regex no matches",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("count", `{{ __line__ | count "notmatching.*" }}`)}),
			labels.EmptyLabels(),
			labels.FromStrings("count", "0"),
		},
		{
			"bytes 1",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | bytes }}")}),
			labels.FromStrings("foo", "3 kB", "bar", "blop"),
			labels.FromStrings("foo", "3 kB", "bar", "3000"),
		},
		{
			"bytes 2",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | bytes }}")}),
			labels.FromStrings("foo", "3MB", "bar", "blop"),
			labels.FromStrings("foo", "3MB", "bar", "3e+06"),
		},
		{
			"duration 1",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | duration }}")}),
			labels.FromStrings("foo", "3ms", "bar", "blop"),
			labels.FromStrings("foo", "3ms",
				"bar", "0.003",
			),
		},
		{
			"duration 2",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | duration }}")}),
			labels.FromStrings("foo", "3m10s", "bar", "blop"),
			labels.FromStrings("foo", "3m10s", "bar", "190"),
		},
		{
			"toDateInZone",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}")}),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "blop"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "1678411960340"),
		},
		{
			"unixEpochMillis",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochMillis }}")}),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "blop"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "1678411960340"),
		},
		{
			"unixEpochNanos",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | toDateInZone \"2006-01-02T15:04:05.999999999Z\" \"UTC\" | unixEpochNanos }}")}),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "blop"),
			labels.FromStrings("foo", "2023-03-10T01:32:40.340485723Z", "bar", "1678411960340485723"),
		},
		{
			"base64encode",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | b64enc }}")}),
			labels.FromStrings("foo", "i'm a string, encode me!", "bar", "blop"),
			labels.FromStrings("foo", "i'm a string, encode me!",
				"bar", "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh",
			),
		},
		{
			"base64decode",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("bar", "{{ .foo | b64dec }}")}),
			labels.FromStrings("foo", "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh", "bar", "blop"),
			labels.FromStrings("foo", "aSdtIGEgc3RyaW5nLCBlbmNvZGUgbWUh",
				"bar", "i'm a string, encode me!",
			),
		},
		{
			"unixToTime days",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", `{{ .bar | unixToTime }}`)}),
			labels.Labels{{Name: "foo", Value: ""}, {Name: "bar", Value: "19503"}},
			labels.Labels{
				{Name: "bar", Value: "19503"},
				{Name: "foo", Value: epochDay19503.String()},
			},
		},
		{
			"unixToTime seconds",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", `{{ .bar | unixToTime }}`)}),
			labels.Labels{{Name: "foo", Value: ""}, {Name: "bar", Value: "1679577215"}},
			labels.Labels{
				{Name: "bar", Value: "1679577215"},
				{Name: "foo", Value: epochSeconds1679577215.String()},
			},
		},
		{
			"unixToTime milliseconds",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", `{{ .bar | unixToTime }}`)}),
			labels.Labels{{Name: "foo", Value: ""}, {Name: "bar", Value: "1257894000000"}},
			labels.Labels{
				{Name: "bar", Value: "1257894000000"},
				{Name: "foo", Value: epochMilliseconds1257894000000.String()},
			},
		},
		{
			"unixToTime microseconds",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", `{{ .bar | unixToTime }}`)}),
			labels.Labels{{Name: "foo", Value: ""}, {Name: "bar", Value: "1673798889902000"}},
			labels.Labels{
				{Name: "bar", Value: "1673798889902000"},
				{Name: "foo", Value: epochMicroseconds1673798889902000.String()},
			},
		},
		{
			"unixToTime nanoseconds",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", `{{ .bar | unixToTime }}`)}),
			labels.Labels{{Name: "foo", Value: ""}, {Name: "bar", Value: "1000000000000000000"}},
			labels.Labels{
				{Name: "bar", Value: "1000000000000000000"},
				{Name: "foo", Value: epochNanoseconds1000000000000000000.String()},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBaseLabelsBuilder().ForLabels(tt.in, tt.in.Hash())
			builder.Reset()
			_, _ = tt.fmter.Process(1661518453244672570, []byte("test line"), builder)
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

func Test_AlignLeft(t *testing.T) {
	tests := []struct {
		s    string
		c    int
		want string
	}{
		{"Hello, 世界", -1, "Hello, 世界"},
		{"Hello, 世界", 0, ""},
		{"Hello, 世界", 1, "H"},
		{"Hello, 世界", 8, "Hello, 世"},
		{"Hello, 世界", 20, "Hello, 世界           "},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s%d", tt.s, tt.c), func(t *testing.T) {
			if got := alignLeft(tt.c, tt.s); got != tt.want {
				t.Errorf("alignLeft() = %q, want %q for %q with %v", got, tt.want, tt.s, tt.c)
			}
		})
	}
}

func Test_AlignRight(t *testing.T) {
	tests := []struct {
		s    string
		c    int
		want string
	}{
		{"Hello, 世界", -1, "Hello, 世界"},
		{"Hello, 世界", 0, ""},
		{"Hello, 世界", 1, "界"},
		{"Hello, 世界", 2, "世界"},
		{"Hello, 世界", 20, "           Hello, 世界"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s%d", tt.s, tt.c), func(t *testing.T) {
			if got := alignRight(tt.c, tt.s); got != tt.want {
				t.Errorf("alignRight() = %q, want %q for %q with %v", got, tt.want, tt.s, tt.c)
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
	decolorizer, _ := NewDecolorizer()
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
			result, _ := decolorizer.Process(0, tt.src, nil)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestInvalidUnixTimes(t *testing.T) {
	_, err := unixToTime("abc")
	require.Error(t, err)

	_, err = unixToTime("464")
	require.Error(t, err)
}

func TestMapPoolPanic(_ *testing.T) {
	wg := sync.WaitGroup{}
	wg.Add(1)
	wgFinished := sync.WaitGroup{}

	ls := labels.FromStrings("cluster", "us-central-0")
	builder := NewBaseLabelsBuilder().ForLabels(ls, ls.Hash())
	// this specific line format was part of the query that first alerted us to the panic caused by map pooling in the label/line formatter Process functions
	tmpl := `[1m{{if .level }}{{alignRight 5 .level}}{{else if .severity}}{{alignRight 5 .severity}}{{end}}[0m [90m[{{alignRight 10 .resources_service_instance_id}}{{if .attributes_thread_name}}/{{alignRight 20 .attributes_thread_name}}{{else if eq "java" .resources_telemetry_sdk_language }}                    {{end}}][0m [36m{{if .instrumentation_scope_name }}{{alignRight 40 .instrumentation_scope_name}}{{end}}[0m {{.body}} {{if .traceid}} [37m[3m[traceid={{.traceid}}]{{end}}`
	a := newMustLineFormatter(tmpl)
	a.Process(0,
		[]byte("logger=sqlstore.metrics traceID=XXXXXXXXXXXXXXXXXXXXXXXXXXXX t=2024-01-04T23:58:47.696779826Z level=debug msg=\"query finished\" status=success elapsedtime=1.523571ms sql=\"some SQL query\" error=null"),
		builder,
	)

	for i := 0; i < 100; i++ {
		wgFinished.Add(1)
		go func() {
			wg.Wait()
			a := newMustLineFormatter(tmpl)
			a.Process(0,
				[]byte("logger=sqlstore.metrics traceID=XXXXXXXXXXXXXXXXXXXXXXXXXXXX t=2024-01-04T23:58:47.696779826Z level=debug msg=\"query finished\" status=success elapsedtime=1.523571ms sql=\"some SQL query\" error=null"),
				builder,
			)
			wgFinished.Done()
		}()
	}
	for i := 0; i < 100; i++ {
		wgFinished.Add(1)
		j := i
		go func() {
			wg.Wait()
			m := smp.Get()
			for k, v := range m {
				m[k] = fmt.Sprintf("%s%d", v, j)
			}
			smp.Put(m)
			wgFinished.Done()
		}()
	}
	wg.Done()
	wgFinished.Wait()
}
