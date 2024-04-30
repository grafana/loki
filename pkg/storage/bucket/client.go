package bucket

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket/azure"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/bucket/swift"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	// S3 is the value for the S3 storage backend.
	S3 = "s3"

	// GCS is the value for the GCS storage backend.
	GCS = "gcs"

	// Azure is the value for the Azure storage backend.
	Azure = "azure"

	// Swift is the value for the Openstack Swift storage backend.
	Swift = "swift"

	// Filesystem is the value for the filesystem storage backend.
	Filesystem = "filesystem"
)

var (
	SupportedBackends = []string{S3, GCS, Azure, Swift, Filesystem}

	ErrUnsupportedStorageBackend = errors.New("unsupported storage backend")
)

// Config holds configuration for accessing long-term storage.
type Config struct {
	Backend string `yaml:"backend"`
	// Backends
	S3         s3.Config         `yaml:"s3"`
	GCS        gcs.Config        `yaml:"gcs"`
	Azure      azure.Config      `yaml:"azure"`
	Swift      swift.Config      `yaml:"swift"`
	Filesystem filesystem.Config `yaml:"filesystem"`

	// Not used internally, meant to allow callers to wrap Buckets
	// created using this config
	Middlewares []func(objstore.Bucket) (objstore.Bucket, error) `yaml:"-"`

	// Used to inject additional backends into the config. Allows for this config to
	// be embedded in multiple contexts and support non-object storage based backends.
	ExtraBackends []string `yaml:"-"`
}

// Returns the supportedBackends for the package and any custom backends injected into the config.
func (cfg *Config) supportedBackends() []string {
	return append(SupportedBackends, cfg.ExtraBackends...)
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.S3.RegisterFlagsWithPrefix(prefix, f)
	cfg.GCS.RegisterFlagsWithPrefix(prefix, f)
	cfg.Azure.RegisterFlagsWithPrefix(prefix, f)
	cfg.Swift.RegisterFlagsWithPrefix(prefix, f)
	cfg.Filesystem.RegisterFlagsWithPrefix(prefix, f)

	f.StringVar(&cfg.Backend, prefix+"backend", S3, fmt.Sprintf("Backend storage to use. Supported backends are: %s.", strings.Join(cfg.supportedBackends(), ", ")))
}

func (cfg *Config) Validate() error {
	if !util.StringsContain(cfg.supportedBackends(), cfg.Backend) {
		return ErrUnsupportedStorageBackend
	}

	if cfg.Backend == S3 {
		if err := cfg.S3.Validate(); err != nil {
			return err
		}
	}

	return nil
}
