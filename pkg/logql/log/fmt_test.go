package log

import (
	"fmt"
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_lineFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *LineFormatter
		lbs   labels.Labels

		want    []byte
		wantLbs labels.Labels
	}{
		{
			"combining",
			newMustLineFormatter("foo{{.foo}}buzz{{  .bar  }}"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("fooblipbuzzblop"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"Replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ Replace .bar "blop" "bar" -1 }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("fooblipbuzzbar"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"replace",
			newMustLineFormatter(`foo{{.foo}}buzz{{ .bar | replace "blop" "bar" }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("fooblipbuzzbar"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"title",
			newMustLineFormatter(`{{.foo | title }}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("Blip"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"substr and trunc",
			newMustLineFormatter(
				`{{.foo | substr 1 3 }} {{ .bar  | trunc 1 }} {{ .bar  | trunc 3 }}`,
			),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("li b blo"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"trim",
			newMustLineFormatter(
				`{{.foo | trim }} {{ .bar  | trimAll "op" }} {{ .bar  | trimPrefix "b" }} {{ .bar  | trimSuffix "p" }}`,
			),
			labels.Labels{{Name: "foo", Value: "  blip "}, {Name: "bar", Value: "blop"}},
			[]byte("blip bl lop blo"),
			labels.Labels{{Name: "foo", Value: "  blip "}, {Name: "bar", Value: "blop"}},
		},
		{
			"lower and upper",
			newMustLineFormatter(`{{.foo | lower }} {{ .bar  | upper }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("blip BLOP"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"repeat",
			newMustLineFormatter(`{{ "foo" | repeat 3 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("foofoofoo"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"indent",
			newMustLineFormatter(`{{ "foo\n bar" | indent 4 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("    foo\n     bar"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"nindent",
			newMustLineFormatter(`{{ "foo" | nindent 2 }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("\n  foo"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"contains",
			newMustLineFormatter(`{{ if  .foo | contains "p"}}yes{{end}}-{{ if  .foo | contains "z"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"hasPrefix",
			newMustLineFormatter(`{{ if  .foo | hasPrefix "BL" }}yes{{end}}-{{ if  .foo | hasPrefix "p"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"hasSuffix",
			newMustLineFormatter(`{{ if  .foo | hasSuffix "Ip" }}yes{{end}}-{{ if  .foo | hasSuffix "pw"}}no{{end}}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("yes-"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"regexReplaceAll",
			newMustLineFormatter(`{{ regexReplaceAll "(p)" .foo "t" }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("BLIt"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"regexReplaceAllLiteral",
			newMustLineFormatter(`{{ regexReplaceAllLiteral "(p)" .foo "${1}" }}`),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
			[]byte("BLI${1}"),
			labels.Labels{{Name: "foo", Value: "BLIp"}, {Name: "bar", Value: "blop"}},
		},
		{
			"err",
			newMustLineFormatter(`{{.foo Replace "foo"}}`),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			nil,
			labels.Labels{{Name: ErrorLabel, Value: errTemplateFormat}, {Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
		{
			"missing",
			newMustLineFormatter("foo {{.foo}}buzz{{  .bar  }}"),
			labels.Labels{{Name: "bar", Value: "blop"}},
			[]byte("foo buzzblop"),
			labels.Labels{{Name: "bar", Value: "blop"}},
		},
		{
			"function",
			newMustLineFormatter("foo {{.foo | ToUpper }} buzz{{  .bar  }}"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			[]byte("foo BLIP buzzblop"),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.lbs)
			sort.Sort(tt.wantLbs)
			builder := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			builder.Reset()
			outLine, _ := tt.fmter.Process(nil, builder)
			require.Equal(t, tt.want, outLine)
			require.Equal(t, tt.wantLbs, builder.Labels())
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewBaseLabelsBuilder().ForLabels(tt.in, tt.in.Hash())
			builder.Reset()
			_, _ = tt.fmter.Process(nil, builder)
			sort.Sort(tt.want)
			require.Equal(t, tt.want, builder.Labels())
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

func Test_validate(t *testing.T) {
	tests := []struct {
		name    string
		fmts    []LabelFmt
		wantErr bool
	}{
		{"no dup", []LabelFmt{NewRenameLabelFmt("foo", "bar"), NewRenameLabelFmt("bar", "foo")}, false},
		{"dup", []LabelFmt{NewRenameLabelFmt("foo", "bar"), NewRenameLabelFmt("foo", "blip")}, true},
		{"no error", []LabelFmt{NewRenameLabelFmt(ErrorLabel, "bar")}, true},
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
