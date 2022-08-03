package base

import (
	"context"
	"flag"
	"fmt"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	promRules "github.com/prometheus/prometheus/rules"

	configClient "github.com/grafana/loki/pkg/configs/client"
	"github.com/grafana/loki/pkg/ruler/rulestore"
	"github.com/grafana/loki/pkg/ruler/rulestore/bucketclient"
	"github.com/grafana/loki/pkg/ruler/rulestore/configdb"
	"github.com/grafana/loki/pkg/ruler/rulestore/local"
	"github.com/grafana/loki/pkg/ruler/rulestore/objectclient"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/bucket"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
)

// RuleStoreConfig configures a rule store.
// TODO remove this legacy config in Cortex 1.11.
type RuleStoreConfig struct {
	Type     string              `yaml:"type"`
	ConfigDB configClient.Config `yaml:"configdb"`

	// Object Storage Configs
	Azure azure.BlobStorageConfig   `yaml:"azure"`
	GCS   gcp.GCSConfig             `yaml:"gcs"`
	S3    aws.S3Config              `yaml:"s3"`
	BOS   baidubce.BOSStorageConfig `yaml:"bos"`
	Swift openstack.SwiftConfig     `yaml:"swift"`
	Local local.Config              `yaml:"local"`

	mock rulestore.RuleStore `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *RuleStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ConfigDB.RegisterFlagsWithPrefix("ruler.", f)
	cfg.Azure.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.GCS.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.S3.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.Swift.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.Local.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.BOS.RegisterFlagsWithPrefix("ruler.storage.", f)
	f.StringVar(&cfg.Type, "ruler.storage.type", "configdb", "Method to use for backend rule storage (configdb, azure, gcs, s3, swift, local)")
}

// Validate config and returns error on failure
func (cfg *RuleStoreConfig) Validate() error {
	if err := cfg.Swift.Validate(); err != nil {
		return errors.Wrap(err, "invalid Swift Storage config")
	}
	if err := cfg.Azure.Validate(); err != nil {
		return errors.Wrap(err, "invalid Azure Storage config")
	}
	if err := cfg.S3.Validate(); err != nil {
		return errors.Wrap(err, "invalid S3 Storage config")
	}
	return nil
}

// IsDefaults returns true if the storage options have not been set
func (cfg *RuleStoreConfig) IsDefaults() bool {
	return cfg.Type == "configdb" && cfg.ConfigDB.ConfigsAPIURL.URL == nil
}

// NewLegacyRuleStore returns a rule store backend client based on the provided cfg.
// The client used by the function is based a legacy object store clients that shouldn't
// be used anymore.
func NewLegacyRuleStore(cfg RuleStoreConfig, hedgeCfg hedging.Config, clientMetrics storage.ClientMetrics, loader promRules.GroupLoader, logger log.Logger) (rulestore.RuleStore, error) {
	if cfg.mock != nil {
		return cfg.mock, nil
	}

	if loader == nil {
		loader = promRules.FileLoader{}
	}

	var err error
	var client client.ObjectClient

	switch cfg.Type {
	case "configdb":
		c, err := configClient.New(cfg.ConfigDB)
		if err != nil {
			return nil, err
		}
		return configdb.NewConfigRuleStore(c), nil
	case "azure":
		client, err = azure.NewBlobStorage(&cfg.Azure, clientMetrics.AzureMetrics, hedgeCfg)
	case "gcs":
		client, err = gcp.NewGCSObjectClient(context.Background(), cfg.GCS, hedgeCfg)
	case "s3":
		client, err = aws.NewS3ObjectClient(cfg.S3, hedgeCfg)
	case "bos":
		client, err = baidubce.NewBOSObjectStorage(&cfg.BOS)
	case "swift":
		client, err = openstack.NewSwiftObjectClient(cfg.Swift, hedgeCfg)
	case "local":
		return local.NewLocalRulesClient(cfg.Local, loader)
	default:
		return nil, fmt.Errorf("unrecognized rule storage mode %v, choose one of: configdb, gcs, s3, swift, azure, local", cfg.Type)
	}

	if err != nil {
		return nil, err
	}

	return objectclient.NewRuleStore(client, loadRulesConcurrency, logger), nil
}

// NewRuleStore returns a rule store backend client based on the provided cfg.
func NewRuleStore(ctx context.Context, cfg rulestore.Config, cfgProvider bucket.TenantConfigProvider, loader promRules.GroupLoader, logger log.Logger, reg prometheus.Registerer) (rulestore.RuleStore, error) {
	if cfg.Backend == configdb.Name {
		c, err := configClient.New(cfg.ConfigDB)
		if err != nil {
			return nil, err
		}

		return configdb.NewConfigRuleStore(c), nil
	}

	if cfg.Backend == local.Name {
		return local.NewLocalRulesClient(cfg.Local, loader)
	}

	bucketClient, err := bucket.NewClient(ctx, cfg.Config, "ruler-storage", logger, reg)
	if err != nil {
		return nil, err
	}

	store := bucketclient.NewBucketRuleStore(bucketClient, cfgProvider, logger)
	if err != nil {
		return nil, err
	}

	return store, nil
}
