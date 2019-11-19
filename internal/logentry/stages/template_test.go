package stages

import (
	"errors"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
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
    source: notexist
    template: "TEST"
- labels:
    app: ''
    level: ''
    type: notexist
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

func TestPipeline_Template(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testTemplateYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	expectedLbls := model.LabelSet{
		"app":   "LOKI doki",
		"level": "OK",
		"type":  "TEST",
	}
	ts := time.Now()
	entry := testTemplateLogLine
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, expectedLbls, lbls)
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
		test := test
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
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newTemplateStage(util.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			lbls := model.LabelSet{}
			entry := "not important for this test"
			st.Process(lbls, test.extracted, nil, &entry)
			assert.Equal(t, test.expectedExtracted, test.extracted)
		})
	}
}
