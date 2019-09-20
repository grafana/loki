package output

import (
	"strings"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/stretchr/testify/assert"
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
			"2006-01-02T08:04:05Z {} ",
		},
		"empty line with labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			len(someLabels.String()),
			"",
			"2006-01-02T08:04:05Z {type=\"test\"} ",
		},
		"max labels length shorter than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"} Hello",
		},
		"max labels length longer than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			someLabels,
			20,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"}        Hello",
		},
		"timezone option set to a Local one": {
			&LogOutputOptions{Timezone: time.FixedZone("test", 2*60*60), NoLabels: false},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T10:04:05+02:00 {type=\"test\"} Hello",
		},
		"labels output disabled": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: true},
			timestamp,
			someLabels,
			0,
			"Hello",
			"2006-01-02T08:04:05Z Hello",
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			out := &DefaultOutput{testData.options}
			actual := out.Format(testData.timestamp, testData.lbls, testData.maxLabelsLen, testData.line)

			assert.Equal(t, testData.expected, actual)
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
	out := &DefaultOutput{options}

	// Format the same log line with different labels
	formattedEntries := make([]string, 0, len(labelsList))
	for _, lbls := range labelsList {
		formattedEntries = append(formattedEntries, out.Format(timestamp, lbls, maxLabelsLen, "XXX"))
	}

	// Ensure the log line starts at the same position in each formatted output
	assert.Equal(t, len(formattedEntries), len(labelsList))

	expectedIndex := strings.Index(formattedEntries[0], "XXX")
	if expectedIndex <= 0 {
		assert.FailNowf(t, "Unexpected starting position for log line in the formatted output", "position: %d", expectedIndex)
	}

	for _, entry := range formattedEntries {
		assert.Equal(t, expectedIndex, strings.Index(entry, "XXX"))
	}
}

func findMaxLabelsLength(labelsList []loghttp.LabelSet) int {
	maxLabelsLen := 0

	for _, lbls := range labelsList {
		len := len(lbls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	return maxLabelsLen
}
