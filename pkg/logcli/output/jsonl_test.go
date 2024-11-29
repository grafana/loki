package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

func TestJSONLOutput_Format(t *testing.T) {
	t.Parallel()

	timestamp, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05+07:00")
	emptyLabels := loghttp.LabelSet{}
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
		"empty line with no labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			emptyLabels,
			0,
			"",
			`{"labels":{},"line":"","timestamp":"2006-01-02T08:04:05Z"}` + "\n",
		},
		"empty line with labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			len(someLabels.String()),
			"",
			`{"labels":{"type":"test"},"line":"","timestamp":"2006-01-02T08:04:05Z"}` + "\n",
		},
		"timezone option set to a Local one": {
			&LogOutputOptions{Timezone: time.FixedZone("test", 2*60*60), NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello",
			`{"labels":{"type":"test"},"line":"Hello","timestamp":"2006-01-02T10:04:05+02:00"}` + "\n",
		},
		"labels output disabled": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: true},
			timestamp,
			someLabels,
			0,
			"Hello",
			`{"line":"Hello","timestamp":"2006-01-02T08:04:05Z"}` + "\n",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			writer := &bytes.Buffer{}
			out := &JSONLOutput{writer, testData.options}
			out.FormatAndPrintln(testData.timestamp, testData.lbls, testData.maxLabelsLen, testData.line)

			actual := writer.String()
			assert.Equal(t, testData.expected, actual)
			assert.NoError(t, isValidJSON(actual))
		})
	}
}

func isValidJSON(s string) error {
	var data map[string]interface{}

	return json.Unmarshal([]byte(s), &data)
}
