package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/grafana/loki/pkg/loghttp"
)

// DefaultOutput provides logs and metadata in human readable format
type DefaultOutput struct {
	options *LogOutputOptions
}

// Format a log entry in a human readable format
func (o *DefaultOutput) Format(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) string {
	timestamp := ts.In(o.options.Timezone).Format(time.RFC3339)
	line = strings.TrimSpace(line)

	if o.options.NoLabels {
		return fmt.Sprintf("%s %s", color.BlueString(timestamp), line)
	}

	return fmt.Sprintf("%s %s %s", color.BlueString(timestamp), color.RedString(padLabel(lbls, maxLabelsLen)), line)
}

// add some padding after labels
func padLabel(ls loghttp.LabelSet, maxLabelsLen int) string {
	labels := ls.String()
	if len(labels) < maxLabelsLen {
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))
	}
	return labels
}
