package alertmanager

import (
	"context"
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/alertmanager/alerts/configdb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alerts/local"
	"github.com/cortexproject/cortex/pkg/alertmanager/alerts/objectclient"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/configs/client"
)

// AlertStore stores and configures users rule configs
type AlertStore interface {
	ListAlertConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error)
	GetAlertConfig(ctx context.Context, user string) (alerts.AlertConfigDesc, error)
	SetAlertConfig(ctx context.Context, cfg alerts.AlertConfigDesc) error
	DeleteAlertConfig(ctx context.Context, user string) error
}

// AlertStoreConfig configures the alertmanager backend
type AlertStoreConfig struct {
	Type     string            `yaml:"type"`
	ConfigDB client.Config     `yaml:"configdb"`
	Local    local.StoreConfig `yaml:"local"`

	GCS gcp.GCSConfig `yaml:"gcs"`
	S3  aws.S3Config  `yaml:"s3"`
}

// RegisterFlags registers flags.
func (cfg *AlertStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Local.RegisterFlags(f)
	cfg.ConfigDB.RegisterFlagsWithPrefix("alertmanager.", f)
	f.StringVar(&cfg.Type, "alertmanager.storage.type", "configdb", "Type of backend to use to store alertmanager configs. Supported values are: \"configdb\", \"gcs\", \"s3\", \"local\".")

	cfg.GCS.RegisterFlagsWithPrefix("alertmanager.storage.", f)
	cfg.S3.RegisterFlagsWithPrefix("alertmanager.storage.", f)
}

// NewAlertStore returns a new rule storage backend poller and store
func NewAlertStore(cfg AlertStoreConfig) (AlertStore, error) {
	switch cfg.Type {
	case "configdb":
		c, err := client.New(cfg.ConfigDB)
		if err != nil {
			return nil, err
		}
		return configdb.NewStore(c), nil
	case "local":
		return local.NewStore(cfg.Local)
	case "gcs":
		return newObjAlertStore(gcp.NewGCSObjectClient(context.Background(), cfg.GCS, ""))
	case "s3":
		return newObjAlertStore(aws.NewS3ObjectClient(cfg.S3, ""))
	default:
		return nil, fmt.Errorf("unrecognized alertmanager storage backend %v, choose one of: azure, configdb, gcs, local, s3", cfg.Type)
	}
}

func newObjAlertStore(client chunk.ObjectClient, err error) (AlertStore, error) {
	if err != nil {
		return nil, err
	}
	return objectclient.NewAlertStore(client), nil
}
