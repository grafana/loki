package stages

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var testTemplateYaml = `
pipeline_stages:
- json:
    expressions:
      app:  app
      level: level
- template:
    source: app
    template: '{{ .Value | ToUpper }} doki'
- template:
    source: level
    template: '{{ if eq .Value "WARN" }}{{ Replace .Value "WARN" "OK" -1 }}{{ else }}{{ .Value }}{{ end }}'
- template:
    source: nonexistent
    template: "TEST"
- labels:
    app: ''
    level: ''
    type: nonexistent
`

var testTemplateLogLine = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN",
	"nested" : {"child":"value"},
	"message" : "this is a log line"
}
`

var testTemplateLogLineWithMissingKey = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"component": ["parser","type"],
	"level" : "WARN",
	"nested" : {"child":"value"},
	"message" : "this is a log line"
}
`

func TestPipeline_Template(t *testing.T) {
	pl, err := NewPipeline(util_log.Logger, loadConfig(testTemplateYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	expectedLbls := model.LabelSet{
		"app":   "LOKI doki",
		"level": "OK",
		"type":  "TEST",
	}
	out := processEntries(pl, newEntry(nil, nil, testTemplateLogLine, time.Now()))[0]
	assert.Equal(t, expectedLbls, out.Labels)
}

func TestPipelineWithMissingKey_Template(t *testing.T) {
	var buf bytes.Buffer
	w := log.NewSyncWriter(&buf)
	logger := log.NewLogfmtLogger(w)
	pl, err := NewPipeline(logger, loadConfig(testTemplateYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}

	_ = processEntries(pl, newEntry(nil, nil, testTemplateLogLineWithMissingKey, time.Now()))

	expectedLog := "level=debug msg=\"extracted template could not be converted to a string\" err=\"Can't convert <nil> to string\" type=null"
	if !(strings.Contains(buf.String(), expectedLog)) {
		t.Errorf("\nexpected: %s\n+actual: %s", expectedLog, buf.String())
	}
}

func TestTemplateValidation(t *testing.T) {
	tests := map[string]struct {
		config *TemplateConfig
		err    error
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyTemplateStageConfig),
		},
		"missing source": {
			config: &TemplateConfig{
				Source: "",
			},
			err: errors.New(ErrTemplateSourceRequired),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := validateTemplateConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateTemplateConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateTemplateConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
		})
	}
}

func TestTemplateStage_Process(t *testing.T) {
	tests := map[string]struct {
		config            TemplateConfig
		extracted         map[string]interface{}
		expectedExtracted map[string]interface{}
	}{
		"simple template": {
			TemplateConfig{
				Source:   "some",
				Template: "{{ .Value }} appended",
			},
			map[string]interface{}{
				"some": "value",
			},
			map[string]interface{}{
				"some": "value appended",
			},
		},
		"add missing": {
			TemplateConfig{
				Source:   "missing",
				Template: "newval",
			},
			map[string]interface{}{
				"notmissing": "value",
			},
			map[string]interface{}{
				"notmissing": "value",
				"missing":    "newval",
			},
		},
		"template with multiple keys": {
			TemplateConfig{
				Source:   "message",
				Template: "{{.Value}} in module {{.module}}",
			},
			map[string]interface{}{
				"level":   "warn",
				"app":     "loki",
				"message": "warn for app loki",
				"module":  "test",
			},
			map[string]interface{}{
				"level":   "warn",
				"app":     "loki",
				"module":  "test",
				"message": "warn for app loki in module test",
			},
		},
		"template with multiple keys with missing source": {
			TemplateConfig{
				Source:   "missing",
				Template: "{{ .level }} for app {{ .app | ToUpper }}",
			},
			map[string]interface{}{
				"level": "warn",
				"app":   "loki",
			},
			map[string]interface{}{
				"level":   "warn",
				"app":     "loki",
				"missing": "warn for app LOKI",
			},
		},
		"template with multiple keys with missing key": {
			TemplateConfig{
				Source:   "message",
				Template: "{{.Value}} in module {{.module}}",
			},
			map[string]interface{}{
				"level":   "warn",
				"app":     "loki",
				"message": "warn for app loki",
			},
			map[string]interface{}{
				"level":   "warn",
				"app":     "loki",
				"message": "warn for app loki in module <no value>",
			},
		},
		"template with multiple keys with nil value in extracted key": {
			TemplateConfig{
				Source:   "level",
				Template: "{{ Replace .Value \"Warning\" \"warn\" 1 }}",
			},
			map[string]interface{}{
				"level":   "Warning",
				"testval": nil,
			},
			map[string]interface{}{
				"level":   "warn",
				"testval": nil,
			},
		},
		"ToLower": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ .Value | ToLower }}",
			},
			map[string]interface{}{
				"testval": "Value",
			},
			map[string]interface{}{
				"testval": "value",
			},
		},
		"sprig": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ add 7 3 }}",
			},
			map[string]interface{}{
				"testval": "Value",
			},
			map[string]interface{}{
				"testval": "10",
			},
		},
		"ToLowerParams": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ ToLower .Value }}",
			},
			map[string]interface{}{
				"testval": "Value",
			},
			map[string]interface{}{
				"testval": "value",
			},
		},
		"ToLowerEmptyValue": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ .Value | ToLower }}",
			},
			map[string]interface{}{},
			map[string]interface{}{},
		},
		"ReplaceAllToLower": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ Replace .Value \" \" \"_\" -1 | ToLower }}",
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
			map[string]interface{}{
				"testval": "some_silly_value_with_lots_of_spaces",
			},
		},
		"regexReplaceAll": {
			TemplateConfig{
				Source:   "testval",
				Template: `{{ regexReplaceAll "(Silly)" .Value "${1}foo"  }}`,
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
			map[string]interface{}{
				"testval": "Some Sillyfoo Value With Lots Of Spaces",
			},
		},
		"regexReplaceAllerr": {
			TemplateConfig{
				Source:   "testval",
				Template: `{{ regexReplaceAll "\\K" .Value "${1}foo"  }}`,
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
		},
		"regexReplaceAllLiteral": {
			TemplateConfig{
				Source:   "testval",
				Template: `{{ regexReplaceAll "( |Of)" .Value "_"  }}`,
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
			map[string]interface{}{
				"testval": "Some_Silly_Value_With_Lots___Spaces",
			},
		},
		"regexReplaceAllLiteralerr": {
			TemplateConfig{
				Source:   "testval",
				Template: `{{ regexReplaceAll "\\K" .Value "err"  }}`,
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
			map[string]interface{}{
				"testval": "Some Silly Value With Lots Of Spaces",
			},
		},
		"Trim": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ Trim .Value \"!\" }}",
			},
			map[string]interface{}{
				"testval": "!!!!!WOOOOO!!!!!",
			},
			map[string]interface{}{
				"testval": "WOOOOO",
			},
		},
		"Remove label empty value": {
			TemplateConfig{
				Source:   "testval",
				Template: "",
			},
			map[string]interface{}{
				"testval": "WOOOOO",
			},
			map[string]interface{}{},
		},
		"Don't add label with empty value": {
			TemplateConfig{
				Source:   "testval",
				Template: "",
			},
			map[string]interface{}{},
			map[string]interface{}{},
		},
		"Sha2Hash": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ Sha2Hash .Value \"salt\" }}",
			},
			map[string]interface{}{
				"testval": "this is PII data",
			},
			map[string]interface{}{
				"testval": "5526fd6f8ad457279cf8ff06453c6cb61bf479fa826e3b099caa6c846f9376f2",
			},
		},
		"Hash": {
			TemplateConfig{
				Source:   "testval",
				Template: "{{ Hash .Value \"salt\" }}",
			},
			map[string]interface{}{
				"testval": "this is PII data",
			},
			map[string]interface{}{
				"testval": "0807ea24e992127128b38e4930f7155013786a4999c73a25910318a793847658",
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newTemplateStage(util_log.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}

			out := processEntries(st, newEntry(test.expectedExtracted, nil, "not important for this test", time.Time{}))[0]
			assert.Equal(t, test.expectedExtracted, out.Extracted)
		})
	}
}
