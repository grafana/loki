package rulestore

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
)

func TestIsDefaults(t *testing.T) {
	tests := map[string]struct {
		setup    func(cfg *Config)
		expected bool
	}{
		"should return true if the config only contains default values": {
			setup: func(cfg *Config) {
				flagext.DefaultValues(cfg)
			},
			expected: true,
		},
		"should return false if the config contains zero values": {
			setup:    func(_ *Config) {},
			expected: false,
		},
		"should return false if the config contains default values and some overrides": {
			setup: func(cfg *Config) {
				flagext.DefaultValues(cfg)
				cfg.Backend = "local"
			},
			expected: false,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			cfg := Config{}
			testData.setup(&cfg)

			assert.Equal(t, testData.expected, cfg.IsDefaults())
		})
	}
}
