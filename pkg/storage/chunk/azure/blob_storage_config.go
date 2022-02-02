package azure

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"
)

// BlobStorageConfig defines the configurable flags that can be defined when using azure blob storage.
type BlobStorageConfig struct {
	Environment        string         `yaml:"environment"`
	ContainerName      string         `yaml:"container_name"`
	AccountName        string         `yaml:"account_name"`
	AccountKey         flagext.Secret `yaml:"account_key"`
	DownloadBufferSize int            `yaml:"download_buffer_size"`
	UploadBufferSize   int            `yaml:"upload_buffer_size"`
	UploadBufferCount  int            `yaml:"upload_buffer_count"`
	RequestTimeout     time.Duration  `yaml:"request_timeout"`
	MaxRetries         int            `yaml:"max_retries"`
	MinRetryDelay      time.Duration  `yaml:"min_retry_delay"`
	MaxRetryDelay      time.Duration  `yaml:"max_retry_delay"`
	UseManagedIdentity bool           `yaml:"use_managed_identity"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Environment, prefix+"azure.environment", azureGlobal, fmt.Sprintf("Azure Cloud environment. Supported values are: %s.", strings.Join(supportedEnvironments, ", ")))
	f.StringVar(&c.ContainerName, prefix+"azure.container-name", "cortex", "Name of the blob container used to store chunks. This container must be created before running cortex.")
	f.StringVar(&c.AccountName, prefix+"azure.account-name", "", "The Microsoft Azure account name to be used")
	f.Var(&c.AccountKey, prefix+"azure.account-key", "The Microsoft Azure account key to use.")
	f.DurationVar(&c.RequestTimeout, prefix+"azure.request-timeout", 30*time.Second, "Timeout for requests made against azure blob storage.")
	f.IntVar(&c.DownloadBufferSize, prefix+"azure.download-buffer-size", 512000, "Preallocated buffer size for downloads.")
	f.IntVar(&c.UploadBufferSize, prefix+"azure.upload-buffer-size", 256000, "Preallocated buffer size for uploads.")
	f.IntVar(&c.UploadBufferCount, prefix+"azure.download-buffer-count", 1, "Number of buffers used to used to upload a chunk.")
	f.IntVar(&c.MaxRetries, prefix+"azure.max-retries", 5, "Number of retries for a request which times out.")
	f.DurationVar(&c.MinRetryDelay, prefix+"azure.min-retry-delay", 10*time.Millisecond, "Minimum time to wait before retrying a request.")
	f.DurationVar(&c.MaxRetryDelay, prefix+"azure.max-retry-delay", 500*time.Millisecond, "Maximum time to wait before retrying a request.")
	f.BoolVar(&c.UseManagedIdentity, prefix+"azure.use-managed-identity", false, "Use Managed Identity or not.")
}
