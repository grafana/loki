package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

func TestRawOutput_Format(t *testing.T) {
	t.Parallel()

	timestamp, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05+07:00")
	someLabels := loghttp.LabelSet(map[string]string{
		"type": "test",
	})

	tests := map[string]struct {
		options      *LogOutputOptions
		timestamp    time.Time
		lbls         loghttp.LabelSet
		maxLabelsLen int
		line         string
		expected     string
	}{
		"empty line": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"",
			"\n",
		},
		"non empty line": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello world",
			"Hello world\n",
		},
		"line with single newline at the end": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello world\n",
			"Hello world\n",
		},
		"line with multiple newlines at the end": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello world\n\n\n",
			"Hello world\n\n\n",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			writer := &bytes.Buffer{}
			out := &RawOutput{writer, testData.options}
			out.FormatAndPrintln(testData.timestamp, testData.lbls, testData.maxLabelsLen, testData.line)

			assert.Equal(t, testData.expected, writer.String())
		})
	}
}
