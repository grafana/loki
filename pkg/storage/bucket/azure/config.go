package azure

import (
	"flag"
	"time"

	"net/http"

	"github.com/grafana/dskit/flagext"

	bucket_http "github.com/grafana/loki/v3/pkg/storage/bucket/http"
)

// HTTPConfig stores the http.Transport configuration for the blob storage client.
type HTTPConfig struct {
	bucket_http.Config `yaml:",inline"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`
}

// Config holds the config options for an Azure backend
type Config struct {
	StorageAccountName      string         `yaml:"account_name"`
	StorageAccountKey       flagext.Secret `yaml:"account_key"`
	StorageConnectionString flagext.Secret `yaml:"connection_string"`
	ContainerName           string         `yaml:"container_name"`
	Endpoint                string         `yaml:"endpoint_suffix"`
	UserAssignedID          string         `yaml:"user_assigned_id"`
	MaxRetries              int            `yaml:"max_retries"`
	MaxRetryDelay           time.Duration  `yaml:"max_retry_delay"`

	HTTP HTTPConfig `yaml:"http"`
}

// RegisterFlags registers the flags for Azure storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for Azure storage
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.StorageAccountName, prefix+"azure.account-name", "", "Azure storage account name")
	f.Var(&cfg.StorageAccountKey, prefix+"azure.account-key", "Azure storage account key")
	f.Var(&cfg.StorageConnectionString, prefix+"azure.connection-string", "If `connection-string` is set, the values of `account-name` and `endpoint-suffix` values will not be used. Use this method over `account-key` if you need to authenticate via a SAS token. Or if you use the Azurite emulator.")
	f.StringVar(&cfg.ContainerName, prefix+"azure.container-name", "loki", "Azure storage container name")
	f.StringVar(&cfg.Endpoint, prefix+"azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The account name will be prefixed to this value to create the FQDN")
	f.StringVar(&cfg.UserAssignedID, prefix+"azure.user-assigned-id", "", "User assigned identity ID to authenticate to the Azure storage account.")
	f.IntVar(&cfg.MaxRetries, prefix+"azure.max-retries", 20, "Number of retries for recoverable errors")
	f.DurationVar(&cfg.MaxRetryDelay, prefix+"azure.max-retry-delay", 500*time.Millisecond, "Maximum time to wait before retrying a request.")
	cfg.HTTP.RegisterFlagsWithPrefix(prefix+"azure.http", f)
}
