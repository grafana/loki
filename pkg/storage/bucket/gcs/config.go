package gcs

import (
	"flag"
	"net/http"

	"github.com/grafana/dskit/flagext"

	bucket_http "github.com/grafana/loki/v3/pkg/storage/bucket/http"
)

// HTTPConfig stores the http.Transport configuration for the s3 minio client.
type HTTPConfig struct {
	bucket_http.Config `yaml:",inline"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`
}

// Config holds the config options for GCS backend
type Config struct {
	BucketName      string         `yaml:"bucket_name"`
	ServiceAccount  flagext.Secret `yaml:"service_account"`
	ChunkBufferSize int            `yaml:"chunk_buffer_size"`

	HTTP HTTPConfig `yaml:"http"`
}

// RegisterFlags registers the flags for GCS storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for GCS storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"gcs.bucket-name", "", "GCS bucket name")
	f.Var(&cfg.ServiceAccount, prefix+"gcs.service-account", "JSON representing either a Google Developers Console client_credentials.json file or a Google Developers service account key file. If empty, fallback to Google default logic.")
	f.IntVar(&cfg.ChunkBufferSize, prefix+"gcs.chunk-buffer-size", 0, "The size of the buffer that GCS client for each PUT request. 0 to disable buffering.")
	cfg.HTTP.RegisterFlagsWithPrefix(prefix+"s3.http.", f)
}
