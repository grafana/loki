package stages

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

// nolint
func mustParseTime(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

func toLabelSet(lbs map[string]string) model.LabelSet {
	res := model.LabelSet{}
	for k, v := range lbs {
		res[model.LabelName(k)] = model.LabelValue(v)
	}
	return res
}

func assertLabels(t *testing.T, expect map[string]string, got model.LabelSet) {
	if len(expect) != len(got) {
		t.Fatalf("labels are not equal in size want: %s got: %s", expect, got)
	}
	for k, v := range expect {
		gotV, ok := got[model.LabelName(k)]
		if !ok {
			t.Fatalf("missing expected label key: %s", k)
		}
		assert.Equal(t, model.LabelValue(v), gotV, "mismatch label value")
	}
}

// Verify the formatting of float conversion to make sure there are not any trailing zeros,
// and also make sure unix timestamps are converted properly
func TestGetString(t *testing.T) {
	var f64, f64_1 float64
	var f32 float32
	f64 = 1
	f64_1 = 1562723913000
	f32 = 2.02
	s64, err := getString(f64)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	s64_1, err := getString(f64_1)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	s32, err := getString(f32)
	if err != nil {
		t.Errorf("Failed to get string from float... this shouldn't have happened: %v", err)
		return
	}
	assert.Equal(t, "1", s64)
	assert.Equal(t, "2.02", s32)
	assert.Equal(t, "1562723913000", s64_1)

	_, err = getString(nil)
	assert.Error(t, err)
}

var (
	location, _ = time.LoadLocation("America/New_York")
)

func TestConvertDateLayout(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		layout    string
		location  *time.Location
		timestamp string
		expected  time.Time
	}{
		"custom layout with year": {
			"2006 Jan 02 15:04:05",
			nil,
			"2019 Jul 15 01:02:03",
			time.Date(2019, 7, 15, 1, 2, 3, 0, time.UTC),
		},
		"custom layout without year": {
			"Jan 02 15:04:05",
			nil,
			"Jul 15 01:02:03",
			time.Date(time.Now().Year(), 7, 15, 1, 2, 3, 0, time.UTC),
		},
		"custom layout with year and location": {
			"Jan 02 15:04:05",
			location,
			"Jul 15 01:02:03",
			time.Date(time.Now().Year(), 7, 15, 1, 2, 3, 0, location),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			parser := convertDateLayout(testData.layout, testData.location)
			parsed, err := parser(testData.timestamp)
			if err != nil {
				t.Errorf("convertDateLayout() parser returned an unexpected error = %v", err)
				return
			}

			assert.Equal(t, testData.expected, parsed)
		})
	}
}

func TestParseTimestampWithoutYear(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		layout    string
		location  *time.Location
		timestamp string
		now       time.Time
		expected  time.Time
		err       error
	}{
		"parse timestamp within current year": {
			"Jan 02 15:04:05",
			nil,
			"Jul 15 01:02:03",
			time.Date(2019, 7, 14, 0, 0, 0, 0, time.UTC),
			time.Date(2019, 7, 15, 1, 2, 3, 0, time.UTC),
			nil,
		},
		"parse timestamp with location DST": {
			"Jan 02 15:04:05",
			location,
			"Jul 15 01:02:03",
			time.Date(2019, 7, 14, 0, 0, 0, 0, time.UTC),
			time.Date(2019, 7, 15, 1, 2, 3, 0, location),
			nil,
		},
		"parse timestamp with location non DST": {
			"Jan 02 15:04:05",
			location,
			"Jan 15 01:02:03",
			time.Date(2019, 7, 14, 0, 0, 0, 0, time.UTC),
			time.Date(2019, 1, 15, 1, 2, 3, 0, location),
			nil,
		},
		"parse timestamp on 31th Dec and today is 1st Jan": {
			"Jan 02 15:04:05",
			nil,
			"Dec 31 23:59:59",
			time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2018, 12, 31, 23, 59, 59, 0, time.UTC),
			nil,
		},
		"parse timestamp on 1st Jan and today is 31st Dec": {
			"Jan 02 15:04:05",
			nil,
			"Jan 01 01:02:03",
			time.Date(2018, 12, 31, 23, 59, 59, 0, time.UTC),
			time.Date(2019, 1, 1, 1, 2, 3, 0, time.UTC),
			nil,
		},
		"error if the input layout actually includes the year component": {
			"2006 Jan 02 15:04:05",
			nil,
			"2019 Jan 01 01:02:03",
			time.Date(2019, 1, 1, 1, 2, 3, 0, time.UTC),
			time.Date(2019, 1, 1, 1, 2, 3, 0, time.UTC),
			fmt.Errorf(ErrTimestampContainsYear, "2019 Jan 01 01:02:03"),
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			parsed, err := parseTimestampWithoutYear(testData.layout, testData.location, testData.timestamp, testData.now)
			if ((err != nil) != (testData.err != nil)) || (err != nil && testData.err != nil && err.Error() != testData.err.Error()) {
				t.Errorf("parseTimestampWithoutYear() expected error = %v, actual error = %v", testData.err, err)
				return
			}

			assert.Equal(t, testData.expected, parsed)
		})
	}
}
