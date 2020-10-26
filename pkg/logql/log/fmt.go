package log

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

var (
	_ Stage = &LineFormatter{}
	_ Stage = &LabelsFormatter{}

	// Available map of functions for the text template engine.
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

type LineFormatter struct {
	*template.Template
	buf *bytes.Buffer
}

// NewFormatter creates a new log line formatter from a given text template.
func NewFormatter(tmpl string) (*LineFormatter, error) {
	t, err := template.New("line").Option("missingkey=zero").Funcs(functionMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("invalid line template: %s", err)
	}
	return &LineFormatter{
		Template: t,
		buf:      bytes.NewBuffer(make([]byte, 4096)),
	}, nil
}

func (lf *LineFormatter) Process(_ []byte, lbs *LabelsBuilder) ([]byte, bool) {
	lf.buf.Reset()
	// todo(cyriltovena): handle error
	_ = lf.Template.Execute(lf.buf, lbs.Labels().Map())
	// todo(cyriltovena): we might want to reuse the input line or a bytes buffer.
	res := make([]byte, len(lf.buf.Bytes()))
	copy(res, lf.buf.Bytes())
	return res, true
}

// LabelFmt is a configuration struct for formatting a label.
type LabelFmt struct {
	Name  string
	Value string

	Rename bool
}

// NewRenameLabelFmt creates a configuration to rename a label.
func NewRenameLabelFmt(dst, target string) LabelFmt {
	return LabelFmt{
		Name:   dst,
		Rename: true,
		Value:  target,
	}
}

// NewTemplateLabelFmt creates a configuration to format a label using text template.
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

type LabelsFormatter struct {
	formats []labelFormatter
	buf     *bytes.Buffer
}

// NewLabelsFormatter creates a new formatter that can format multiple labels at once.
// Either by renaming or using text template.
// It is not allowed to reformat the same label twice within the same formatter.
func NewLabelsFormatter(fmts []LabelFmt) (*LabelsFormatter, error) {
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
	return &LabelsFormatter{
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

func (lf *LabelsFormatter) Process(l []byte, lbs *LabelsBuilder) ([]byte, bool) {
	var data interface{}
	for _, f := range lf.formats {
		if f.Rename {
			v, ok := lbs.Get(f.Value)
			if ok {
				lbs.Set(f.Name, v)
				lbs.Del(f.Value)
			}
			continue
		}
		lf.buf.Reset()
		//todo (cyriltovena): handle error
		if data == nil {
			data = lbs.Labels().Map()
		}
		_ = f.tmpl.Execute(lf.buf, data)
		lbs.Set(f.Name, lf.buf.String())
	}
	return l, true
}
