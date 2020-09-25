package logql

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
			outLine, outLbs := tt.fmter.Format(nil, tt.lbs)
			require.Equal(t, tt.want, outLine)
			sort.Sort(tt.wantLbs)
			sort.Sort(outLbs)
			require.Equal(t, tt.wantLbs, outLbs)
		})
	}
}

func newMustLineFormatter(tmpl string) *lineFormatter {
	l, err := newLineFormatter(tmpl)
	if err != nil {
		panic(err)
	}
	return l
}

func Test_labelsFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *labelsFormatter
		in    labels.Labels
		want  labels.Labels
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
			out := tt.fmter.Format(tt.in)
			require.Equal(t, tt.want, out)
		})
	}
}

func mustNewLabelsFormatter(fmts []labelFmt) *labelsFormatter {
	lf, err := newLabelsFormatter(fmts)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.fmts); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
