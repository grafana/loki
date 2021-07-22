package stages

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
	"github.com/google/go-cmp/cmp"
)

type inspector struct {
	writer    io.Writer
	formatter *formatter
}

func newInspector(writer io.Writer, disableFormatting bool) *inspector {
	f := &formatter{
		red:    color.New(color.FgRed),
		yellow: color.New(color.FgYellow),
		green:  color.New(color.FgGreen),
		bold:   color.New(color.Bold),
	}

	if disableFormatting {
		f.disable()
	}

	return &inspector{
		writer:    writer,
		formatter: f,
	}
}

type formatter struct {
	red    *color.Color
	yellow *color.Color
	green  *color.Color
	bold   *color.Color
}

func (f *formatter) disable() {
	f.red.DisableColor()
	f.yellow.DisableColor()
	f.green.DisableColor()
	f.bold.DisableColor()
}

func (i inspector) inspect(stageName string, before *Entry, after Entry) {
	if before == nil {
		fmt.Fprintln(i.writer, i.formatter.red.Sprintf("could not copy entry in '%s' stage; inspect aborted", stageName))
		return
	}

	r := diffReporter{
		formatter: i.formatter,
	}

	cmp.Equal(*before, after, cmp.Reporter(&r))

	diff := r.String()
	if strings.TrimSpace(diff) == "" {
		diff = i.formatter.red.Sprintf("none")
	}

	fmt.Fprintf(i.writer, "[inspect: %s stage]: %s\n", i.formatter.bold.Sprintf("%s", stageName), diff)
}

// diffReporter is a simple custom reporter that only records differences
// detected during comparison.
type diffReporter struct {
	path cmp.Path

	formatter *formatter

	diffs []string
}

func (r *diffReporter) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

func (r *diffReporter) Report(rs cmp.Result) {
	if rs.Equal() {
		return
	}

	vx, vy := r.path.Last().Values()

	// TODO(dannyk): try using go-cmp to filter this condition out with Equal(), but for now this just makes it work
	if fmt.Sprintf("%v", vx) == fmt.Sprintf("%v", vy) {
		return
	}

	change := vx.IsValid()
	addition := vy.IsValid()
	removal := change && !addition
	mod := addition && change

	var titleColor *color.Color
	switch {
	case mod:
		titleColor = r.formatter.yellow
	case removal:
		titleColor = r.formatter.red
	default:
		titleColor = r.formatter.green
	}

	r.diffs = append(r.diffs, titleColor.Sprintf("%#v:", r.path))

	if removal {
		r.diffs = append(r.diffs, r.formatter.red.Sprintf("\t-: %v", vx))
	}
	if mod {
		r.diffs = append(r.diffs, r.formatter.yellow.Sprintf("\t-: %v", vx))
	}
	if addition {
		r.diffs = append(r.diffs, r.formatter.green.Sprintf("\t+: %v", vy))
	}
}

func (r *diffReporter) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

func (r *diffReporter) String() string {
	return fmt.Sprintf("\n%s", strings.Join(r.diffs, "\n"))
}
