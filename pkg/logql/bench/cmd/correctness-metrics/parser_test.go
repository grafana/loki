package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTestName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *testLabels
		wantErr  bool
	}{
		{
			name:  "metric query",
			input: "TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD",
			expected: &testLabels{
				Suite:     "fast",
				QueryFile: "basic",
				Kind:      "metric",
				Direction: "FORWARD",
			},
		},
		{
			name:  "log query backward",
			input: "TestRemoteStorageEquality/regression/agg.yaml:12/kind=log/direction=BACKWARD",
			expected: &testLabels{
				Suite:     "regression",
				QueryFile: "agg",
				Kind:      "log",
				Direction: "BACKWARD",
			},
		},
		{
			name:  "yml extension",
			input: "TestRemoteStorageEquality/exhaustive/filters.yml:7/kind=metric/direction=FORWARD",
			expected: &testLabels{
				Suite:     "exhaustive",
				QueryFile: "filters",
				Kind:      "metric",
				Direction: "FORWARD",
			},
		},
		{
			name:    "too few segments",
			input:   "TestRemoteStorageEquality/fast",
			wantErr: true,
		},
		{
			name:    "missing kind prefix",
			input:   "TestRemoteStorageEquality/fast/basic.yaml:3/metric/direction=FORWARD",
			wantErr: true,
		},
		{
			name:    "unrelated test name",
			input:   "TestSomethingElse/foo/bar",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTestName(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestParseJUnitResults(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg/logql/bench" tests="3" failures="1" time="12.5">
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=metric/direction=FORWARD" time="2.1"></testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=FORWARD" time="3.2">
      <failure message="assertion failed">expected equal</failure>
    </testcase>
    <testcase name="TestRemoteStorageEquality/fast/basic.yaml:3/kind=log/direction=BACKWARD" time="4.0"></testcase>
  </testsuite>
</testsuites>`

	results, err := parseJUnitXML([]byte(xml))
	assert.NoError(t, err)
	assert.Equal(t, 12.5, results.TotalDurationSec)
	assert.Len(t, results.Tests, 3)

	assert.Equal(t, statusPass, results.Tests[0].Status)
	assert.Equal(t, "fast", results.Tests[0].Labels.Suite)
	assert.Equal(t, 2.1, results.Tests[0].DurationSec)

	assert.Equal(t, statusFail, results.Tests[1].Status)

	assert.Equal(t, statusPass, results.Tests[2].Status)
	assert.Equal(t, "BACKWARD", results.Tests[2].Labels.Direction)
}

func TestParseJUnitResults_UnparseableName(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="pkg/logql/bench" tests="1" time="1.0">
    <testcase name="TestSomethingElse/unknown" time="0.5"></testcase>
  </testsuite>
</testsuites>`

	results, err := parseJUnitXML([]byte(xml))
	assert.NoError(t, err)
	assert.Len(t, results.Tests, 1)
	assert.Nil(t, results.Tests[0].Labels)
	assert.Equal(t, statusPass, results.Tests[0].Status)
}
