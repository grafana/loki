package azure

import (
	"flag"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/storage/bucket/http"
)

// Config holds the config options for an Azure backend
type Config struct {
	StorageAccountName string         `yaml:"account_name"`
	StorageAccountKey  flagext.Secret `yaml:"account_key"`
	ConnectionString   flagext.Secret `yaml:"connection_string"`
	ContainerName      string         `yaml:"container_name"`
	EndpointSuffix     string         `yaml:"endpoint_suffix"`
	MaxRetries         int            `yaml:"max_retries"`

	http.Config `yaml:"http"`
}

// RegisterFlags registers the flags for Azure storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for Azure storage
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.StorageAccountName, prefix+"azure.account-name", "", "Azure storage account name")
	f.Var(&cfg.StorageAccountKey, prefix+"azure.account-key", "Azure storage account key")
	f.Var(&cfg.ConnectionString, prefix+"azure.connection-string", "If `connection-string` is set, the values of `account-name` and `endpoint-suffix` values will not be used. Use this method over `account-key` if you need to authenticate via a SAS token. Or if you use the Azurite emulator.")
	f.StringVar(&cfg.ContainerName, prefix+"azure.container-name", "loki", "Azure storage container name")
	f.StringVar(&cfg.EndpointSuffix, prefix+"azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The account name will be prefixed to this value to create the FQDN")
	f.IntVar(&cfg.MaxRetries, prefix+"azure.max-retries", 20, "Number of retries for recoverable errors")
	cfg.Config.RegisterFlagsWithPrefix(prefix+"azure.", f)
}
