package stages

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

var testTimestampYaml = `
pipeline_stages:
- json:
    expressions:
      ts: time
- timestamp:
    source: ts
    format: RFC3339
`

var testTimestampLogLine = `
{
	"time":"2012-11-01T22:08:41-04:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN"
}
`

func TestTimestampPipeline(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testTimestampYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	ts := time.Now()
	entry := testTimestampLogLine
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, time.Date(2012, 11, 01, 22, 8, 41, 0, time.FixedZone("", -4*60*60)).Unix(), ts.Unix())
}

func TestTimestampValidation(t *testing.T) {
	tests := map[string]struct {
		config       *TimestampConfig
		err          error
		testString   string
		expectedTime time.Time
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyTimestampStageConfig),
		},
		"missing source": {
			config: &TimestampConfig{},
			err:    errors.New(ErrTimestampSourceRequired),
		},
		"missing format": {
			config: &TimestampConfig{
				Source: "source1",
			},
			err: errors.New(ErrTimestampFormatRequired),
		},
		"standard format": {
			config: &TimestampConfig{
				Source: "source1",
				Format: time.RFC3339,
			},
			err:          nil,
			testString:   "2012-11-01T22:08:41-04:00",
			expectedTime: time.Date(2012, 11, 01, 22, 8, 41, 0, time.FixedZone("", -4*60*60)),
		},
		"custom format": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "2006-01-02",
			},
			err:          nil,
			testString:   "2009-01-01",
			expectedTime: time.Date(2009, 01, 01, 00, 00, 00, 0, time.UTC),
		},
		"unix_ms": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "UnixMs",
			},
			err:          nil,
			testString:   "1562708916919",
			expectedTime: time.Date(2019, 7, 9, 21, 48, 36, 919*1000000, time.UTC),
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parser, err := validateTimestampConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.testString != "" {
				ts, err := parser(test.testString)
				if err != nil {
					t.Errorf("validateOutputConfig() unexpected error parsing test time: %v", err)
					return
				}
				assert.Equal(t, test.expectedTime.UnixNano(), ts.UnixNano())
			}
		})
	}
}

func TestTimestampStage_Process(t *testing.T) {
	tests := map[string]struct {
		config    TimestampConfig
		extracted map[string]interface{}
		expected  time.Time
	}{
		"set success": {
			TimestampConfig{
				Source: "ts",
				Format: time.RFC3339,
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "2106-01-02T23:04:05-04:00",
			},
			time.Date(2106, 01, 02, 23, 04, 05, 0, time.FixedZone("", -4*60*60)),
		},
		"unix success": {
			TimestampConfig{
				Source: "ts",
				Format: "Unix",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 0, time.UTC),
		},
		"unix millisecond success": {
			TimestampConfig{
				Source: "ts",
				Format: "UnixMs",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916414",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 414*1000000, time.UTC),
		},
		"unix nano success": {
			TimestampConfig{
				Source: "ts",
				Format: "UnixNs",
			},
			map[string]interface{}{
				"somethigelse": "notimportant",
				"ts":           "1562708916000000123",
			},
			time.Date(2019, 7, 9, 21, 48, 36, 123, time.UTC),
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newTimestampStage(util.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			ts := time.Now()
			lbls := model.LabelSet{}
			st.Process(lbls, test.extracted, &ts, nil)
			assert.Equal(t, test.expected.UnixNano(), ts.UnixNano())

		})
	}
}
