package output

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
)

func TestDefaultOutput_Format(t *testing.T) {
	t.Parallel()

	timestamp, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05+07:00")
	emptyLabels := labels.New()
	someLabels := labels.New(labels.Label{Name: "type", Value: "test"})

	tests := map[string]struct {
		options      *LogOutputOptions
		timestamp    time.Time
		lbls         *labels.Labels
		maxLabelsLen int
		line         string
		expected     string
	}{
		"empty line with no labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			&emptyLabels,
			0,
			"",
			"2006-01-02T08:04:05Z {} ",
		},
		"empty line with labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			&someLabels,
			len(someLabels.String()),
			"",
			"2006-01-02T08:04:05Z {type=\"test\"} ",
		},
		"max labels length shorter than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			&someLabels,
			0,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"} Hello",
		},
		"max labels length longer than input labels": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: false},
			timestamp,
			&someLabels,
			20,
			"Hello",
			"2006-01-02T08:04:05Z {type=\"test\"}        Hello",
		},
		"timezone option set to a Local one": {
			&LogOutputOptions{Timezone: time.FixedZone("test", 2*60*60), NoLabels: false},
			timestamp,
			&someLabels,
			0,
			"Hello",
			"2006-01-02T10:04:05+02:00 {type=\"test\"} Hello",
		},
		"labels output disabled": {
			&LogOutputOptions{Timezone: time.UTC, NoLabels: true},
			timestamp,
			&someLabels,
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
