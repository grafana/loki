package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/pkg/loghttp"
)

// DefaultOutput provides logs and metadata in human readable format
type DefaultOutput struct {
	w       io.Writer
	options *LogOutputOptions
}

// Format a log entry in a human readable format
func (o *DefaultOutput) FormatAndPrintln(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) {
	timestamp := ts.In(o.options.Timezone).Format(time.RFC3339)
	line = strings.TrimSpace(line)

	if o.options.NoLabels {
		fmt.Fprintf(o.w, "%s %s\n", color.BlueString(timestamp), line)
		return
	}
	if o.options.ColoredOutput {
		labelsColor := getColor(lbls.String()).SprintFunc()
		fmt.Fprintf(o.w, "%s %s %s\n", color.BlueString(timestamp), labelsColor(padLabel(lbls, maxLabelsLen)), line)
	} else {
		fmt.Fprintf(o.w, "%s %s %s\n", color.BlueString(timestamp), color.RedString(padLabel(lbls, maxLabelsLen)), line)
	}

}

// add some padding after labels
func padLabel(ls loghttp.LabelSet, maxLabelsLen int) string {
	labels := ls.String()
	if len(labels) < maxLabelsLen {
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))
	}
	return labels
}
