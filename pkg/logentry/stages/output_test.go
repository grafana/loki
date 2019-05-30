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

var testOutputYaml = `
pipeline_stages:
- json:
    expressions:
      out:  message
- output:
    source: out
`

var testOutputLogLine = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"nested" : {"child":"value"},
	"message" : "this is a log line"
}
`

func TestPipeline_Output(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testOutputYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	ts := time.Now()
	entry := testOutputLogLine
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, "this is a log line", entry)
}

func TestOutputValidation(t *testing.T) {
	tests := map[string]struct {
		config *OutputConfig
		err    error
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyOutputStageConfig),
		},
		"missing source": {
			config: &OutputConfig{
				Source: "",
			},
			err: errors.New(ErrOutputSourceRequired),
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateOutputConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
		})
	}
}

func TestOutputStage_Process(t *testing.T) {
	tests := map[string]struct {
		config         OutputConfig
		extracted      map[string]interface{}
		expectedOutput string
	}{
		"sets output": {
			OutputConfig{
				Source: "out",
			},
			map[string]interface{}{
				"something": "notimportant",
				"out":       "outmessage",
			},
			"outmessage",
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newOutputStage(util.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			lbls := model.LabelSet{}
			entry := "replaceme"
			st.Process(lbls, test.extracted, nil, &entry)
			assert.Equal(t, test.expectedOutput, entry)
		})
	}
}
