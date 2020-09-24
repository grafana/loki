package logql

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	functionMap = template.FuncMap{
		"ToLower":    strings.ToLower,
		"ToUpper":    strings.ToUpper,
		"Replace":    strings.Replace,
		"Trim":       strings.Trim,
		"TrimLeft":   strings.TrimLeft,
		"TrimRight":  strings.TrimRight,
		"TrimPrefix": strings.TrimPrefix,
		"TrimSuffix": strings.TrimSuffix,
		"TrimSpace":  strings.TrimSpace,
		"regexReplaceAll": func(regex string, s string, repl string) string {
			r := regexp.MustCompile(regex)
			return r.ReplaceAllString(s, repl)
		},
		"regexReplaceAllLiteral": func(regex string, s string, repl string) string {
			r := regexp.MustCompile(regex)
			return r.ReplaceAllLiteralString(s, repl)
		},
	}
)

type lineFormatter struct {
	*template.Template
	buf *bytes.Buffer
}

func newLineFormatter(tmpl string) (*lineFormatter, error) {
	t, err := template.New(OpFmtLine).Option("missingkey=zero").Funcs(functionMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("invalid line template: %s", err)
	}
	return &lineFormatter{
		Template: t,
		buf:      bytes.NewBuffer(make([]byte, 4096)),
	}, nil
}

func (lf *lineFormatter) Format(_ []byte, lbs labels.Labels) ([]byte, labels.Labels) {
	lf.buf.Reset()
	// todo(cyriltovena) handle error
	_ = lf.Template.Execute(lf.buf, lbs.Map())
	// todo we might want to reuse the input line.
	res := make([]byte, len(lf.buf.Bytes()))
	copy(res, lf.buf.Bytes())
	return res, lbs
}

type labelFmt struct {
	name string

	value  string
	rename bool
}

func newRenameLabelFmt(dst, target string) labelFmt {
	return labelFmt{
		name:   dst,
		rename: true,
		value:  target,
	}
}
func newTemplateLabelFmt(dst, template string) labelFmt {
	return labelFmt{
		name:   dst,
		rename: false,
		value:  template,
	}
}

type labelFormatter struct {
	*template.Template
	labelFmt
}

type labelsFormatter struct {
	formats []labelFormatter
	builder *labels.Builder
	buf     *bytes.Buffer
}

func newLabelsFormatter(fmts []labelFmt) (*labelsFormatter, error) {
	if err := validate(fmts); err != nil {
		return nil, err
	}
	formats := make([]labelFormatter, 0, len(fmts))
	for _, fm := range fmts {
		toAdd := labelFormatter{labelFmt: fm}
		if !fm.rename {
			t, err := template.New(OpFmtLabel).Option("missingkey=zero").Funcs(functionMap).Parse(fm.value)
			if err != nil {
				return nil, fmt.Errorf("invalid template for label '%s': %s", fm.name, err)
			}
			toAdd.Template = t
		}
		formats = append(formats, toAdd)
	}
	return &labelsFormatter{
		formats: formats,
		builder: labels.NewBuilder(nil),
		buf:     bytes.NewBuffer(make([]byte, 1024)),
	}, nil
}

func validate(fmts []labelFmt) error {
	// it would be too confusing to rename and change the same label value.
	// To avoid confusion we allow to have a label name only once per stage.
	uniqueLabelName := map[string]struct{}{}
	for _, f := range fmts {
		if _, ok := uniqueLabelName[f.name]; ok {
			return fmt.Errorf("multiple label name '%s' not allowed in a single format operation", f.name)
		}
		uniqueLabelName[f.name] = struct{}{}
	}
	return nil
}

func (lf *labelsFormatter) Format(lbs labels.Labels) labels.Labels {
	lf.builder.Reset(lbs)
	for _, f := range lf.formats {
		if f.rename {
			lf.builder.Set(f.name, lbs.Get(f.value))
			lf.builder.Del(f.value)
			continue
		}
		lf.buf.Reset()
		//todo (cyriltovena): handle error
		_ = f.Template.Execute(lf.buf, lbs.Map())
		lf.builder.Set(f.name, lf.buf.String())
	}
	return lf.builder.Labels()
}
