package ruler

import (
	"context"
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/configs/client"
	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/cortexproject/cortex/pkg/ruler/rules/objectclient"
)

// RuleStoreConfig conigures a rule store
type RuleStoreConfig struct {
	Type     string        `yaml:"type"`
	ConfigDB client.Config `yaml:"configdb"`

	// Object Storage Configs
	Azure azure.BlobStorageConfig `yaml:"azure"`
	GCS   gcp.GCSConfig           `yaml:"gcs"`
	S3    aws.S3Config            `yaml:"s3"`

	mock rules.RuleStore `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *RuleStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ConfigDB.RegisterFlagsWithPrefix("ruler.", f)
	cfg.Azure.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.GCS.RegisterFlagsWithPrefix("ruler.storage.", f)
	cfg.S3.RegisterFlagsWithPrefix("ruler.storage.", f)
	f.StringVar(&cfg.Type, "ruler.storage.type", "configdb", "Method to use for backend rule storage (configdb, azure, gcs, s3)")
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
	default:
		return nil, fmt.Errorf("Unrecognized rule storage mode %v, choose one of: configdb, gcs", cfg.Type)
	}
}

func newObjRuleStore(client chunk.ObjectClient, err error) (rules.RuleStore, error) {
	if err != nil {
		return nil, err
	}
	return objectclient.NewRuleStore(client), nil
}
