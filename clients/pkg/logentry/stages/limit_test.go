package stages

import (
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Not all these are tested but are here to make sure the different types marshal without error
var testLimitYaml = `
pipeline_stages:
- json:
    expressions:
      app:
      msg:
- limit:
    source: app
    value: loki
    rate: 10
    burst: 100
    drop: true
`

// TestLimitPipeline is used to verify we properly parse the yaml config and create a working pipeline
func TestLimitPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	plName := "test_pipeline"
	pl, err := NewPipeline(util_log.Logger, loadConfig(testLimitYaml), &plName, registry)
	require.NoError(t, err)
	out := processEntries(pl,
		newEntry(nil, nil, testMatchLogLineApp1, time.Now()),
		newEntry(nil, nil, testMatchLogLineApp2, time.Now()),
	)

	// Only the second line will go through.
	assert.Len(t, out, 2)
	assert.Equal(t, out[0].Line, testMatchLogLineApp1)
}
