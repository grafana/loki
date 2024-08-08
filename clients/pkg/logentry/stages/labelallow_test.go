package stages

import (
	"testing"
	"time"

	ww "github.com/grafana/dskit/server"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func Test_addLabelStage_Process(t *testing.T) {
	// Enable debug logging
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg, nil, false)
	Debug = true

	tests := []struct {
		name           string
		config         *LabelAllowConfig
		inputLabels    model.LabelSet
		expectedLabels model.LabelSet
	}{
		{
			name:   "allow single label",
			config: &LabelAllowConfig{"testLabel1"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel1": "testValue",
			},
		},
		{
			name:   "allow multiple labels",
			config: &LabelAllowConfig{"testLabel1", "testLabel2"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
				"testLabel3": "testValue",
			},
			expectedLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
		},
		{
			name:   "allow non-existing label",
			config: &LabelAllowConfig{"foobar"},
			inputLabels: model.LabelSet{
				"testLabel1": "testValue",
				"testLabel2": "testValue",
			},
			expectedLabels: model.LabelSet{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st, err := newLabelAllowStage(test.config)
			if err != nil {
				t.Fatal(err)
			}
			out := processEntries(st, newEntry(nil, test.inputLabels, "", time.Now()))[0]
			assert.Equal(t, test.expectedLabels, out.Labels)
		})
	}
}
