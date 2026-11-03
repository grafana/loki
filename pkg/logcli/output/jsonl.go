package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// JSONLOutput prints logs and metadata as JSON Lines, suitable for scripts
type JSONLOutput struct {
	w       io.Writer
	options *LogOutputOptions
}

// Format a log entry as json line
func (o *JSONLOutput) FormatAndPrintln(ts time.Time, lbls loghttp.LabelSet, _ int, line string) error {
	entry := map[string]interface{}{
		"timestamp": ts.In(o.options.Timezone),
		"line":      line,
	}

	// Labels are optional
	if !o.options.NoLabels {
		entry["labels"] = lbls
	}

	out, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("error marshalling entry: %w", err)
	}

	_, err = fmt.Fprintln(o.w, string(out))
	return err
}

// WithWriter returns a copy of the LogOutput with the writer set to the given writer
func (o JSONLOutput) WithWriter(w io.Writer) LogOutput {
	return &JSONLOutput{
		w:       w,
		options: o.options,
	}
}
