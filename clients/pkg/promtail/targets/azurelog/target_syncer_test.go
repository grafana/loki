package azurelog

import (
	"errors"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/stretchr/testify/assert"
	"testing"
)

func Test_validateConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *scrapeconfig.Config
		expectedCfg *scrapeconfig.Config
	}{
		{
			name: "default groupID",
			cfg: &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{
				Brokers: []string{"some broker"},
				Topics:  []string{"some topic"},
			}},
			expectedCfg: &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{
				Brokers: []string{"some broker"},
				Topics:  []string{"some topic"},
				GroupID: "promtail",
			}},
		},
		{
			name: "success",
			cfg: &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{
				Brokers: []string{"some broker"},
				Topics:  []string{"some topic"},
				GroupID: "my groupID",
			}},
			expectedCfg: &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{
				Brokers: []string{"some broker"},
				Topics:  []string{"some topic"},
				GroupID: "my groupID",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			assert.NoError(t, err)
			assert.Equal(t, tt.cfg, tt.expectedCfg)
		})
	}
}

func Test_validateConfig_error(t *testing.T) {

	tests := []struct {
		name string
		cfg  *scrapeconfig.Config
		err  error
	}{
		{
			name: "empty config",
			cfg:  &scrapeconfig.Config{},
			err:  errors.New("azurelog configuration is empty"),
		},
		{
			name: "no brokers",
			cfg:  &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{}},
			err:  errors.New("no event hubs brokers defined"),
		},
		{
			name: "no topics",
			cfg: &scrapeconfig.Config{AzurelogConfig: &scrapeconfig.AzurelogTargetConfig{
				Brokers: []string{"some broker"},
			}},
			err: errors.New("no topics given to be consumed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			assert.Error(t, err)
			assert.Equal(t, tt.err, err)
		})
	}
}
