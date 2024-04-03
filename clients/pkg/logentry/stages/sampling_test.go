package stages

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var testSampingYaml = `
pipeline_stages:
- sampling:
    rate: 0.5
`

func TestSamplingPipeline(t *testing.T) {
	registry := prometheus.NewRegistry()
	pl, err := NewPipeline(util_log.Logger, loadConfig(testSampingYaml), &plName, registry)
	require.NoError(t, err)

	entries := make([]Entry, 0)
	for i := 0; i < 100; i++ {
		entries = append(entries, newEntry(nil, nil, testMatchLogLineApp1, time.Now()))
	}

	out := processEntries(pl, entries...,
	)
	// sampling rate = 0.5,entries len = 100,
	// The theoretical sample size is 50.
	// 50>30 and 50<70
	assert.GreaterOrEqual(t, len(out), 30)
	assert.LessOrEqual(t, len(out), 70)

}

func Test_validateSamplingConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *SamplingConfig
		wantErr error
	}{
		{
			name: "Invalid rate",
			config: &SamplingConfig{
				SamplingRate: 12,
			},
			wantErr: fmt.Errorf(ErrSamplingStageInvalidRate, 12.0),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSamplingConfig(tt.config); ((err != nil) && (err.Error() != tt.wantErr.Error())) || (err == nil && tt.wantErr != nil) {
				t.Errorf("validateDropConfig() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
