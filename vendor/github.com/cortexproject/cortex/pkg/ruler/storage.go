package ruler

import (
	"context"
	"flag"
	"fmt"

	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/chunk/openstack"
	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/ruler/rules/local"
	"github.com/cortexproject/cortex/pkg/ruler/rules/objectclient"
)

// RuleStoreConfig configures a rule store.
type RuleStoreConfig struct {
	Type     string        `yaml:"type"`
	ConfigDB client.Config `yaml:"configdb"`

	// Object Storage Configs
	Azure azure.BlobStorageConfig `yaml:"azure"`
	GCS   gcp.GCSConfig           `yaml:"gcs"`
	S3    aws.S3Config            `yaml:"s3"`
	Swift openstack.SwiftConfig   `yaml:"swift"`
	Local local.Config            `yaml:"local"`

	mock rules.RuleStore `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *RuleStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ConfigDB.RegisterFlagsWithPrefix("ruler.", f)
	cfg.Azure.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.GCS.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.S3.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.Swift.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.Local.RegisterFlagsWithPrefix("ruler.storage.", f)

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
	return nil
}

// IsDefaults returns true if the storage options have not been set
func (cfg *RuleStoreConfig) IsDefaults() bool {
	return cfg.Type == "configdb" && cfg.ConfigDB.ConfigsAPIURL.URL == nil
}

// NewRuleStorage returns a new rule storage backend poller and store
func NewRuleStorage(cfg RuleStoreConfig) (rules.RuleStore, error) {
	if cfg.mock != nil {
		return cfg.mock, nil
	}

	switch cfg.Type {
	case "configdb":
		c, err := client.New(cfg.ConfigDB)

		if err != nil {
			return nil, err
		}

		return rules.NewConfigRuleStore(c), nil
	case "azure":
		return newObjRuleStore(azure.NewBlobStorage(&cfg.Azure, ""))
	case "gcs":
		return newObjRuleStore(gcp.NewGCSObjectClient(context.Background(), cfg.GCS, ""))
	case "s3":
		return newObjRuleStore(aws.NewS3ObjectClient(cfg.S3, ""))
	case "swift":
		return newObjRuleStore(openstack.NewSwiftObjectClient(cfg.Swift, ""))
	case "local":
		return local.NewLocalRulesClient(cfg.Local)
	default:
		return nil, fmt.Errorf("Unrecognized rule storage mode %v, choose one of: configdb, gcs, s3, swift, azure, local", cfg.Type)
	}
}

func newObjRuleStore(client chunk.ObjectClient, err error) (rules.RuleStore, error) {
	if err != nil {
		return nil, err
	}
	return objectclient.NewRuleStore(client), nil
}
