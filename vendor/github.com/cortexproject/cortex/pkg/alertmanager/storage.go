package alertmanager

import (
	"context"
	"flag"
	"fmt"

	"github.com/cortexproject/cortex/pkg/alertmanager/alerts"
	"github.com/cortexproject/cortex/pkg/alertmanager/alerts/configdb"
	"github.com/cortexproject/cortex/pkg/alertmanager/alerts/local"
	"github.com/cortexproject/cortex/pkg/configs/client"
)

// AlertStore stores and configures users rule configs
type AlertStore interface {
	ListAlertConfigs(ctx context.Context) (map[string]alerts.AlertConfigDesc, error)
}

// AlertStoreConfig configures the alertmanager backend
type AlertStoreConfig struct {
	Type     string            `yaml:"type"`
	ConfigDB client.Config     `yaml:"configdb"`
	Local    local.StoreConfig `yaml:"local"`
}

// RegisterFlags registers flags.
func (cfg *AlertStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Local.RegisterFlags(f)
	cfg.ConfigDB.RegisterFlagsWithPrefix("alertmanager.", f)
	f.StringVar(&cfg.Type, "alertmanager.storage.type", "configdb", "Type of backend to use to store alertmanager configs. Supported values are: \"configdb\", \"local\".")
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
	default:
		return nil, fmt.Errorf("unrecognized alertmanager storage backend %v, choose one of:  \"configdb\", \"local\"", cfg.Type)
	}
}
