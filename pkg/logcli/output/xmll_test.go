package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/stretchr/testify/require"
)

func TestXMLLOutput_FormatAndPrintln(t *testing.T) {
	tests := []struct {
		name       string
		timestamp  time.Time
		labels     loghttp.LabelSet
		line       string
		noLabels   bool
		assertions func(t *testing.T, output string)
	}{
		{
			name:      "simple log with labels",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 123456789, time.UTC),
			labels: loghttp.LabelSet{
				"job":      "api",
				"instance": "localhost:3000",
			},
			line: "request processed",
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, "<entry")
				require.Contains(t, output, "timestamp=")
				require.Contains(t, output, "<line>request processed</line>")
				require.Contains(t, output, `<label name="job">api</label>`)
				require.Contains(t, output, `<label name="instance">localhost:3000</label>`)
				require.Contains(t, output, "</entry>")
			},
		},
		{
			name:      "log with special XML characters in line",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
			labels: loghttp.LabelSet{
				"app": "test",
			},
			line: "error: x < 5 & y > 10",
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, "<line>error: x &lt; 5 &amp; y &gt; 10</line>")
			},
		},
		{
			name:      "log with special XML characters in labels",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
			labels: loghttp.LabelSet{
				"error": `key="value" & something`,
			},
			line: "test log",
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, `<label name="error">key=&quot;value&quot; &amp; something</label>`)
			},
		},
		{
			name:      "log without labels",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
			labels: loghttp.LabelSet{
				"job": "api",
			},
			line:     "test message",
			noLabels: true,
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, "<line>test message</line>")
				require.NotContains(t, output, "<labels>")
			},
		},
		{
			name:      "empty log line",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
			labels: loghttp.LabelSet{
				"stream": "empty",
			},
			line: "",
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, "<line></line>")
			},
		},
		{
			name:      "log with quotes in line",
			timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
			labels:    loghttp.LabelSet{},
			line:      `message with "quotes" and 'apostrophes'`,
			assertions: func(t *testing.T, output string) {
				require.Contains(t, output, `message with &quot;quotes&quot; and &apos;apostrophes&apos;`)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			output := &XMLLOutput{
				w: &buf,
				options: &LogOutputOptions{
					NoLabels: tc.noLabels,
					Timezone: time.UTC,
				},
			}

			output.FormatAndPrintln(tc.timestamp, tc.labels, 0, tc.line)

			result := strings.TrimSpace(buf.String())
			tc.assertions(t, result)
		})
	}
}

func TestXMLLOutput_WithWriter(t *testing.T) {
	var originalBuf bytes.Buffer
	output := &XMLLOutput{
		w: &originalBuf,
		options: &LogOutputOptions{
			NoLabels: false,
			Timezone: time.UTC,
		},
	}

	var newBuf bytes.Buffer
	newOutput := output.WithWriter(&newBuf)

	// Verify it returns a new XMLLOutput
	require.IsType(t, &XMLLOutput{}, newOutput)

	// Verify the writer was set correctly
	typedOutput := newOutput.(*XMLLOutput)
	typedOutput.FormatAndPrintln(
		time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
		loghttp.LabelSet{"test": "value"},
		0,
		"test line",
	)

	// Check output went to new buffer, not original
	require.Empty(t, originalBuf.String())
	require.Contains(t, newBuf.String(), "test line")
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal text", "normal text"},
		{"<tag>", "&lt;tag&gt;"},
		{"a & b", "a &amp; b"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"'apostrophe'", "&apos;apostrophe&apos;"},
		{`all: <>&"'`, "all: &lt;&gt;&amp;&quot;&apos;"},
		{"multiple && ampersands", "multiple &amp;&amp; ampersands"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := escapeXML(tc.input)
			require.Equal(t, tc.expected, result)
		})
	}
}
