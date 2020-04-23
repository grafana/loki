package azure

import (
	"flag"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// Config holds the config options for an Azure backend
type Config struct {
	StorageAccountName string         `yaml:"account_name"`
	StorageAccountKey  flagext.Secret `yaml:"account_key"`
	ContainerName      string         `yaml:"container_name"`
	Endpoint           string         `yaml:"endpoint_suffix"`
	MaxRetries         int            `yaml:"max_retries"`
}

// RegisterFlags registers the flags for TSDB Azure storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.StorageAccountName, "experimental.tsdb.azure.account-name", "", "Azure storage account name")
	f.Var(&cfg.StorageAccountKey, "experimental.tsdb.azure.account-key", "Azure storage account key")
	f.StringVar(&cfg.ContainerName, "experimental.tsdb.azure.container-name", "", "Azure storage container name")
	f.StringVar(&cfg.Endpoint, "experimental.tsdb.azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The account name will be prefixed to this value to create the FQDN")
	f.IntVar(&cfg.MaxRetries, "experimental.tsdb.azure.max-retries", 20, "Number of retries for recoverable errors")
}
