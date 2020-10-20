package log

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_lineFormatter_Format(t *testing.T) {
	tests := []struct {
		name  string
		fmter *LineFormatter
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

		in   Labels
		want Labels
	}{
		{
			"combined with template",
			mustNewLabelsFormatter([]LabelFmt{NewTemplateLabelFmt("foo", "{{.foo}} and {{.bar}}")}),
			map[string]string{"foo": "blip", "bar": "blop"},
			map[string]string{"foo": "blip and blop", "bar": "blop"},
		},
		{
			"combined with template and rename",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo}} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			map[string]string{"foo": "blip", "bar": "blop"},
			map[string]string{"blip": "blip and blop", "bar": "blip"},
		},
		{
			"fn",
			mustNewLabelsFormatter([]LabelFmt{
				NewTemplateLabelFmt("blip", "{{.foo | ToUpper }} and {{.bar}}"),
				NewRenameLabelFmt("bar", "foo"),
			}),
			map[string]string{"foo": "blip", "bar": "blop"},
			map[string]string{"blip": "BLIP and blop", "bar": "blip"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _ = tt.fmter.Process(nil, tt.in)
			require.Equal(t, tt.want, tt.in)
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
