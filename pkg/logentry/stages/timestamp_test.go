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
		config         *TimestampConfig
		err            error
		expectedFormat string
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
			err:            nil,
			expectedFormat: time.RFC3339,
		},
		"custom format": {
			config: &TimestampConfig{
				Source: "source1",
				Format: "2006-01-23",
			},
			err:            nil,
			expectedFormat: "2006-01-23",
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			format, err := validateTimestampConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.expectedFormat != "" {
				assert.Equal(t, test.expectedFormat, format)
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
			assert.Equal(t, test.expected, ts)

		})
	}
}
