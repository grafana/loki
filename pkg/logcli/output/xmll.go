package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// XMLLOutput prints logs and metadata as XML Lines, suitable for scripts
type XMLLOutput struct {
	w       io.Writer
	options *LogOutputOptions
}

// escapeXML escapes special XML characters
func escapeXML(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

// FormatAndPrintln formats a log entry as XML Line
func (o *XMLLOutput) FormatAndPrintln(ts time.Time, lbls loghttp.LabelSet, _ int, line string) {
	var sb strings.Builder

	// Write opening entry tag with timestamp
	sb.WriteString("<entry")
	sb.WriteString(fmt.Sprintf(" timestamp=\"%s\"", escapeXML(ts.In(o.options.Timezone).Format(time.RFC3339Nano))))
	sb.WriteString(">")

	// Write log line content
	sb.WriteString("<line>")
	sb.WriteString(escapeXML(line))
	sb.WriteString("</line>")

	// Write labels if not suppressed (LabelSet is map[string]string)
	if !o.options.NoLabels && len(lbls) > 0 {
		sb.WriteString("<labels>")
		// Iterate over map entries
		for name, value := range lbls {
			sb.WriteString(fmt.Sprintf(
				"<label name=\"%s\">%s</label>",
				escapeXML(name),
				escapeXML(value),
			))
		}
		sb.WriteString("</labels>")
	}

	// Write closing entry tag
	sb.WriteString("</entry>")

	fmt.Fprintln(o.w, sb.String())
}

// WithWriter returns a copy of the LogOutput with the writer set to the given writer
func (o XMLLOutput) WithWriter(w io.Writer) LogOutput {
	return &XMLLOutput{
		w:       w,
		options: o.options,
	}
}
