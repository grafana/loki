package gcs

import (
	"flag"
	"net/http"

	"github.com/grafana/dskit/flagext"
)

// Config holds the config options for GCS backend
type Config struct {
	BucketName      string         `yaml:"bucket_name"`
	ServiceAccount  flagext.Secret `yaml:"service_account" doc:"description_method=GCSServiceAccountLongDescription"`
	ChunkBufferSize int            `yaml:"chunk_buffer_size"`
	MaxRetries      int            `yaml:"max_retries"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`
}

// RegisterFlags registers the flags for GCS storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for GCS storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"gcs.bucket-name", "", "GCS bucket name")
	f.Var(&cfg.ServiceAccount, prefix+"gcs.service-account", cfg.GCSServiceAccountShortDescription())
	f.IntVar(&cfg.ChunkBufferSize, prefix+"gcs.chunk-buffer-size", 0, "The maximum size of the buffer that GCS client for a single PUT request. 0 to disable buffering.")
	f.IntVar(&cfg.MaxRetries, prefix+"gcs.max-retries", 10, "The maximum number of retries for idempotent operations. Overrides the default gcs storage client behavior if this value is greater than 0. Set this to 1 to disable retries.")
}

func (cfg *Config) GCSServiceAccountShortDescription() string {
	return "JSON either from a Google Developers Console client_credentials.json file, or a Google Developers service account key. Needs to be valid JSON, not a filesystem path."
}

func (cfg *Config) GCSServiceAccountLongDescription() string {
	return cfg.GCSServiceAccountShortDescription() +
		" If empty, fallback to Google default logic:" +
		"\n1. A JSON file whose path is specified by the GOOGLE_APPLICATION_CREDENTIALS environment variable. For workload identity federation, refer to https://cloud.google.com/iam/docs/how-to#using-workload-identity-federation on how to generate the JSON configuration file for on-prem/non-Google cloud platforms." +
		"\n2. A JSON file in a location known to the gcloud command-line tool: $HOME/.config/gcloud/application_default_credentials.json." +
		"\n3. On Google Compute Engine it fetches credentials from the metadata server."
}
