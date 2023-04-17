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

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	v4 "github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/minio/minio-go/v7/pkg/signer"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"

	bucket_s3 "github.com/grafana/loki/pkg/storage/bucket/s3"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	storageawscommon "github.com/grafana/loki/pkg/storage/common/aws"
	"github.com/grafana/loki/pkg/util"
)

const (
	SignatureVersionV4 = "v4"
	SignatureVersionV2 = "v2"
)

var (
	supportedSignatureVersions     = []string{SignatureVersionV4, SignatureVersionV2}
	errUnsupportedSignatureVersion = errors.New("unsupported signature version")
)

var s3RequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "loki",
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
	SSEEncryption    bool                `yaml:"sse_encryption"`
	HTTPConfig       HTTPConfig          `yaml:"http_config"`
	SignatureVersion string              `yaml:"signature_version"`
	StorageClass     string              `yaml:"storage_class"`
	SSEConfig        bucket_s3.SSEConfig `yaml:"sse"`
	BackoffConfig    backoff.Config      `yaml:"backoff_config" doc:"description=Configures back off when S3 get Object."`

	Inject InjectRequestMiddleware `yaml:"-"`
}

// HTTPConfig stores the http.Transport configuration
type HTTPConfig struct {
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

	// TODO Remove in Cortex 1.10.0
	f.BoolVar(&cfg.SSEEncryption, prefix+"s3.sse-encryption", false, "Enable AWS Server Side Encryption [Deprecated: Use .sse instead. if s3.sse-encryption is enabled, it assumes .sse.type SSE-S3]")

	cfg.SSEConfig.RegisterFlagsWithPrefix(prefix+"s3.sse.", f)

	f.DurationVar(&cfg.HTTPConfig.IdleConnTimeout, prefix+"s3.http.idle-conn-timeout", 90*time.Second, "The maximum amount of time an idle connection will be held open.")
	f.DurationVar(&cfg.HTTPConfig.ResponseHeaderTimeout, prefix+"s3.http.response-header-timeout", 0, "If non-zero, specifies the amount of time to wait for a server's response headers after fully writing the request.")
	f.BoolVar(&cfg.HTTPConfig.InsecureSkipVerify, prefix+"s3.http.insecure-skip-verify", false, "Set to true to skip verifying the certificate chain and hostname.")
	f.StringVar(&cfg.HTTPConfig.CAFile, prefix+"s3.http.ca-file", "", "Path to the trusted CA file that signed the SSL certificate of the S3 endpoint.")
	f.StringVar(&cfg.SignatureVersion, prefix+"s3.signature-version", SignatureVersionV4, fmt.Sprintf("The signature version to use for authenticating against S3. Supported values are: %s.", strings.Join(supportedSignatureVersions, ", ")))
	f.StringVar(&cfg.StorageClass, prefix+"s3.storage-class", storageawscommon.StorageClassStandard, fmt.Sprintf("The S3 storage class which objects will use. Supported values are: %s.", strings.Join(storageawscommon.SupportedStorageClasses, ", ")))

	f.DurationVar(&cfg.BackoffConfig.MinBackoff, prefix+"s3.min-backoff", 100*time.Millisecond, "Minimum backoff time when s3 get Object")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, prefix+"s3.max-backoff", 3*time.Second, "Maximum backoff time when s3 get Object")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, prefix+"s3.max-retries", 5, "Maximum number of times to retry when s3 get Object")
}

// Validate config and returns error on failure
func (cfg *S3Config) Validate() error {
	if !util.StringsContain(supportedSignatureVersions, cfg.SignatureVersion) {
		return errUnsupportedSignatureVersion
	}

	return storageawscommon.ValidateStorageClass(cfg.StorageClass)
}

type S3ObjectClient struct {
	cfg S3Config

	bucketNames []string
	S3          s3iface.S3API
	hedgedS3    s3iface.S3API
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

	// deprecated, but if used it assumes SSE-S3 type
	if cfg.SSEEncryption {
		return NewSSEParsedConfig(bucket_s3.SSEConfig{
			Type: bucket_s3.SSES3,
		})
	}

	return nil, nil
}

func v2SignRequestHandler(cfg S3Config) request.NamedHandler {
	return request.NamedHandler{
		Name: "v2.SignRequestHandler",
		Fn: func(req *request.Request) {
			credentials, err := req.Config.Credentials.GetWithContext(req.Context())
			if err != nil {
				if err != nil {
					req.Error = err
					return
				}
			}

			req.HTTPRequest = signer.SignV2(
				*req.HTTPRequest,
				credentials.AccessKeyID,
				credentials.SecretAccessKey,
				!cfg.S3ForcePathStyle,
			)
		},
	}
}

func buildS3Client(cfg S3Config, hedgingCfg hedging.Config, hedging bool) (*s3.S3, error) {
	var s3Config *aws.Config
	var err error

	// if an s3 url is passed use it to initialize the s3Config and then override with any additional params
	if cfg.S3.URL != nil {
		s3Config, err = awscommon.ConfigFromURL(cfg.S3.URL)
		if err != nil {
			return nil, err
		}
	} else {
		s3Config = &aws.Config{}
		s3Config = s3Config.WithRegion("dummy")
	}

	s3Config = s3Config.WithMaxRetries(0)                          // We do our own retries, so we can monitor them
	s3Config = s3Config.WithS3ForcePathStyle(cfg.S3ForcePathStyle) // support for Path Style S3 url if has the flag

	if cfg.Endpoint != "" {
		s3Config = s3Config.WithEndpoint(cfg.Endpoint)
	}

	if cfg.Insecure {
		s3Config = s3Config.WithDisableSSL(true)
	}

	if cfg.Region != "" {
		s3Config = s3Config.WithRegion(cfg.Region)
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() == "" ||
		cfg.AccessKeyID == "" && cfg.SecretAccessKey.String() != "" {
		return nil, errors.New("must supply both an Access Key ID and Secret Access Key or neither")
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() != "" {
		creds := credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey.String(), cfg.SessionToken.String())
		s3Config = s3Config.WithCredentials(creds)
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.HTTPConfig.InsecureSkipVerify,
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
			DualStack: true,
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
	}

	if hedging {
		httpClient, err = hedgingCfg.ClientWithRegisterer(httpClient, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	s3Config = s3Config.WithHTTPClient(httpClient)

	sess, err := session.NewSession(s3Config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new s3 session")
	}

	s3Client := s3.New(sess)

	if cfg.SignatureVersion == SignatureVersionV2 {
		s3Client.Handlers.Sign.Swap(v4.SignRequestHandler.Name, v2SignRequestHandler(cfg))
	}

	return s3Client, nil
}

func buckets(cfg S3Config) ([]string, error) {
	// bucketnames
	var bucketNames []string
	if cfg.S3.URL != nil {
		bucketNames = []string{strings.TrimPrefix(cfg.S3.URL.Path, "/")}
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

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (a *S3ObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "S3.DeleteObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		deleteObjectInput := &s3.DeleteObjectInput{
			Bucket: aws.String(a.bucketFromKey(objectKey)),
			Key:    aws.String(objectKey),
		}

		_, err := a.S3.DeleteObjectWithContext(ctx, deleteObjectInput)
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

	// Map the key into a bucket
	bucket := a.bucketFromKey(objectKey)

	retries := backoff.New(ctx, a.cfg.BackoffConfig)
	err := ctx.Err()
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return nil, 0, errors.Wrap(ctx.Err(), "ctx related error during s3 getObject")
		}
		err = instrument.CollectedRequest(ctx, "S3.GetObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = a.hedgedS3.GetObjectWithContext(ctx, &s3.GetObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(objectKey),
			})
			return requestErr
		})
		var size int64
		if resp.ContentLength != nil {
			size = *resp.ContentLength
		}
		if err == nil && resp.Body != nil {
			return resp.Body, size, nil
		}
		retries.Wait()
	}
	return nil, 0, errors.Wrap(err, "failed to get s3 object")
}

// PutObject into the store
func (a *S3ObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "S3.PutObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		putObjectInput := &s3.PutObjectInput{
			Body:         object,
			Bucket:       aws.String(a.bucketFromKey(objectKey)),
			Key:          aws.String(objectKey),
			StorageClass: aws.String(a.cfg.StorageClass),
		}

		if a.sseConfig != nil {
			putObjectInput.ServerSideEncryption = aws.String(a.sseConfig.ServerSideEncryption)
			putObjectInput.SSEKMSKeyId = a.sseConfig.KMSKeyID
			putObjectInput.SSEKMSEncryptionContext = a.sseConfig.KMSEncryptionContext
		}

		_, err := a.S3.PutObjectWithContext(ctx, putObjectInput)
		return err
	})
}

// List implements chunk.ObjectClient.
func (a *S3ObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	for i := range a.bucketNames {
		err := instrument.CollectedRequest(ctx, "S3.List", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := s3.ListObjectsV2Input{
				Bucket:    aws.String(a.bucketNames[i]),
				Prefix:    aws.String(prefix),
				Delimiter: aws.String(delimiter),
			}

			for {
				output, err := a.S3.ListObjectsV2WithContext(ctx, &input)
				if err != nil {
					return err
				}

				for _, content := range output.Contents {
					storageObjects = append(storageObjects, client.StorageObject{
						Key:        *content.Key,
						ModifiedAt: *content.LastModified,
					})
				}

				for _, commonPrefix := range output.CommonPrefixes {
					commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(aws.StringValue(commonPrefix.Prefix)))
				}

				if output.IsTruncated == nil || !*output.IsTruncated {
					// No more results to fetch
					break
				}
				if output.NextContinuationToken == nil {
					// No way to continue
					break
				}
				input.SetContinuationToken(*output.NextContinuationToken)
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
	if aerr, ok := errors.Cause(err).(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
		return true
	}

	return false
}
