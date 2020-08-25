package output

import (
	"fmt"
	"io"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
)

// RawOutput prints logs in their original form, without any metadata
type RawOutput struct {
	w       io.Writer
	options *LogOutputOptions
}

func NewRaw (writer io.Writer, options *LogOutputOptions) LogOutput {
	return &RawOutput{
		w: writer,
		options: options,
	}
}

// Format a log entry as is
func (o *RawOutput) FormatAndPrintln(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	fmt.Fprintln(o.w, line)
}
