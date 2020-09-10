package stages

import (
	"testing"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	ww "github.com/weaveworks/common/server"
)

func Test_dropLabelStage_Process(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	cfg.LogLevel.Set("debug")
	util.InitLogger(cfg)
	Debug = true

	tests := []struct {
		name           string
		config         *LabelDropConfig
		inputLabels    model.LabelSet
		expectedLabels model.LabelSet
	}{
		{
			name:   "drop one label",
			config: &LabelDropConfig{"testLabel1"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel2": "testValue",
			},
		},
		{
			name:   "drop two labels",
			config: &LabelDropConfig{"testLabel1", "testLabel2"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
			expectedLabels: model.LabelSet{},
		},
		{
			name:   "drop non-existing label",
			config: &LabelDropConfig{"foobar"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := newLabelDropStage(test.config)
			if err != nil {
				t.Fatal(err)
			}
			st.Process(test.inputLabels, map[string]interface{}{}, nil, nil)
			assert.Equal(t, test.expectedLabels, test.inputLabels)
		})
	}
}
