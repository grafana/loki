package stages

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
var testOutputLogLineWithMissingKey = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"nested" : {"child":"value"}
}
`

func TestPipeline_Output(t *testing.T) {
	pl, err := NewPipeline(util_log.Logger, loadConfig(testOutputYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	out := processEntries(pl, newEntry(nil, nil, testOutputLogLine, time.Now()))[0]

	assert.Equal(t, "this is a log line", out.Line)
}

func TestPipelineWithMissingKey_Output(t *testing.T) {
	var buf bytes.Buffer
	w := log.NewSyncWriter(&buf)
	logger := log.NewLogfmtLogger(w)
	pl, err := NewPipeline(logger, loadConfig(testOutputYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	Debug = true
	_ = processEntries(pl, newEntry(nil, nil, testOutputLogLineWithMissingKey, time.Now()))
	expectedLog := "level=debug msg=\"extracted output could not be converted to a string\" err=\"Can't convert <nil> to string\" type=null"
	if !(strings.Contains(buf.String(), expectedLog)) {
		t.Errorf("\nexpected: %s\n+actual: %s", expectedLog, buf.String())
	}
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
			st, err := newOutputStage(util_log.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(st, newEntry(test.extracted, nil, "replaceme", time.Time{}))[0]

			assert.Equal(t, test.expectedOutput, out.Line)
		})
	}
}
