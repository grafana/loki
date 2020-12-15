package log

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"text/template/parse"
)

var (
	_ Stage = &LineFormatter{}
	_ Stage = &LabelsFormatter{}

	// Available map of functions for the text template engine.
	functionMap = template.FuncMap{
		// olds function deprecated.
		"ToLower":    strings.ToLower,
		"ToUpper":    strings.ToUpper,
		"Replace":    strings.Replace,
		"Trim":       strings.Trim,
		"TrimLeft":   strings.TrimLeft,
		"TrimRight":  strings.TrimRight,
		"TrimPrefix": strings.TrimPrefix,
		"TrimSuffix": strings.TrimSuffix,
		"TrimSpace":  strings.TrimSpace,

		// New function ported from https://github.com/Masterminds/sprig/
		// Those function takes the string as the last parameter, allowing pipe chaining.
		// Example: .mylabel | lower | substring 0 5
		"lower":      strings.ToLower,
		"upper":      strings.ToUpper,
		"title":      strings.Title,
		"trunc":      trunc,
		"substr":     substring,
		"contains":   contains,
		"hasPrefix":  hasPrefix,
		"hasSuffix":  hasSuffix,
		"indent":     indent,
		"nindent":    nindent,
		"replace":    replace,
		"repeat":     repeat,
		"trim":       strings.TrimSpace,
		"trimAll":    trimAll,
		"trimSuffix": trimSuffix,
		"trimPrefix": trimPrefix,

		// regex functions
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

func (lf *LineFormatter) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	lf.buf.Reset()
	if err := lf.Template.Execute(lf.buf, lbs.Labels().Map()); err != nil {
		lbs.SetErr(errTemplateFormat)
		return line, true
	}
	// todo(cyriltovena): we might want to reuse the input line or a bytes buffer.
	res := make([]byte, len(lf.buf.Bytes()))
	copy(res, lf.buf.Bytes())
	return res, true
}

func (lf *LineFormatter) RequiredLabelNames() []string {
	return uniqueString(listNodeFields(lf.Root))
}

func listNodeFields(node parse.Node) []string {
	var res []string
	if node.Type() == parse.NodeAction {
		res = append(res, listNodeFieldsFromPipe(node.(*parse.ActionNode).Pipe)...)
	}
	res = append(res, listNodeFieldsFromBranch(node)...)
	if ln, ok := node.(*parse.ListNode); ok {
		for _, n := range ln.Nodes {
			res = append(res, listNodeFields(n)...)
		}
	}
	return res
}

func listNodeFieldsFromBranch(node parse.Node) []string {
	var res []string
	var b parse.BranchNode
	switch node.Type() {
	case parse.NodeIf:
		b = node.(*parse.IfNode).BranchNode
	case parse.NodeWith:
		b = node.(*parse.WithNode).BranchNode
	case parse.NodeRange:
		b = node.(*parse.RangeNode).BranchNode
	default:
		return res
	}
	if b.Pipe != nil {
		res = append(res, listNodeFieldsFromPipe(b.Pipe)...)
	}
	if b.List != nil {
		res = append(res, listNodeFields(b.List)...)
	}
	if b.ElseList != nil {
		res = append(res, listNodeFields(b.ElseList)...)
	}
	return res
}

func listNodeFieldsFromPipe(p *parse.PipeNode) []string {
	var res []string
	for _, c := range p.Cmds {
		for _, a := range c.Args {
			if f, ok := a.(*parse.FieldNode); ok {
				res = append(res, f.Ident...)
			}
		}
	}
	return res
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
		if data == nil {
			data = lbs.Labels().Map()
		}
		if err := f.tmpl.Execute(lf.buf, data); err != nil {
			lbs.SetErr(errTemplateFormat)
			continue
		}
		lbs.Set(f.Name, lf.buf.String())
	}
	return l, true
}

func (lf *LabelsFormatter) RequiredLabelNames() []string {
	var names []string
	return names
}

func trunc(c int, s string) string {
	runes := []rune(s)
	l := len(runes)
	if c < 0 && l+c > 0 {
		return string(runes[l+c:])
	}
	if c >= 0 && l > c {
		return string(runes[:c])
	}
	return s
}

// substring creates a substring of the given string.
//
// If start is < 0, this calls string[:end].
//
// If start is >= 0 and end < 0 or end bigger than s length, this calls string[start:]
//
// Otherwise, this calls string[start, end].
func substring(start, end int, s string) string {
	runes := []rune(s)
	l := len(runes)
	if end > l {
		end = l
	}
	if start > l {
		start = l
	}
	if start < 0 {
		if end < 0 {
			return ""
		}
		return string(runes[:end])
	}
	if end < 0 {
		return string(runes[start:])
	}
	if start > end {
		return ""
	}
	return string(runes[start:end])
}

func contains(substr string, str string) bool  { return strings.Contains(str, substr) }
func hasPrefix(substr string, str string) bool { return strings.HasPrefix(str, substr) }
func hasSuffix(substr string, str string) bool { return strings.HasSuffix(str, substr) }
func repeat(count int, str string) string      { return strings.Repeat(str, count) }
func replace(old, new, src string) string      { return strings.Replace(src, old, new, -1) }
func trimAll(a, b string) string               { return strings.Trim(b, a) }
func trimSuffix(a, b string) string            { return strings.TrimSuffix(b, a) }
func trimPrefix(a, b string) string            { return strings.TrimPrefix(b, a) }
func indent(spaces int, v string) string {
	pad := strings.Repeat(" ", spaces)
	return pad + strings.Replace(v, "\n", "\n"+pad, -1)
}

func nindent(spaces int, v string) string { return "\n" + indent(spaces, v) }
