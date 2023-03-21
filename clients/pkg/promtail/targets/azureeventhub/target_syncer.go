package azureeventhub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/kafka"
)

func NewSyncer(
	reg prometheus.Registerer,
	logger log.Logger,
	cfg scrapeconfig.Config,
	pushClient api.EntryHandler,
) (*kafka.TargetSyncer, error) {
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	config := getConfig(cfg.AzureEventHubConfig.ConnectionString)

	client, err := sarama.NewClient([]string{cfg.AzureEventHubConfig.FullyQualifiedNamespace}, config)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %w", err)
	}
	group, err := sarama.NewConsumerGroup([]string{cfg.AzureEventHubConfig.FullyQualifiedNamespace}, cfg.AzureEventHubConfig.GroupID, config)
	if err != nil {
		return nil, fmt.Errorf("error creating consumer group client: %w", err)
	}
	pipeline, err := stages.NewPipeline(log.With(logger, "component", "azureeventhub_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
	if err != nil {
		return nil, fmt.Errorf("error creating pipeline: %w", err)
	}

	targetSyncConfig := &kafka.TargetSyncerConfig{
		RelabelConfigs:       cfg.RelabelConfigs,
		UseIncomingTimestamp: cfg.AzureEventHubConfig.UseIncomingTimestamp,
		Labels:               cfg.AzureEventHubConfig.Labels,
		GroupID:              cfg.AzureEventHubConfig.GroupID,
	}

	t, err := kafka.NewSyncer(context.Background(), reg, logger, pushClient, pipeline, group, client, &eventHubMessageParser{
		disallowCustomMessages: cfg.AzureEventHubConfig.DisallowCustomMessages,
	}, cfg.AzureEventHubConfig.EventHubs, targetSyncConfig)
	if err != nil {
		return nil, fmt.Errorf("error starting azureeventhub target: %w", err)
	}

	return t, nil
}

func validateConfig(cfg *scrapeconfig.Config) error {
	if cfg.AzureEventHubConfig == nil {
		return errors.New("azureeventhub configuration is empty")
	}

	if len(cfg.AzureEventHubConfig.FullyQualifiedNamespace) == 0 {
		return errors.New("no event hubs brokers defined")
	}

	if len(cfg.AzureEventHubConfig.EventHubs) == 0 {
		return errors.New("no topics given to be consumed")
	}

	if cfg.AzureEventHubConfig.GroupID == "" {
		cfg.AzureEventHubConfig.GroupID = "promtail"
	}
	return nil
}

func getConfig(connection string) *sarama.Config {
	config := sarama.NewConfig()
	config.Net.DialTimeout = 30 * time.Second
	config.Net.SASL.Enable = true
	config.Net.SASL.User = "$ConnectionString"
	config.Net.SASL.Password = connection
	config.Net.SASL.Mechanism = sarama.SASLTypePlaintext

	config.Net.TLS.Enable = true
	config.Version = sarama.V1_0_0_0

	return config
}
