package output

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"
)

// DefaultOutput provides logs and metadata in human readable format
type DefaultOutput struct {
	options *LogOutputOptions
}

// Format a log entry in a human readable format
func (o *DefaultOutput) Format(ts time.Time, lbls *labels.Labels, maxLabelsLen int, line string) string {
	timestamp := ts.In(o.options.Timezone).Format(time.RFC3339)
	line = strings.TrimSpace(line)

	if o.options.NoLabels {
		return fmt.Sprintf("%s %s", color.BlueString(timestamp), line)
	}

	return fmt.Sprintf("%s %s %s", color.BlueString(timestamp), color.RedString(padLabel(*lbls, maxLabelsLen)), line)
}

// Print a log entry in a human readable format
func (o *DefaultOutput) Print(ts time.Time, lbls *labels.Labels, maxLabelsLen int, line string) {
	fmt.Println(o.Format(ts, lbls, maxLabelsLen, line))
}

// add some padding after labels
func padLabel(ls labels.Labels, maxLabelsLen int) string {
	labels := ls.String()
	if len(labels) < maxLabelsLen {
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))
	}
	return labels
}
