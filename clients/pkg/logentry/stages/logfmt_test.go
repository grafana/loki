package stages

import (
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var testLogfmtYamlSingleStageWithoutSource = `
pipeline_stages:
- logfmt:
    mapping:
      out:  message
      app:
      duration:
      unknown:
`

var testLogfmtYamlMultiStageWithSource = `
pipeline_stages:
- logfmt:
    mapping:
      extra:
- logfmt:
    mapping:
      user:
    source: extra
`

func TestPipeline_Logfmt(t *testing.T) {
	var testLogfmtLogLine = `
		time=2012-11-01T22:08:41+00:00 app=loki	level=WARN duration=125 message="this is a log line" extra="user=foo""
	`
	t.Parallel()

	tests := map[string]struct {
		config          string
		entry           string
		expectedExtract map[string]interface{}
	}{
		"successfully run a pipeline with 1 logfmt stage without source": {
			testLogfmtYamlSingleStageWithoutSource,
			testLogfmtLogLine,
			map[string]interface{}{
				"out":      "this is a log line",
				"app":      "loki",
				"duration": "125",
			},
		},
		"successfully run a pipeline with 2 logfmt stages with source": {
			testLogfmtYamlMultiStageWithSource,
			testLogfmtLogLine,
			map[string]interface{}{
				"extra": "user=foo",
				"user":  "foo",
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			pl, err := NewPipeline(util_log.Logger, loadConfig(testData.config), nil, prometheus.DefaultRegisterer)
			assert.NoError(t, err)
			out := processEntries(pl, newEntry(nil, nil, testData.entry, time.Now()))[0]
			assert.Equal(t, testData.expectedExtract, out.Extracted)
		})
	}
}

var testLogfmtCfg = `logfmt:
  mapping:
    foo1: bar1
    foo2:`

// nolint
func TestLogfmtYamlMapStructure(t *testing.T) {
	t.Parallel()

	// testing that we can use yaml data into mapstructure.
	var mapstruct map[interface{}]interface{}
	assert.NoError(t, yaml.Unmarshal([]byte(testLogfmtCfg), &mapstruct))
	p, ok := mapstruct["logfmt"].(map[interface{}]interface{})
	assert.True(t, ok)
	got, err := parseLogfmtConfig(p)
	assert.NoError(t, err)
	want := &LogfmtConfig{
		Mapping: map[string]string{
			"foo1": "bar1",
			"foo2": "",
		},
	}
	assert.EqualValues(t, want, got)
}

func TestLogfmtConfig_validate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config           interface{}
		wantMappingCount int
		err              error
	}{
		"empty config": {
			nil,
			0,
			errors.New(ErrMappingRequired),
		},
		"no mapping": {
			map[string]interface{}{},
			0,
			errors.New(ErrMappingRequired),
		},
		"empty source": {
			map[string]interface{}{
				"mapping": map[string]string{
					"extr1": "expr",
				},
				"source": "",
			},
			0,
			errors.New(ErrEmptyLogfmtStageSource),
		},
		"valid without source": {
			map[string]interface{}{
				"mapping": map[string]string{
					"foo1": "foo",
					"foo2": "",
				},
			},
			2,
			nil,
		},
		"valid with source": {
			map[string]interface{}{
				"mapping": map[string]string{
					"foo1": "foo",
					"foo2": "",
				},
				"source": "log",
			},
			2,
			nil,
		},
	}
	for tName, tt := range tests {
		t.Run(tName, func(t *testing.T) {
			c, err := parseLogfmtConfig(tt.config)
			assert.NoError(t, err)
			got, err := validateLogfmtConfig(c)
			if tt.err != nil {
				assert.EqualError(t, err, tt.err.Error())
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.wantMappingCount, len(got))
		})
	}
}

var testLogfmtLogFixture = `
	time=2012-11-01T22:08:41+00:00
	app=loki
	level=WARN
	nested="child=value"
	message="this is a log line"
`

func TestLogfmtParser_Parse(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		config          interface{}
		extracted       map[string]interface{}
		entry           string
		expectedExtract map[string]interface{}
	}{
		"successfully decode logfmt on entry": {
			map[string]interface{}{
				"mapping": map[string]string{
					"time":    "",
					"app":     "",
					"level":   "",
					"nested":  "",
					"message": "",
				},
			},
			map[string]interface{}{},
			testLogfmtLogFixture,
			map[string]interface{}{
				"time":    "2012-11-01T22:08:41+00:00",
				"app":     "loki",
				"level":   "WARN",
				"nested":  "child=value",
				"message": "this is a log line",
			},
		},
		"successfully decode logfmt on extracted[source]": {
			map[string]interface{}{
				"mapping": map[string]string{
					"time":    "",
					"app":     "",
					"level":   "",
					"nested":  "",
					"message": "",
				},
				"source": "log",
			},
			map[string]interface{}{
				"log": testLogfmtLogFixture,
			},
			"{}",
			map[string]interface{}{
				"time":    "2012-11-01T22:08:41+00:00",
				"app":     "loki",
				"level":   "WARN",
				"nested":  "child=value",
				"message": "this is a log line",
				"log":     testLogfmtLogFixture,
			},
		},
		"missing extracted[source]": {
			map[string]interface{}{
				"mapping": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]interface{}{},
			testLogfmtLogFixture,
			map[string]interface{}{},
		},
		"invalid logfmt on entry": {
			map[string]interface{}{
				"mapping": map[string]string{
					"expr1": "",
				},
			},
			map[string]interface{}{},
			"{\"invalid\":\"logfmt\"}",
			map[string]interface{}{},
		},
		"invalid logfmt on extracted[source]": {
			map[string]interface{}{
				"mapping": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]interface{}{
				"log": "not logfmt",
			},
			testLogfmtLogFixture,
			map[string]interface{}{
				"log": "not logfmt",
			},
		},
		"nil source": {
			map[string]interface{}{
				"mapping": map[string]string{
					"app": "",
				},
				"source": "log",
			},
			map[string]interface{}{
				"log": nil,
			},
			testLogfmtLogFixture,
			map[string]interface{}{
				"log": nil,
			},
		},
	}
	for tName, tt := range tests {
		t.Run(tName, func(t *testing.T) {
			t.Parallel()
			p, err := New(util_log.Logger, nil, StageTypeLogfmt, tt.config, nil)
			assert.NoError(t, err)
			out := processEntries(p, newEntry(tt.extracted, nil, tt.entry, time.Now()))[0]

			assert.Equal(t, tt.expectedExtract, out.Extracted)
		})
	}
}
