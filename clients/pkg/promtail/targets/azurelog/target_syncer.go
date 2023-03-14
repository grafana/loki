package azurelog

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
	config := getConfig(cfg.AzurelogConfig.ConnectionString)

	client, err := sarama.NewClient(cfg.AzurelogConfig.Brokers, config)
	if err != nil {
		return nil, fmt.Errorf("error creating kafka client: %w", err)
	}
	group, err := sarama.NewConsumerGroup(cfg.AzurelogConfig.Brokers, cfg.AzurelogConfig.GroupID, config)
	if err != nil {
		return nil, fmt.Errorf("error creating consumer group client: %w", err)
	}
	pipeline, err := stages.NewPipeline(log.With(logger, "component", "azurelog_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
	if err != nil {
		return nil, fmt.Errorf("error creating pipeline: %w", err)
	}

	targetSyncConfig := &kafka.TargetSyncerConfig{
		RelabelConfigs:       cfg.RelabelConfigs,
		UseIncomingTimestamp: cfg.AzurelogConfig.UseIncomingTimestamp,
		Labels:               cfg.AzurelogConfig.Labels,
		GroupID:              cfg.AzurelogConfig.GroupID,
	}

	t, err := kafka.NewSyncer(context.Background(), reg, logger, pushClient, pipeline, group, client, messageParser, cfg.AzurelogConfig.Topics, targetSyncConfig)
	if err != nil {
		return nil, fmt.Errorf("error starting azurelog target: %w", err)
	}

	return t, nil
}

func validateConfig(cfg *scrapeconfig.Config) error {
	if cfg.AzurelogConfig == nil {
		return errors.New("azurelog configuration is empty")
	}

	if len(cfg.AzurelogConfig.Brokers) == 0 {
		return errors.New("no event hubs bootstrap brokers defined")
	}

	if len(cfg.AzurelogConfig.Topics) == 0 {
		return errors.New("no topics given to be consumed")
	}

	if cfg.AzurelogConfig.GroupID == "" {
		cfg.KafkaConfig.GroupID = "promtail"
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
