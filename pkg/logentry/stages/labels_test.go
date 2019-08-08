package stages

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

var testLabelsYaml = `
pipeline_stages:
- json:
    expressions:
      level:
      app_rename: app
- labels:
    level:
    app: app_rename
`

var testLabelsLogLine = `
{
	"time":"2012-11-01T22:08:41+00:00",
	"app":"loki",
	"component": ["parser","type"],
	"level" : "WARN"
}
`

func TestLabelsPipeline_Labels(t *testing.T) {
	pl, err := NewPipeline(util.Logger, loadConfig(testLabelsYaml), nil, prometheus.DefaultRegisterer)
	if err != nil {
		t.Fatal(err)
	}
	lbls := model.LabelSet{}
	expectedLbls := model.LabelSet{
		"level": "WARN",
		"app":   "loki",
	}
	ts := time.Now()
	entry := testLabelsLogLine
	extracted := map[string]interface{}{}
	pl.Process(lbls, extracted, &ts, &entry)
	assert.Equal(t, expectedLbls, lbls)
}

var (
	lv1  = "lv1"
	lv2c = "l2"
	lv3  = ""
	lv3c = "l3"
)

func TestLabels(t *testing.T) {
	tests := map[string]struct {
		config       LabelsConfig
		err          error
		expectedCfgs LabelsConfig
	}{
		"missing config": {
			config:       nil,
			err:          errors.New(ErrEmptyLabelStageConfig),
			expectedCfgs: nil,
		},
		"invalid label name": {
			config: LabelsConfig{
				"#*FDDS*": nil,
			},
			err:          fmt.Errorf(ErrInvalidLabelName, "#*FDDS*"),
			expectedCfgs: nil,
		},
		"label value is set from name": {
			config: LabelsConfig{
				"l1": &lv1,
				"l2": nil,
				"l3": &lv3,
			},
			err: nil,
			expectedCfgs: LabelsConfig{
				"l1": &lv1,
				"l2": &lv2c,
				"l3": &lv3c,
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateLabelsConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateLabelsConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateLabelsConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.expectedCfgs != nil {
				assert.Equal(t, test.expectedCfgs, test.config)
			}
		})
	}
}

func TestLabelStage_Process(t *testing.T) {
	sourceName := "diff_source"
	tests := map[string]struct {
		config         LabelsConfig
		extractedData  map[string]interface{}
		inputLabels    model.LabelSet
		expectedLabels model.LabelSet
	}{
		"extract_success": {
			LabelsConfig{
				"testLabel": nil,
			},
			map[string]interface{}{
				"testLabel": "testValue",
			},
			model.LabelSet{},
			model.LabelSet{
				"testLabel": "testValue",
			},
		},
		"different_source_name": {
			LabelsConfig{
				"testLabel": &sourceName,
			},
			map[string]interface{}{
				sourceName: "testValue",
			},
			model.LabelSet{},
			model.LabelSet{
				"testLabel": "testValue",
			},
		},
		"empty_extracted_data": {
			LabelsConfig{
				"testLabel": &sourceName,
			},
			map[string]interface{}{},
			model.LabelSet{},
			model.LabelSet{},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st, err := newLabelStage(util.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			st.Process(test.inputLabels, test.extractedData, nil, nil)
			assert.Equal(t, test.expectedLabels, test.inputLabels)
		})
	}
}
