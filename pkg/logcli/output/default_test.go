package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

func TestDefaultOutput_Format(t *testing.T) {
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
			"2006-01-02T08:04:05Z {} \n",
		},
		"empty line with labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			len(someLabels.String()),
			"",
			"2006-01-02T08:04:05Z {type=\"test\"} \n",
		},
		"max labels length shorter than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"} Hello\n",
		},
		"max labels length longer than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			20,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"}        Hello\n",
		},
		"timezone option set to a Local one": {
			&LogOutputOptions{Timezone: time.FixedZone("test", 2*60*60), NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T10:04:05+02:00 {type=\"test\"} Hello\n",
		},
		"labels output disabled": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: true},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T08:04:05Z Hello\n",
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			writer := &bytes.Buffer{}
			out := &DefaultOutput{writer, testData.options}
			out.FormatAndPrintln(testData.timestamp, testData.lbls, testData.maxLabelsLen, testData.line)

			assert.Equal(t, testData.expected, writer.String())
		})
	}
}

func TestDefaultOutput_FormatLabelsPadding(t *testing.T) {
	t.Parallel()

	// Define a list of labels that - once formatted - have a different length
	labelsList := []loghttp.LabelSet{
		loghttp.LabelSet(map[string]string{
			"type": "test",
		}),
		loghttp.LabelSet(map[string]string{
			"type": "test",
			"foo":  "bar",
		}),
		loghttp.LabelSet(map[string]string{
			"type": "a-longer-test",
		}),
	}

	timestamp, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05+07:00")
	maxLabelsLen := findMaxLabelsLength(labelsList)
	options := &LogOutputOptions{Timezone: time.UTC, NoLabels: false}
	writer := &bytes.Buffer{}
	out := &DefaultOutput{writer, options}

	// Format the same log line with different labels
	formattedEntries := make([]string, 0, len(labelsList))
	for _, lbls := range labelsList {
		out.FormatAndPrintln(timestamp, lbls, maxLabelsLen, "XXX")
		formattedEntries = append(formattedEntries, writer.String())
		writer.Reset()
	}

	// Ensure the log line starts at the same position in each formatted output
	assert.Equal(t, len(labelsList), len(formattedEntries))

	expectedIndex := strings.Index(formattedEntries[0], "XXX")
	if expectedIndex <= 0 {
		assert.FailNowf(t, "Unexpected starting position for log line in the formatted output", "position: %d", expectedIndex)
	}

	for _, entry := range formattedEntries {
		assert.Equal(t, expectedIndex, strings.Index(entry, "XXX"))
	}
}

func TestColorForLabels(t *testing.T) {
	tests := map[string]struct {
		labels      loghttp.LabelSet
		otherLabels loghttp.LabelSet
		expected    bool
	}{

		"different labels": {
			loghttp.LabelSet(map[string]string{
				"type": "test",
				"app":  "loki",
			}),
			loghttp.LabelSet(map[string]string{
				"type": "test",
				"app":  "grafana-loki",
			}),
			false,
		},
		"same labels": {
			loghttp.LabelSet(map[string]string{
				"type": "test",
				"app":  "loki",
			}),
			loghttp.LabelSet(map[string]string{
				"type": "test",
				"app":  "loki",
			}),
			true,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			labelsColor := getColor(testData.labels.String())
			otherLablesColor := getColor(testData.otherLabels.String())
			assert.Equal(t, testData.expected, labelsColor.Equals(otherLablesColor))
		})
	}
}

func findMaxLabelsLength(labelsList []loghttp.LabelSet) int {
	maxLabelsLen := 0

	for _, lbls := range labelsList {
		length := len(lbls.String())
		if maxLabelsLen < length {
			maxLabelsLen = length
		}
	}

	return maxLabelsLen
}
