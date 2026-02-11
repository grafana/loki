package bucket

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

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
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/util/constants"
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

	// validPrefixCharactersRegex allows only alphanumeric characters and dashes to prevent subtle bugs and simplify validation
	validPrefixCharactersRegex = `^[\da-zA-Z-]+$`
)

var (
	SupportedBackends = []string{S3, GCS, Azure, Swift, Filesystem, Alibaba, BOS}

	ErrUnsupportedStorageBackend        = errors.New("unsupported storage backend")
	ErrInvalidCharactersInStoragePrefix = errors.New("storage prefix contains invalid characters, it may only contain digits, English alphabet letters and dashes")

	metrics *objstore.Metrics

	// added to track the status codes by method
	bucketRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "objstore_bucket_transport_requests_total",
			Help:      "Total number of HTTP transport requests made to the bucket backend by status code and method.",
		},
		[]string{"status_code", "method"},
	)
	bucketRequestsDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "objstore_bucket_transport_hedged_requests_duration_seconds",
			Help:                            "Time spent doing requests to the bucket backend after request hedging.",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
		},
		[]string{"status_code", "method"},
	)
)

func init() {
	prometheus.MustRegister(bucketRequestsTotal)
	prometheus.MustRegister(bucketRequestsDuration)
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

	Hedging hedging.Config `yaml:"hedging"`

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
	cfg.Hedging.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.StoragePrefix, prefix+"storage-prefix", "", "Prefix for all objects stored in the backend storage. For simplicity, it may only contain digits, English alphabet letters and dashes.")
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

// hedgedClient wraps an objstore.InstrumentedBucket and performs request hedging for some read operations.
type hedgedClient struct {
	bucket, hedgedBucket objstore.InstrumentedBucket
}

var _ objstore.InstrumentedBucket = (*hedgedClient)(nil)

// NewClient creates a new bucket client based on the configured backend
func NewClient(ctx context.Context, backend string, cfg Config, name string, logger log.Logger) (objstore.InstrumentedBucket, error) {
	c, err := internalNewClient(ctx, backend, cfg, name, logger, instrumentTransport())
	if err != nil {
		return nil, err
	}

	hc := c
	if cfg.Hedging.At != 0 {
		wrapRT := func(next http.RoundTripper) http.RoundTripper {
			hedgedRT, err := cfg.Hedging.RoundTripper(next)
			if err != nil {
				panic(err)
			}
			return instrumentTransport()(hedgedRT)
		}
		hc, err = internalNewClient(ctx, backend, cfg, name, logger, wrapRT)
		if err != nil {
			return nil, err
		}
	}

	return &hedgedClient{
		bucket:       c,
		hedgedBucket: hc,
	}, nil
}

func internalNewClient(ctx context.Context, backend string, cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.InstrumentedBucket, error) {
	var (
		client objstore.Bucket
		err    error
	)

	// TODO: add support for other backends that loki already supports
	switch backend {
	case S3:
		client, err = s3.NewBucketClient(cfg.S3, name, logger, wrapRT)
	case GCS:
		client, err = gcs.NewBucketClient(ctx, cfg.GCS, name, logger, wrapRT)
	case Azure:
		client, err = azure.NewBucketClient(cfg.Azure, name, logger, wrapRT)
	case Swift:
		client, err = swift.NewBucketClient(cfg.Swift, name, logger, wrapRT)
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

func (c *hedgedClient) WithExpectedErrs(f objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	return c.bucket.WithExpectedErrs(f)
}

func (c *hedgedClient) ReaderWithExpectedErrs(f objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
	return c.hedgedBucket.ReaderWithExpectedErrs(f)
}

func (c *hedgedClient) Provider() objstore.ObjProvider {
	return c.bucket.Provider()
}

func (c *hedgedClient) Upload(ctx context.Context, name string, r io.Reader) error {
	return c.bucket.Upload(ctx, name, r)
}

func (c *hedgedClient) GetAndReplace(ctx context.Context, name string, f func(existing io.ReadCloser) (io.ReadCloser, error)) error {
	return c.bucket.GetAndReplace(ctx, name, f)
}

func (c *hedgedClient) Delete(ctx context.Context, name string) error {
	return c.bucket.Delete(ctx, name)
}

func (c *hedgedClient) Name() string {
	return c.bucket.Name()
}

func (c *hedgedClient) Close() error {
	return errors.Join(c.hedgedBucket.Close(), c.bucket.Close())
}

func (c *hedgedClient) Iter(ctx context.Context, dir string, f func(name string) error, options ...objstore.IterOption) error {
	return c.hedgedBucket.Iter(ctx, dir, f, options...)
}

func (c *hedgedClient) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	return c.hedgedBucket.IterWithAttributes(ctx, dir, f, options...)
}

func (c *hedgedClient) SupportedIterOptions() []objstore.IterOptionType {
	return c.bucket.SupportedIterOptions()
}

func (c *hedgedClient) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return c.hedgedBucket.Get(ctx, name)
}

func (c *hedgedClient) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return c.hedgedBucket.GetRange(ctx, name, off, length)
}

func (c *hedgedClient) Exists(ctx context.Context, name string) (bool, error) {
	return c.hedgedBucket.Exists(ctx, name)
}

func (c *hedgedClient) IsObjNotFoundErr(err error) bool {
	return c.bucket.IsObjNotFoundErr(err)
}

func (c *hedgedClient) IsAccessDeniedErr(err error) bool {
	return c.bucket.IsAccessDeniedErr(err)
}

func (c *hedgedClient) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return c.hedgedBucket.Attributes(ctx, name)
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
	start := time.Now()

	resp, err := i.next.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Record status code and method metrics
	statusCode := strconv.Itoa(resp.StatusCode)
	bucketRequestsTotal.WithLabelValues(statusCode, req.Method).Inc()
	bucketRequestsDuration.WithLabelValues(statusCode, req.Method).Observe(time.Since(start).Seconds())

	return resp, nil
}
