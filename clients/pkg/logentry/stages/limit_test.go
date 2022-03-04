package stages

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// Not all these are tested but are here to make sure the different types marshal without error
var testLimitWaitYaml = `
pipeline_stages:
- json:
    expressions:
      app:
      msg:
- limit:
    rate: 1
    burst: 1
    drop: false
`

// Not all these are tested but are here to make sure the different types marshal without error
var testLimitDropYaml = `
pipeline_stages:
- json:
    expressions:
      app:
      msg:
- limit:
    rate: 1
    burst: 1
    drop: true
`

// TestLimitPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestLimitWaitPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "testPipeline"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testLimitWaitYaml), &plName, registry)
	logs := make([]Entry, 0)
	logCount := 5
	for i := 0; i < logCount; i++ {
		logs = append(logs, newEntry(nil, model.LabelSet{"app": "loki"}, testMatchLogLineApp1, time.Now()))
	}
	require.NoError(t, err)
	out := processEntries(pl,
		logs...,
	)
	// Only the second line will go through.
	assert.Len(t, out, logCount)
	assert.Equal(t, out[0].Line, testMatchLogLineApp1)
}

// TestLimitPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestLimitDropPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "testPipeline"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testLimitDropYaml), &plName, registry)
	logs := make([]Entry, 0)
	logCount := 10
	for i := 0; i < logCount; i++ {
		logs = append(logs, newEntry(nil, model.LabelSet{"app": "loki"}, testMatchLogLineApp1, time.Now()))
	}
	require.NoError(t, err)
	out := processEntries(pl,
		logs...,
	)
	// Only the second line will go through.
	assert.Len(t, out, 1)
	assert.Equal(t, out[0].Line, testMatchLogLineApp1)
}
