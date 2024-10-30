package azureeventhubs

import (
	"errors"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func Test_validateConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *scrapeconfig.Config
		expectedCfg *scrapeconfig.Config
	}{
		{
			name: "default groupID",
			cfg: &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
			}},
			expectedCfg: &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
				GroupID:                 "promtail",
			}},
		},
		{
			name: "success",
			cfg: &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
				GroupID:                 "my groupID",
			}},
			expectedCfg: &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
				GroupID:                 "my groupID",
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
			err:  errors.New("azure_event_hubs configuration is empty"),
		},
		{
			name: "no fully qualified namespace",
			cfg:  &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{}},
			err:  errors.New("no fully_qualified_namespace defined"),
		},
		{
			name: "no event hubs",
			cfg: &scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
			}},
			err: errors.New("no event_hubs defined"),
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

func Test_getConfig(t *testing.T) {
	cfg := getConfig("myConnectionString")

	assert.Equal(t, 30*time.Second, cfg.Net.DialTimeout)
	assert.True(t, cfg.Net.SASL.Enable)
	assert.Equal(t, "$ConnectionString", cfg.Net.SASL.User)
	assert.Equal(t, "myConnectionString", cfg.Net.SASL.Password)
	assert.Equal(t, sarama.SASLMechanism(sarama.SASLTypePlaintext), cfg.Net.SASL.Mechanism)

	assert.True(t, cfg.Net.TLS.Enable)
	assert.Equal(t, sarama.V1_0_0_0, cfg.Version)
}

func TestNewSyncer_errors(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.DefaultRegisterer

	tests := []struct {
		name string
		cfg  scrapeconfig.Config
		err  error
	}{
		{
			name: "error creating kafka client, missing password",
			cfg: scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
				GroupID:                 "my groupID",
			}},
			err: errors.New("error creating kafka client: kafka: invalid configuration (Net.SASL.Password must not be empty when SASL is enabled)"),
		},
		{
			name: "error creating kafka client, missing port in address",
			cfg: scrapeconfig.Config{AzureEventHubsConfig: &scrapeconfig.AzureEventHubsTargetConfig{
				FullyQualifiedNamespace: "some host name",
				EventHubs:               []string{"some event hub"},
				GroupID:                 "my groupID",
				ConnectionString:        "some connection string",
			}},
			err: errors.New("error creating kafka client: kafka: client has run out of available brokers to talk to: dial tcp: address some host name: missing port in address"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSyncer(reg, logger, tt.cfg, nil)
			assert.Equal(t, err.Error(), tt.err.Error())
		})
	}
}
