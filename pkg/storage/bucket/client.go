package bucket

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"regexp"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	objstoretracing "github.com/thanos-io/objstore/tracing/opentracing"

	"github.com/grafana/loki/v3/pkg/storage/bucket/azure"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/bucket/swift"
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

	// validPrefixCharactersRegex allows only alphanumeric characters to prevent subtle bugs and simplify validation
	validPrefixCharactersRegex = `^[\da-zA-Z]+$`
)

var (
	SupportedBackends = []string{S3, GCS, Azure, Swift, Filesystem}

	ErrUnsupportedStorageBackend        = errors.New("unsupported storage backend")
	ErrInvalidCharactersInStoragePrefix = errors.New("storage prefix contains invalid characters, it may only contain digits and English alphabet letters")

	metrics = objstore.BucketMetrics(prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer), "")
)

// StorageBackendConfig holds configuration for accessing long-term storage.
type StorageBackendConfig struct {
	// Backends
	S3         s3.Config         `yaml:"s3"`
	GCS        gcs.Config        `yaml:"gcs"`
	Azure      azure.Config      `yaml:"azure"`
	Swift      swift.Config      `yaml:"swift"`
	Filesystem filesystem.Config `yaml:"filesystem"`

	NamedStores NamedStores `yaml:"named_stores"`

	// Used to inject additional backends into the config. Allows for this config to
	// be embedded in multiple contexts and support non-object storage based backends.
	ExtraBackends []string `yaml:"-"`
}

// Returns the SupportedBackends for the package and any custom backends injected into the config.
func (cfg *StorageBackendConfig) SupportedBackends() []string {
	return append(SupportedBackends, cfg.ExtraBackends...)
}

// RegisterFlags registers the backend storage config.
func (cfg *StorageBackendConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *StorageBackendConfig) RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir string, f *flag.FlagSet) {
	cfg.GCS.RegisterFlagsWithPrefix(prefix, f)
	cfg.S3.RegisterFlagsWithPrefix(prefix, f)
	cfg.Azure.RegisterFlagsWithPrefix(prefix, f)
	cfg.Swift.RegisterFlagsWithPrefix(prefix, f)
	cfg.Filesystem.RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir, f)
}

func (cfg *StorageBackendConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefixAndDefaultDirectory(prefix, "", f)
}

func (cfg *StorageBackendConfig) Validate() error {
	if err := cfg.S3.Validate(); err != nil {
		return err
	}

	return cfg.NamedStores.Validate()
}

// Config holds configuration for accessing long-term storage.
type Config struct {
	StorageBackendConfig `yaml:",inline"`
	StoragePrefix        string `yaml:"storage_prefix"`

	// Not used internally, meant to allow callers to wrap Buckets
	// created using this config
	Middlewares []func(objstore.InstrumentedBucket) (objstore.InstrumentedBucket, error) `yaml:"-"`
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *Config) RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir string, f *flag.FlagSet) {
	cfg.StorageBackendConfig.RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir, f)
	f.StringVar(&cfg.StoragePrefix, prefix+"storage-prefix", "", "Prefix for all objects stored in the backend storage. For simplicity, it may only contain digits and English alphabet letters.")
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefixAndDefaultDirectory(prefix, "", f)
}

func (cfg *Config) Validate() error {
	if cfg.StoragePrefix != "" {
		acceptablePrefixCharacters := regexp.MustCompile(validPrefixCharactersRegex)
		if !acceptablePrefixCharacters.MatchString(cfg.StoragePrefix) {
			return ErrInvalidCharactersInStoragePrefix
		}
	}

	if err := cfg.StorageBackendConfig.Validate(); err != nil {
		return err
	}

	return cfg.NamedStores.Validate()
}

// NewClient creates a new bucket client based on the configured backend
func NewClient(ctx context.Context, backend string, cfg Config, name string, logger log.Logger) (objstore.InstrumentedBucket, error) {
	var (
		storeType  = backend
		namedStore bool

		client objstore.Bucket
		err    error
	)

	if st, ok := cfg.NamedStores.storeType[backend]; ok {
		namedStore = true
		storeType = st
	}

	// TODO: add support for other backends that loki already supports
	switch storeType {
	case S3:
		s3Cfg := cfg.S3
		if namedStore {
			nsCfg, ok := cfg.NamedStores.S3[backend]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named s3 storage config %s", backend)
			}

			s3Cfg = (s3.Config)(nsCfg)
		}

		client, err = s3.NewBucketClient(s3Cfg, name, logger)
	case GCS:
		gcsCfg := cfg.GCS
		if namedStore {
			nsCfg, ok := cfg.NamedStores.GCS[backend]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named gcs storage config %s", backend)
			}

			gcsCfg = (gcs.Config)(nsCfg)
		}

		client, err = gcs.NewBucketClient(ctx, gcsCfg, name, logger)
	case Azure:
		azureCfg := cfg.Azure
		if namedStore {
			nsCfg, ok := cfg.NamedStores.Azure[backend]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named azure storage config %s", backend)
			}

			azureCfg = (azure.Config)(nsCfg)
		}

		client, err = azure.NewBucketClient(azureCfg, name, logger)
	case Swift:
		swiftCfg := cfg.Swift
		if namedStore {
			nsCfg, ok := cfg.NamedStores.Swift[backend]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named swift storage config %s", backend)
			}

			swiftCfg = (swift.Config)(nsCfg)
		}

		client, err = swift.NewBucketClient(swiftCfg, name, logger)
	case Filesystem:
		fsCfg := cfg.Filesystem
		if namedStore {
			nsCfg, ok := cfg.NamedStores.Filesystem[backend]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named swift storage config %s", backend)
			}

			fsCfg = (filesystem.Config)(nsCfg)
		}

		client, err = filesystem.NewBucketClient(fsCfg)
	default:
		return nil, ErrUnsupportedStorageBackend
	}

	if err != nil {
		return nil, err
	}

	if cfg.StoragePrefix != "" {
		client = NewPrefixedBucketClient(client, cfg.StoragePrefix)
	}

	instrumentedClient := objstoretracing.WrapWithTraces(objstore.WrapWith(client, metrics))

	// Wrap the client with any provided middleware
	for _, wrap := range cfg.Middlewares {
		instrumentedClient, err = wrap(instrumentedClient)
		if err != nil {
			return nil, err
		}
	}

	return instrumentedClient, nil
}
