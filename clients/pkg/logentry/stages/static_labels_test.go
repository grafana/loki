package stages

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func Test_staticLabelStage_Process(t *testing.T) {
	staticVal := "val"

	tests := []struct {
		name           string
		config         StaticLabelConfig
		inputLabels    model.LabelSet
		expectedLabels model.LabelSet
	}{
		{
			name: "add static label",
			config: map[string]*string{
				"staticLabel": &staticVal,
			},
			inputLabels: model.LabelSet{
				"testLabel": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel":   "testValue",
				"staticLabel": "val",
			},
		},
		{
			name: "add static label with empty value",
			config: map[string]*string{
				"staticLabel": nil,
			},
			inputLabels: model.LabelSet{
				"testLabel": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel": "testValue",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := newStaticLabelsStage(util_log.Logger, test.config)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(st, newEntry(nil, test.inputLabels, "", time.Now()))[0]
			assert.Equal(t, test.expectedLabels, out.Labels)
		})
	}
}
