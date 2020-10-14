package log

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

var (
	_ Stage = &lineFormatter{}
	_ Stage = &labelsFormatter{}

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

func NewFormatter(tmpl string) (*lineFormatter, error) {
	t, err := template.New("line").Option("missingkey=zero").Funcs(functionMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("invalid line template: %s", err)
	}
	return &lineFormatter{
		Template: t,
		buf:      bytes.NewBuffer(make([]byte, 4096)),
	}, nil
}

func (lf *lineFormatter) Process(_ []byte, lbs Labels) ([]byte, bool) {
	lf.buf.Reset()
	// todo(cyriltovena) handle error
	_ = lf.Template.Execute(lf.buf, lbs)
	// todo we might want to reuse the input line.
	res := make([]byte, len(lf.buf.Bytes()))
	copy(res, lf.buf.Bytes())
	return res, true
}

type LabelFmt struct {
	Name  string
	Value string

	Rename bool
}

func NewRenameLabelFmt(dst, target string) LabelFmt {
	return LabelFmt{
		Name:   dst,
		Rename: true,
		Value:  target,
	}
}
func NewTemplateLabelFmt(dst, template string) LabelFmt {
	return LabelFmt{
		Name:   dst,
		Rename: false,
		Value:  template,
	}
}

type labelFormatter struct {
	tmpl *template.Template
	LabelFmt
}

type labelsFormatter struct {
	formats []labelFormatter
	buf     *bytes.Buffer
}

func NewLabelsFormatter(fmts []LabelFmt) (*labelsFormatter, error) {
	if err := validate(fmts); err != nil {
		return nil, err
	}
	formats := make([]labelFormatter, 0, len(fmts))
	for _, fm := range fmts {
		toAdd := labelFormatter{LabelFmt: fm}
		if !fm.Rename {
			t, err := template.New("label").Option("missingkey=zero").Funcs(functionMap).Parse(fm.Value)
			if err != nil {
				return nil, fmt.Errorf("invalid template for label '%s': %s", fm.Name, err)
			}
			toAdd.tmpl = t
		}
		formats = append(formats, toAdd)
	}
	return &labelsFormatter{
		formats: formats,
		buf:     bytes.NewBuffer(make([]byte, 1024)),
	}, nil
}

func validate(fmts []LabelFmt) error {
	// it would be too confusing to rename and change the same label value.
	// To avoid confusion we allow to have a label name only once per stage.
	uniqueLabelName := map[string]struct{}{}
	for _, f := range fmts {
		if f.Name == ErrorLabel {
			return fmt.Errorf("%s cannot be formatted", f.Name)
		}
		if _, ok := uniqueLabelName[f.Name]; ok {
			return fmt.Errorf("multiple label name '%s' not allowed in a single format operation", f.Name)
		}
		uniqueLabelName[f.Name] = struct{}{}
	}
	return nil
}

func (lf *labelsFormatter) Process(l []byte, lbs Labels) ([]byte, bool) {
	for _, f := range lf.formats {
		if f.Rename {
			lbs[f.Name] = lbs[f.Value]
			delete(lbs, f.Value)
			continue
		}
		lf.buf.Reset()
		//todo (cyriltovena): handle error
		_ = f.tmpl.Execute(lf.buf, lbs)
		lbs[f.Name] = lf.buf.String()
	}
	return l, true
}
