package bucket

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	objstoretracing "github.com/thanos-io/objstore/tracing/opentracing"

	"github.com/grafana/loki/v3/pkg/storage/bucket/azure"
	"github.com/grafana/loki/v3/pkg/storage/bucket/bos"
	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/bucket/oss"
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

	// Alibaba is the value for the Alibaba Cloud OSS storage backend
	Alibaba = "alibabacloud"

	// BOS is the value for the Baidu Cloud BOS storage backend
	BOS = "bos"

	// validPrefixCharactersRegex allows only alphanumeric characters to prevent subtle bugs and simplify validation
	validPrefixCharactersRegex = `^[\da-zA-Z]+$`
)

var (
	SupportedBackends = []string{S3, GCS, Azure, Swift, Filesystem, Alibaba, BOS}

	ErrUnsupportedStorageBackend        = errors.New("unsupported storage backend")
	ErrInvalidCharactersInStoragePrefix = errors.New("storage prefix contains invalid characters, it may only contain digits and English alphabet letters")

	metrics *objstore.Metrics

	// added to track the status codes by method
	bucketRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "objstore_bucket_transport_requests_total",
			Help:      "Total number of HTTP transport requests made to the bucket backend by status code and method.",
		},
		[]string{"status_code", "method"},
	)
)

func init() {
	prometheus.MustRegister(bucketRequestsTotal)
	metrics = objstore.BucketMetrics(prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer), "")
}

// Config holds configuration for accessing long-term storage.
type Config struct {
	// Backends
	S3         s3.Config         `yaml:"s3"`
	GCS        gcs.Config        `yaml:"gcs"`
	Azure      azure.Config      `yaml:"azure"`
	Swift      swift.Config      `yaml:"swift"`
	Filesystem filesystem.Config `yaml:"filesystem"`
	Alibaba    oss.Config        `yaml:"alibaba"`
	BOS        bos.Config        `yaml:"bos"`

	StoragePrefix string `yaml:"storage_prefix"`

	// Used to inject additional backends into the config. Allows for this config to
	// be embedded in multiple contexts and support non-object storage based backends.
	ExtraBackends []string `yaml:"-"`

	// Not used internally, meant to allow callers to wrap Buckets
	// created using this config
	Middlewares []func(objstore.InstrumentedBucket) (objstore.InstrumentedBucket, error) `yaml:"-"`
}

// Returns the SupportedBackends for the package and any custom backends injected into the config.
func (cfg *Config) SupportedBackends() []string {
	return append(SupportedBackends, cfg.ExtraBackends...)
}

// RegisterFlags registers the backend storage config.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *Config) RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir string, f *flag.FlagSet) {
	cfg.GCS.RegisterFlagsWithPrefix(prefix, f)
	cfg.S3.RegisterFlagsWithPrefix(prefix, f)
	cfg.Azure.RegisterFlagsWithPrefix(prefix, f)
	cfg.Swift.RegisterFlagsWithPrefix(prefix, f)
	cfg.Filesystem.RegisterFlagsWithPrefixAndDefaultDirectory(prefix, dir, f)
	cfg.Alibaba.RegisterFlagsWithPrefix(prefix, f)
	cfg.BOS.RegisterFlagsWithPrefix(prefix, f)
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

	if err := cfg.S3.Validate(); err != nil {
		return err
	}

	return nil
}

type ConfigWithNamedStores struct {
	Config      `yaml:",inline"`
	NamedStores NamedStores `yaml:"named_stores"`
}

func (cfg *ConfigWithNamedStores) Validate() error {
	if err := cfg.Config.Validate(); err != nil {
		return err
	}

	return cfg.NamedStores.Validate()
}

func (cfg *Config) disableRetries(backend string) error {
	switch backend {
	case S3:
		cfg.S3.MaxRetries = 1
	case GCS:
		cfg.GCS.MaxRetries = 1
	case Azure:
		cfg.Azure.MaxRetries = 1
	case Swift:
		cfg.Swift.MaxRetries = 1
	case Filesystem, Alibaba, BOS:
		// do nothing
	default:
		return fmt.Errorf("cannot disable retries for backend: %s", backend)
	}

	return nil
}

func (cfg *Config) configureTransport(backend string, rt http.RoundTripper) error {
	switch backend {
	case S3:
		cfg.S3.HTTP.Transport = rt
	case GCS:
		cfg.GCS.Transport = rt
	case Azure:
		cfg.Azure.Transport = rt
	case Swift:
		cfg.Swift.HTTP.Transport = rt
	case Filesystem, Alibaba, BOS:
		// do nothing
	default:
		return fmt.Errorf("cannot configure transport for backend: %s", backend)
	}

	return nil
}

// NewClient creates a new bucket client based on the configured backend
func NewClient(ctx context.Context, backend string, cfg Config, name string, logger log.Logger) (objstore.InstrumentedBucket, error) {
	var (
		client objstore.Bucket
		err    error
	)

	// TODO: add support for other backends that loki already supports
	switch backend {
	case S3:
		client, err = s3.NewBucketClient(cfg.S3, name, logger, instrumentTransport())
	case GCS:
		client, err = gcs.NewBucketClient(ctx, cfg.GCS, name, logger, instrumentTransport())
	case Azure:
		client, err = azure.NewBucketClient(cfg.Azure, name, logger, instrumentTransport())
	case Swift:
		client, err = swift.NewBucketClient(cfg.Swift, name, logger, instrumentTransport())
	case Filesystem:
		client, err = filesystem.NewBucketClient(cfg.Filesystem)
	case Alibaba:
		client, err = oss.NewBucketClient(cfg.Alibaba, name, logger)
	case BOS:
		client, err = bos.NewBucketClient(cfg.BOS, name, logger)
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

type instrumentedRoundTripper struct {
	next http.RoundTripper
}

func instrumentTransport() func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &instrumentedRoundTripper{next: rt}
	}
}

func (i *instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := i.next.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Record status code and method metrics
	bucketRequestsTotal.WithLabelValues(strconv.Itoa(resp.StatusCode), req.Method).Inc()
	return resp, nil
}
