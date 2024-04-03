package output

import (
	"fmt"
	"hash/fnv"
	"io"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// Blue color is excluded since we are already printing timestamp
// in blue color
var colorList = []*color.Color{
	color.New(color.FgHiCyan),
	color.New(color.FgCyan),
	color.New(color.FgHiGreen),
	color.New(color.FgGreen),
	color.New(color.FgHiMagenta),
	color.New(color.FgMagenta),
	color.New(color.FgHiYellow),
	color.New(color.FgYellow),
	color.New(color.FgHiRed),
	color.New(color.FgRed),
}

// LogOutput is the interface any output mode must implement
type LogOutput interface {
	FormatAndPrintln(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string)
	WithWriter(w io.Writer) LogOutput
}

// LogOutputOptions defines options supported by LogOutput
type LogOutputOptions struct {
	Timezone      *time.Location
	NoLabels      bool
	ColoredOutput bool
}

// NewLogOutput creates a log output based on the input mode and options
func NewLogOutput(w io.Writer, mode string, options *LogOutputOptions) (LogOutput, error) {
	if options.Timezone == nil {
		options.Timezone = time.Local
	}

	switch mode {
	case "default":
		return &DefaultOutput{
			w:       w,
			options: options,
		}, nil
	case "jsonl":
		return &JSONLOutput{
			w:       w,
			options: options,
		}, nil
	case "raw":
		return &RawOutput{
			w:       w,
			options: options,
		}, nil
	default:
		return nil, fmt.Errorf("unknown log output mode '%s'", mode)
	}
}

func getColor(labels string) *color.Color {
	hash := fnv.New32()
	_, _ = hash.Write([]byte(labels))
	id := hash.Sum32() % uint32(len(colorList))
	color := colorList[id]
	return color
}
