package output

import (
	"time"

	"github.com/grafana/loki/pkg/loghttp"
)

// RawOutput prints logs in their original form, without any metadata
type RawOutput struct {
	options *LogOutputOptions
}

// Format a log entry as is
func (o *RawOutput) Format(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) string {
	return line
}
