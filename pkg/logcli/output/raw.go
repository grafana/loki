package output

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
)

// RawOutput prints logs in their original form, without any metadata
type RawOutput struct {
	options *LogOutputOptions
}

// Format a log entry as is
func (o *RawOutput) Format(ts time.Time, lbls *labels.Labels, maxLabelsLen int, line string) string {
	return line
}

// Print a log entry as is
func (o *RawOutput) Print(ts time.Time, lbls *labels.Labels, maxLabelsLen int, line string) {
	fmt.Println(o.Format(ts, lbls, maxLabelsLen, line))
}
