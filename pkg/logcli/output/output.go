package output

import (
	"fmt"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
)

// LogOutput is the interface any output mode must implement
type LogOutput interface {
	Format(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) string
}

// LogOutputOptions defines options supported by LogOutput
type LogOutputOptions struct {
	Timezone *time.Location
	NoLabels bool
}

// NewLogOutput creates a log output based on the input mode and options
func NewLogOutput(mode string, options *LogOutputOptions) (LogOutput, error) {
	if options.Timezone == nil {
		options.Timezone = time.Local
	}

	switch mode {
	case "default":
		return &DefaultOutput{
			options: options,
		}, nil
	case "jsonl":
		return &JSONLOutput{
			options: options,
		}, nil
	case "raw":
		return &RawOutput{
			options: options,
		}, nil
	default:
		return nil, fmt.Errorf("unknown log output mode '%s'", mode)
	}
}
