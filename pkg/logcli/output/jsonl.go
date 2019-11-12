package output

import (
	"encoding/json"
	"log"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
)

// JSONLOutput prints logs and metadata as JSON Lines, suitable for scripts
type JSONLOutput struct {
	options *LogOutputOptions
}

// Format a log entry as json line
func (o *JSONLOutput) Format(ts time.Time, lbls loghttp.LabelSet, maxLabelsLen int, line string) string {
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
		log.Fatalf("error marshalling entry: %s", err)
	}

	return string(out)
}
