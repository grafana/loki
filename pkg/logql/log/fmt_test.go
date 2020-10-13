package log

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_lineFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *lineFormatter
		lbs   map[string]string

		want    []byte
		wantLbs map[string]string
	}{
		{
			"combining",
			newMustLineFormatter("foo{{.foo}}buzz{{  .bar  }}"),
			map[string]string{"foo": "blip", "bar": "blop"},
			[]byte("fooblipbuzzblop"),
			map[string]string{"foo": "blip", "bar": "blop"},
		},
		{
			"missing",
			newMustLineFormatter("foo {{.foo}}buzz{{  .bar  }}"),
			map[string]string{"bar": "blop"},
			[]byte("foo buzzblop"),
			map[string]string{"bar": "blop"},
		},
		{
			"function",
			newMustLineFormatter("foo {{.foo | ToUpper }} buzz{{  .bar  }}"),
			map[string]string{"foo": "blip", "bar": "blop"},
			[]byte("foo BLIP buzzblop"),
			map[string]string{"foo": "blip", "bar": "blop"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outLine, _ := tt.fmter.Process(nil, tt.lbs)
			require.Equal(t, tt.want, outLine)
			require.Equal(t, tt.wantLbs, tt.lbs)
		})
	}
}

func newMustLineFormatter(tmpl string) *lineFormatter {
	l, err := NewFormatter(tmpl)
	if err != nil {
		panic(err)
	}
	return l
}

func Test_labelsFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *labelsFormatter

		in   labels.Labels
		want labels.Labels
	}{
		{
			"combined with template",
			mustNewLabelsFormatter([]labelFmt{newTemplateLabelFmt("foo", "{{.foo}} and {{.bar}}")}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "foo", Value: "blip and blop"}, {Name: "bar", Value: "blop"}},
		},
		{
			"combined with template and rename",
			mustNewLabelsFormatter([]labelFmt{
				newTemplateLabelFmt("blip", "{{.foo}} and {{.bar}}"),
				newRenameLabelFmt("bar", "foo"),
			}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "blip", Value: "blip and blop"}, {Name: "bar", Value: "blip"}},
		},
		{
			"fn",
			mustNewLabelsFormatter([]labelFmt{
				newTemplateLabelFmt("blip", "{{.foo | ToUpper }} and {{.bar}}"),
				newRenameLabelFmt("bar", "foo"),
			}),
			labels.Labels{{Name: "foo", Value: "blip"}, {Name: "bar", Value: "blop"}},
			labels.Labels{{Name: "blip", Value: "BLIP and blop"}, {Name: "bar", Value: "blip"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.want)
			_, _ = tt.fmter.Process(nil, tt.in)
			require.Equal(t, tt.want, tt.in)
		})
	}
}

func mustNewLabelsFormatter(fmts []labelFmt) *labelsFormatter {
	lf, err := NewLabelsFormatter(fmts)
	if err != nil {
		panic(err)
	}
	return lf
}

func Test_validate(t *testing.T) {
	tests := []struct {
		name    string
		fmts    []labelFmt
		wantErr bool
	}{
		{"no dup", []labelFmt{newRenameLabelFmt("foo", "bar"), newRenameLabelFmt("bar", "foo")}, false},
		{"dup", []labelFmt{newRenameLabelFmt("foo", "bar"), newRenameLabelFmt("foo", "blip")}, true},
		{"no error", []labelFmt{newRenameLabelFmt(errorLabel, "bar")}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.fmts); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
