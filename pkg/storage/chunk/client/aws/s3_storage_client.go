package aws

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/aws/smithy-go"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	amnet "k8s.io/apimachinery/pkg/util/net"

	bucket_s3 "github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	clientutil "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	storageawscommon "github.com/grafana/loki/v3/pkg/storage/common/aws"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_instrument "github.com/grafana/loki/v3/pkg/util/instrument"
)

var tracer = otel.Tracer("pkg/storage/chunk/client/awsd")

const (
	SignatureVersionV4 = "v4"
)

var (
	supportedSignatureVersions     = []string{SignatureVersionV4}
	errUnsupportedSignatureVersion = errors.New("unsupported signature version")
)

var s3RequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "s3_request_duration_seconds",
	Help:      "Time spent doing S3 requests.",
	Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
}, []string{"operation", "status_code"}))

// InjectRequestMiddleware gives users of this client the ability to make arbitrary
// changes to outgoing requests.
type InjectRequestMiddleware func(next http.RoundTripper) http.RoundTripper

func init() {
	s3RequestDuration.Register()
}

// S3Config specifies config for storing chunks on AWS S3.
type S3Config struct {
	S3               flagext.URLValue
	S3ForcePathStyle bool

	BucketNames      string
	Endpoint         string              `yaml:"endpoint"`
	Region           string              `yaml:"region"`
	AccessKeyID      string              `yaml:"access_key_id"`
	SecretAccessKey  flagext.Secret      `yaml:"secret_access_key"`
	SessionToken     flagext.Secret      `yaml:"session_token"`
	Insecure         bool                `yaml:"insecure"`
	ChunkDelimiter   string              `yaml:"chunk_delimiter"`
	HTTPConfig       HTTPConfig          `yaml:"http_config"`
	SignatureVersion string              `yaml:"signature_version"`
	StorageClass     string              `yaml:"storage_class"`
	SSEConfig        bucket_s3.SSEConfig `yaml:"sse"`
	BackoffConfig    backoff.Config      `yaml:"backoff_config" doc:"description=Configures back off when S3 get Object."`
	DisableDualstack bool                `yaml:"disable_dualstack"`

	Inject InjectRequestMiddleware `yaml:"-"`
}

// HTTPConfig stores the http.Transport configuration
type HTTPConfig struct {
	Timeout               time.Duration `yaml:"timeout"`
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool          `yaml:"insecure_skip_verify"`
	CAFile                string        `yaml:"ca_file"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *S3Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet with a specified prefix
func (cfg *S3Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&cfg.S3, prefix+"s3.url", "S3 endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<bucket-name> to use a mock in-memory implementation.")
	f.BoolVar(&cfg.S3ForcePathStyle, prefix+"s3.force-path-style", false, "Set this to `true` to force the request to use path-style addressing.")
	f.StringVar(&cfg.BucketNames, prefix+"s3.buckets", "", "Comma separated list of bucket names to evenly distribute chunks over. Overrides any buckets specified in s3.url flag")

	f.StringVar(&cfg.Endpoint, prefix+"s3.endpoint", "", "S3 Endpoint to connect to.")
	f.StringVar(&cfg.Region, prefix+"s3.region", "", "AWS region to use.")
	f.StringVar(&cfg.AccessKeyID, prefix+"s3.access-key-id", "", "AWS Access Key ID")
	f.Var(&cfg.SecretAccessKey, prefix+"s3.secret-access-key", "AWS Secret Access Key")
	f.Var(&cfg.SessionToken, prefix+"s3.session-token", "AWS Session Token")
	f.BoolVar(&cfg.Insecure, prefix+"s3.insecure", false, "Disable https on s3 connection.")
	f.StringVar(&cfg.ChunkDelimiter, prefix+"s3.chunk-delimiter", "", "Delimiter used to replace the default delimiter ':' in chunk IDs when storing chunks. This is mainly intended when you run a MinIO instance on a Windows machine. You should not change this value inflight.")
	f.BoolVar(&cfg.DisableDualstack, prefix+"s3.disable-dualstack", false, "Disable forcing S3 dualstack endpoint usage.")

	cfg.SSEConfig.RegisterFlagsWithPrefix(prefix+"s3.sse.", f)

	f.DurationVar(&cfg.HTTPConfig.IdleConnTimeout, prefix+"s3.http.idle-conn-timeout", 90*time.Second, "The maximum amount of time an idle connection will be held open.")
	f.DurationVar(&cfg.HTTPConfig.Timeout, prefix+"s3.http.timeout", 0, "Timeout specifies a time limit for requests made by s3 Client.")
	f.DurationVar(&cfg.HTTPConfig.ResponseHeaderTimeout, prefix+"s3.http.response-header-timeout", 0, "If non-zero, specifies the amount of time to wait for a server's response headers after fully writing the request.")
	f.BoolVar(&cfg.HTTPConfig.InsecureSkipVerify, prefix+"s3.http.insecure-skip-verify", false, "Set to true to skip verifying the certificate chain and hostname.")
	f.StringVar(&cfg.HTTPConfig.CAFile, prefix+"s3.http.ca-file", "", "Path to the trusted CA file that signed the SSL certificate of the S3 endpoint.")
	f.StringVar(&cfg.SignatureVersion, prefix+"s3.signature-version", SignatureVersionV4, fmt.Sprintf("The signature version to use for authenticating against S3. Supported values are: %s.", strings.Join(supportedSignatureVersions, ", ")))
	f.StringVar(&cfg.StorageClass, prefix+"s3.storage-class", storageawscommon.StorageClassStandard, fmt.Sprintf("The S3 storage class which objects will use. Supported values are: %s.", strings.Join(storageawscommon.SupportedStorageClasses, ", ")))

	f.DurationVar(&cfg.BackoffConfig.MinBackoff, prefix+"s3.min-backoff", 100*time.Millisecond, "Minimum backoff time when s3 get Object")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, prefix+"s3.max-backoff", 3*time.Second, "Maximum backoff time when s3 get Object")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, prefix+"s3.max-retries", 5, "Maximum number of times to retry for s3 GetObject or ObjectExists")
}

// Validate config and returns error on failure
func (cfg *S3Config) Validate() error {
	if !util.StringsContain(supportedSignatureVersions, cfg.SignatureVersion) {
		return errUnsupportedSignatureVersion
	}

	return storageawscommon.ValidateStorageClass(cfg.StorageClass)
}

type s3Deleter interface {
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3Getter interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

type s3Putter interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type s3Interface interface {
	s3.HeadObjectAPIClient
	s3Deleter
	s3Getter
	s3Putter
	s3.ListObjectsV2APIClient
}

type S3ObjectClient struct {
	cfg S3Config

	bucketNames []string
	S3          s3Interface
	hedgedS3    s3Interface
	sseConfig   *SSEParsedConfig
}

// NewS3ObjectClient makes a new S3-backed ObjectClient.
func NewS3ObjectClient(cfg S3Config, hedgingCfg hedging.Config) (*S3ObjectClient, error) {
	bucketNames, err := buckets(cfg)
	if err != nil {
		return nil, err
	}
	s3Client, err := buildS3Client(cfg, hedgingCfg, false)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build s3 config")
	}
	s3ClientHedging, err := buildS3Client(cfg, hedgingCfg, true)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build s3 config")
	}

	sseCfg, err := buildSSEParsedConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build SSE config")
	}

	client := S3ObjectClient{
		cfg:         cfg,
		S3:          s3Client,
		hedgedS3:    s3ClientHedging,
		bucketNames: bucketNames,
		sseConfig:   sseCfg,
	}
	return &client, nil
}

func buildSSEParsedConfig(cfg S3Config) (*SSEParsedConfig, error) {
	if cfg.SSEConfig.Type != "" {
		return NewSSEParsedConfig(cfg.SSEConfig)
	}

	return nil, nil
}

func buildS3Client(cfg S3Config, hedgingCfg hedging.Config, hedging bool) (*s3.Client, error) {
	s3Options := s3.Options{}
	var err error

	// if an s3 url is passed use it to initialize the s3Config and then override with any additional params
	if cfg.S3.URL != nil {
		key, secret, session, err := CredentialsFromURL(cfg.S3.URL)
		if err != nil {
			return nil, err
		}
		s3Options.Credentials = credentials.NewStaticCredentialsProvider(key, secret, session)
	} else {
		s3Options.Region = "dummy"
	}

	if cfg.DisableDualstack {
		s3Options.EndpointOptions.UseDualStackEndpoint = aws.DualStackEndpointStateDisabled
	}
	s3Options.RetryMaxAttempts = 0                // We do our own retries, so we can monitor them
	s3Options.UsePathStyle = cfg.S3ForcePathStyle // support for Path Style S3 url if has the flag

	if cfg.Endpoint != "" {
		s3Options.BaseEndpoint = &cfg.Endpoint
	}

	if cfg.Insecure {
		s3Options.EndpointOptions.DisableHTTPS = true
	}

	if cfg.Region != "" {
		s3Options.Region = cfg.Region
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() == "" ||
		cfg.AccessKeyID == "" && cfg.SecretAccessKey.String() != "" {
		return nil, errors.New("must supply both an Access Key ID and Secret Access Key or neither")
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() != "" {
		s3Options.Credentials = credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey.String(), cfg.SessionToken.String())
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.HTTPConfig.InsecureSkipVerify, //#nosec G402 -- User has explicitly requested to disable TLS -- nosemgrep: tls-with-insecure-cipher
	}

	if cfg.HTTPConfig.CAFile != "" {
		tlsConfig.RootCAs = x509.NewCertPool()
		data, err := os.ReadFile(cfg.HTTPConfig.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs.AppendCertsFromPEM(data)
	}

	// While extending S3 configuration this http config was copied in order to
	// to maintain backwards compatibility with previous versions of Cortex while providing
	// more flexible configuration of the http client
	// https://github.com/weaveworks/common/blob/4b1847531bc94f54ce5cf210a771b2a86cd34118/aws/config.go#L23
	transport := http.RoundTripper(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: !cfg.DisableDualstack,
		}).DialContext,
		MaxIdleConns:          200,
		IdleConnTimeout:       cfg.HTTPConfig.IdleConnTimeout,
		MaxIdleConnsPerHost:   200,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: cfg.HTTPConfig.ResponseHeaderTimeout,
		TLSClientConfig:       tlsConfig,
	})

	if cfg.Inject != nil {
		transport = cfg.Inject(transport)
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.HTTPConfig.Timeout,
	}

	if hedging {
		httpClient, err = hedgingCfg.ClientWithRegisterer(httpClient, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	s3Options.HTTPClient = httpClient
	s3Client := s3.New(s3Options)

	return s3Client, nil
}

func buckets(cfg S3Config) ([]string, error) {
	// bucketnames
	var bucketNames []string
	if cfg.S3.URL != nil {
		bucketNames = []string{strings.TrimPrefix(cfg.S3.Path, "/")}
	}

	if cfg.BucketNames != "" {
		bucketNames = strings.Split(cfg.BucketNames, ",") // comma separated list of bucket names
	}

	if len(bucketNames) == 0 {
		return nil, errors.New("at least one bucket name must be specified")
	}
	return bucketNames, nil
}

// Stop fulfills the chunk.ObjectClient interface
func (a *S3ObjectClient) Stop() {}

func (a *S3ObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := a.objectAttributes(ctx, objectKey, "S3.ObjectExists"); err != nil {
		if a.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *S3ObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return a.objectAttributes(ctx, objectKey, "S3.GetAttributes")
}

func (a *S3ObjectClient) objectAttributes(ctx context.Context, objectKey, method string) (client.ObjectAttributes, error) {
	var lastErr error
	var objectSize int64

	retries := backoff.New(ctx, a.cfg.BackoffConfig)
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return client.ObjectAttributes{}, errors.Wrap(ctx.Err(), "ctx related error during s3 objectExists")
		}
		lastErr = instrument.CollectedRequest(ctx, method, s3RequestDuration, instrument.ErrorCode, func(_ context.Context) error {
			headObjectInput := &s3.HeadObjectInput{
				Bucket: aws.String(a.bucketFromKey(objectKey)),
				Key:    aws.String(a.convertObjectKey(objectKey, true)),
			}
			headOutput, requestErr := a.S3.HeadObject(ctx, headObjectInput)
			if requestErr != nil {
				return requestErr
			}
			if headOutput != nil && headOutput.ContentLength != nil {
				objectSize = *headOutput.ContentLength
			}
			return nil
		})
		if lastErr == nil {
			return client.ObjectAttributes{Size: objectSize}, nil
		}

		if a.IsObjectNotFoundErr(lastErr) {
			return client.ObjectAttributes{}, lastErr
		}

		retries.Wait()
	}

	return client.ObjectAttributes{}, lastErr
}

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (a *S3ObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "S3.DeleteObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		deleteObjectInput := &s3.DeleteObjectInput{
			Bucket: aws.String(a.bucketFromKey(objectKey)),
			Key:    aws.String(a.convertObjectKey(objectKey, true)),
		}

		_, err := a.S3.DeleteObject(ctx, deleteObjectInput)
		return err
	})
}

// bucketFromKey maps a key to a bucket name
func (a *S3ObjectClient) bucketFromKey(key string) string {
	if len(a.bucketNames) == 0 {
		return ""
	}

	hasher := fnv.New32a()
	hasher.Write([]byte(key)) //nolint: errcheck
	hash := hasher.Sum32()

	return a.bucketNames[hash%uint32(len(a.bucketNames))]
}

// GetObject returns a reader and the size for the specified object key from the configured S3 bucket.
func (a *S3ObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var resp *s3.GetObjectOutput

	lastErr := ctx.Err()

	retries := backoff.New(ctx, a.cfg.BackoffConfig)
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return nil, 0, errors.Wrap(ctx.Err(), "ctx related error during s3 getObject")
		}

		lastErr = loki_instrument.TimeRequest(ctx, "S3.GetObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = a.hedgedS3.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(a.bucketFromKey(objectKey)),
				Key:    aws.String(a.convertObjectKey(objectKey, true)),
			})
			return requestErr
		})

		var size int64
		if resp != nil && resp.ContentLength != nil {
			size = *resp.ContentLength
		}
		if lastErr == nil && resp.Body != nil {
			return resp.Body, size, nil
		}
		retries.Wait()
	}

	return nil, 0, errors.Wrap(lastErr, "failed to get s3 object")
}

// GetObject from the store
func (a *S3ObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var resp *s3.GetObjectOutput

	// Map the key into a bucket
	var lastErr error

	retries := backoff.New(ctx, a.cfg.BackoffConfig)
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return nil, errors.Wrap(ctx.Err(), "ctx related error during s3 getObject")
		}

		lastErr = loki_instrument.TimeRequest(ctx, "S3.GetObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = a.hedgedS3.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(a.bucketFromKey(objectKey)),
				Key:    aws.String(a.convertObjectKey(objectKey, true)),
				Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)),
			})
			return requestErr
		})

		if lastErr == nil && resp.Body != nil {
			return resp.Body, nil
		}
		retries.Wait()
	}

	return nil, errors.Wrap(lastErr, "failed to get s3 object")
}

// PutObject into the store
func (a *S3ObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return loki_instrument.TimeRequest(ctx, "S3.PutObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		readSeeker, err := clientutil.ReadSeeker(object)
		if err != nil {
			return err
		}
		putObjectInput := &s3.PutObjectInput{
			Body:         readSeeker,
			Bucket:       aws.String(a.bucketFromKey(objectKey)),
			Key:          aws.String(a.convertObjectKey(objectKey, true)),
			StorageClass: types.StorageClass(a.cfg.StorageClass),
		}

		if a.sseConfig != nil {
			putObjectInput.ServerSideEncryption = types.ServerSideEncryption(a.sseConfig.ServerSideEncryption)
			putObjectInput.SSEKMSKeyId = a.sseConfig.KMSKeyID
			putObjectInput.SSEKMSEncryptionContext = a.sseConfig.KMSEncryptionContext
		}

		_, err = a.S3.PutObject(ctx, putObjectInput)
		return err
	})
}

// List implements chunk.ObjectClient.
func (a *S3ObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	commonPrefixesSet := make(map[string]bool)

	for i := range a.bucketNames {
		err := loki_instrument.TimeRequest(ctx, "S3.List", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := s3.ListObjectsV2Input{
				Bucket:    aws.String(a.bucketNames[i]),
				Prefix:    aws.String(prefix),
				Delimiter: aws.String(delimiter),
			}

			for {
				output, err := a.S3.ListObjectsV2(ctx, &input)
				if err != nil {
					return err
				}

				for _, content := range output.Contents {
					storageObjects = append(storageObjects, client.StorageObject{
						Key:        a.convertObjectKey(*content.Key, false),
						ModifiedAt: *content.LastModified,
					})
				}

				for _, commonPrefix := range output.CommonPrefixes {
					if !commonPrefixesSet[*commonPrefix.Prefix] {
						commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(*commonPrefix.Prefix))
						commonPrefixesSet[*commonPrefix.Prefix] = true
					}
				}

				if output.IsTruncated == nil || !*output.IsTruncated {
					// No more results to fetch
					break
				}
				if output.NextContinuationToken == nil {
					// No way to continue
					break
				}
				input.ContinuationToken = output.NextContinuationToken
			}

			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return storageObjects, commonPrefixes, nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (a *S3ObjectClient) IsObjectNotFoundErr(err error) bool {
	var awsErr *types.NotFound
	if errors.As(err, &awsErr) {
		return true
	}
	var noKeyErr *types.NoSuchKey
	if errors.As(err, &noKeyErr) {
		return true
	}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch (apiError).(type) {
		case *types.NotFound:
			return true
		default:
			return false
		}
	}
	return false
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isContextErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

// IsStorageTimeoutErr returns true if error means that object cannot be retrieved right now due to server-side timeouts.
func IsStorageTimeoutErr(err error) bool {
	// TODO(dannyk): move these out to be generic
	// context errors are all client-side
	if isContextErr(err) {
		// Go 1.23 changed the type of the error returned by the http client when a timeout occurs
		// while waiting for headers.  This is a server side timeout.
		return strings.Contains(err.Error(), "Client.Timeout")
	}

	// connection misconfiguration, or writing on a closed connection
	// do NOT retry; this is not a server-side issue
	if errors.Is(err, net.ErrClosed) || amnet.IsConnectionRefused(err) {
		return false
	}

	// this is a server-side timeout
	if isTimeoutError(err) {
		return true
	}

	// connection closed (closed before established) or reset (closed after established)
	// this is a server-side issue
	if errors.Is(err, io.EOF) || amnet.IsConnectionReset(err) {
		return true
	}
	// TODO types.Error does not actually implement error!
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "RequestTimeout":
			return true
		default:
			return false
		}
	}
	return false
}

// IsStorageThrottledErr returns true if error means that object cannot be retrieved right now due to throttling.
func IsStorageThrottledErr(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		// all 5xx errors are retryable
		switch apiError.ErrorCode() {
		case "RequestTimeout": // 400
			return true
		case "TooManyRequestsException": // 429
			return true
		case "InternalError": // 500
			return true
		case "NotImplemented": // 501
			return true
		case "ServiceUnavailable": // 503
			return true
		case errCodeSlowDown: // 503
			return true
		default:
			return false
		}
	}
	return false
}

func IsRetryableErr(err error) bool {
	return IsStorageTimeoutErr(err) || IsStorageThrottledErr(err)
}

func (a *S3ObjectClient) IsRetryableErr(err error) bool {
	return IsRetryableErr(err)
}

// convertObjectKey modifies the object key based on a delimiter and a mode flag determining conversion.
func (a *S3ObjectClient) convertObjectKey(objectKey string, toS3 bool) string {
	if len(a.cfg.ChunkDelimiter) == 1 {
		if toS3 {
			objectKey = strings.ReplaceAll(objectKey, ":", a.cfg.ChunkDelimiter)
		} else {
			objectKey = strings.ReplaceAll(objectKey, a.cfg.ChunkDelimiter, ":")
		}
	}
	return objectKey
}
