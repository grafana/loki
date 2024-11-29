package ibmcloud

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	ibm "github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	cos "github.com/IBM/ibm-cos-sdk-go/service/s3"
	cosiface "github.com/IBM/ibm-cos-sdk-go/service/s3/s3iface"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/log"
)

const defaultCOSAuthEndpoint = "https://iam.cloud.ibm.com/identity/token"

var (
	errInvalidCOSHMACCredentials = errors.New("must supply both an Access Key ID and Secret Access Key or neither")
	errEmptyRegion               = errors.New("region should not be empty")
	errEmptyEndpoint             = errors.New("endpoint should not be empty")
	errEmptyBucket               = errors.New("at least one bucket name must be specified")
	errCOSConfig                 = "failed to build cos config"
	errServiceInstanceID         = errors.New("must supply ServiceInstanceID")
	errInvalidCredentials        = errors.New("must supply any of Access Key ID and Secret Access Key or API Key or CR token file path")
	errTrustedProfile            = errors.New("must supply any of trusted profile name or trusted profile ID")
)

var cosRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "cos_request_duration_seconds",
	Help:      "Time spent doing cos requests.",
	Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
}, []string{"operation", "status_code"}))

// InjectRequestMiddleware gives users of this client the ability to make arbitrary
// changes to outgoing requests.
type InjectRequestMiddleware func(next http.RoundTripper) http.RoundTripper

func init() {
	cosRequestDuration.Register()
}

// COSConfig specifies config for storing chunks on IBM cos.
type COSConfig struct {
	ForcePathStyle     bool           `yaml:"forcepathstyle"`
	BucketNames        string         `yaml:"bucketnames"`
	Endpoint           string         `yaml:"endpoint"`
	Region             string         `yaml:"region"`
	AccessKeyID        string         `yaml:"access_key_id"`
	SecretAccessKey    flagext.Secret `yaml:"secret_access_key"`
	HTTPConfig         HTTPConfig     `yaml:"http_config"`
	BackoffConfig      backoff.Config `yaml:"backoff_config" doc:"description=Configures back off when cos get Object."`
	APIKey             flagext.Secret `yaml:"api_key"`
	ServiceInstanceID  string         `yaml:"service_instance_id"`
	AuthEndpoint       string         `yaml:"auth_endpoint"`
	CRTokenFilePath    string         `yaml:"cr_token_file_path"`
	TrustedProfileName string         `yaml:"trusted_profile_name"`
	TrustedProfileID   string         `yaml:"trusted_profile_id"`
}

// HTTPConfig stores the http.Transport configuration
type HTTPConfig struct {
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *COSConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet with a specified prefix
func (cfg *COSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.ForcePathStyle, prefix+"cos.force-path-style", false, "Set this to `true` to force the request to use path-style addressing.")
	f.StringVar(&cfg.BucketNames, prefix+"cos.buckets", "", "Comma separated list of bucket names to evenly distribute chunks over.")

	f.StringVar(&cfg.Endpoint, prefix+"cos.endpoint", "", "COS Endpoint to connect to.")
	f.StringVar(&cfg.Region, prefix+"cos.region", "", "COS region to use.")
	f.StringVar(&cfg.AccessKeyID, prefix+"cos.access-key-id", "", "COS HMAC Access Key ID.")
	f.Var(&cfg.SecretAccessKey, prefix+"cos.secret-access-key", "COS HMAC Secret Access Key.")

	f.DurationVar(&cfg.HTTPConfig.IdleConnTimeout, prefix+"cos.http.idle-conn-timeout", 90*time.Second, "The maximum amount of time an idle connection will be held open.")
	f.DurationVar(&cfg.HTTPConfig.ResponseHeaderTimeout, prefix+"cos.http.response-header-timeout", 0, "If non-zero, specifies the amount of time to wait for a server's response headers after fully writing the request.")

	f.DurationVar(&cfg.BackoffConfig.MinBackoff, prefix+"cos.min-backoff", 100*time.Millisecond, "Minimum backoff time when cos get Object.")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, prefix+"cos.max-backoff", 3*time.Second, "Maximum backoff time when cos get Object.")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, prefix+"cos.max-retries", 5, "Maximum number of times to retry when cos get Object.")

	f.Var(&cfg.APIKey, prefix+"cos.api-key", "IAM API key to access COS.")
	f.StringVar(&cfg.AuthEndpoint, prefix+"cos.auth-endpoint", defaultCOSAuthEndpoint, "IAM Auth Endpoint for authentication.")
	f.StringVar(&cfg.ServiceInstanceID, prefix+"cos.service-instance-id", "", "COS service instance id to use.")

	f.StringVar(&cfg.CRTokenFilePath, prefix+"cos.cr-token-file-path", "", "Compute resource token file path.")
	f.StringVar(&cfg.TrustedProfileName, prefix+"cos.trusted-profile-name", "", "Name of the trusted profile.")
	f.StringVar(&cfg.TrustedProfileID, prefix+"cos.trusted-profile-id", "", "ID of the trusted profile.")
}

type COSObjectClient struct {
	cfg COSConfig

	bucketNames []string
	cos         cosiface.S3API
	hedgedCOS   cosiface.S3API
}

// NewCOSObjectClient makes a new COS backed ObjectClient.
func NewCOSObjectClient(cfg COSConfig, hedgingCfg hedging.Config) (*COSObjectClient, error) {
	bucketNames, err := buckets(cfg)
	if err != nil {
		return nil, err
	}
	cosClient, err := buildCOSClient(cfg, hedgingCfg, false)
	if err != nil {
		return nil, errors.Wrap(err, errCOSConfig)
	}
	cosClientHedging, err := buildCOSClient(cfg, hedgingCfg, true)
	if err != nil {
		return nil, errors.Wrap(err, errCOSConfig)
	}
	client := COSObjectClient{
		cfg:         cfg,
		cos:         cosClient,
		hedgedCOS:   cosClientHedging,
		bucketNames: bucketNames,
	}
	return &client, nil
}

func validate(cfg COSConfig) error {
	if (cfg.AccessKeyID == "" && cfg.SecretAccessKey.String() == "") &&
		cfg.APIKey.String() == "" && cfg.CRTokenFilePath == "" {
		return errInvalidCredentials
	}

	if cfg.CRTokenFilePath != "" {
		if _, err := os.Stat(cfg.CRTokenFilePath); errors.Is(err, os.ErrNotExist) {
			return err
		}

		if cfg.TrustedProfileName == "" && cfg.TrustedProfileID == "" {
			return errTrustedProfile
		}
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() == "" ||
		cfg.AccessKeyID == "" && cfg.SecretAccessKey.String() != "" {
		return errInvalidCOSHMACCredentials
	}

	if cfg.Region == "" {
		return errEmptyRegion
	}

	if cfg.Endpoint == "" {
		return errEmptyEndpoint
	}

	if cfg.APIKey.String() != "" && cfg.AuthEndpoint == "" {
		cfg.AuthEndpoint = defaultCOSAuthEndpoint
	}

	if cfg.APIKey.String() != "" && cfg.ServiceInstanceID == "" {
		return errServiceInstanceID
	}
	return nil
}

func getCreds(cfg COSConfig) *credentials.Credentials {
	switch {
	case cfg.CRTokenFilePath != "":
		level.Info(log.Logger).Log(
			"msg", "using the trusted profile auth",
			"cr_token_file_path", cfg.CRTokenFilePath,
			"trusted_profile_name", cfg.TrustedProfileName,
			"trusted_profile_id", cfg.TrustedProfileID,
			"auth_endpoint", cfg.AuthEndpoint,
		)

		return NewTrustedProfileCredentials(cfg.AuthEndpoint, cfg.TrustedProfileName,
			cfg.TrustedProfileID, cfg.CRTokenFilePath)

	case cfg.APIKey.String() != "":
		level.Info(log.Logger).Log(
			"msg", "using the APIkey auth",
			"service_instance_id", cfg.ServiceInstanceID,
			"auth_endpoint", cfg.AuthEndpoint,
		)

		return ibmiam.NewStaticCredentials(ibm.NewConfig(),
			cfg.AuthEndpoint, cfg.APIKey.String(), cfg.ServiceInstanceID)

	case cfg.AccessKeyID != "" && cfg.SecretAccessKey.String() != "":
		level.Info(log.Logger).Log("msg", "using the HMAC auth")

		return credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey.String(), "")

	default:
		return nil
	}
}

func buildCOSClient(cfg COSConfig, hedgingCfg hedging.Config, hedging bool) (*cos.S3, error) {
	var err error
	if err = validate(cfg); err != nil {
		return nil, err
	}
	cosConfig := &ibm.Config{}

	cosConfig = cosConfig.WithMaxRetries(0)                        // We do our own retries, so we can monitor them
	cosConfig = cosConfig.WithS3ForcePathStyle(cfg.ForcePathStyle) // support for Path Style cos url if has the flag

	cosConfig = cosConfig.WithEndpoint(cfg.Endpoint)

	cosConfig = cosConfig.WithRegion(cfg.Region)

	cosConfig = cosConfig.WithCredentials(getCreds(cfg))

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
	})

	httpClient := &http.Client{
		Transport: transport,
	}

	if hedging {
		httpClient, err = hedgingCfg.ClientWithRegisterer(httpClient, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	cosConfig = cosConfig.WithHTTPClient(httpClient)

	sess, err := session.NewSession(cosConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new cos session")
	}

	cosClient := cos.New(sess)

	return cosClient, nil
}

func buckets(cfg COSConfig) ([]string, error) {
	// bucketnames
	var bucketNames []string

	if cfg.BucketNames != "" {
		bucketNames = strings.Split(cfg.BucketNames, ",") // comma separated list of bucket names
	}

	if len(bucketNames) == 0 {
		return nil, errEmptyBucket
	}
	return bucketNames, nil
}

// bucketFromKey maps a key to a bucket name
func (c *COSObjectClient) bucketFromKey(key string) string {
	if len(c.bucketNames) == 0 {
		return ""
	}

	hasher := fnv.New32a()
	hasher.Write([]byte(key)) //nolint: errcheck
	hash := hasher.Sum32()

	return c.bucketNames[hash%uint32(len(c.bucketNames))]
}

// Stop fulfills the chunk.ObjectClient interface
func (c *COSObjectClient) Stop() {}

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (c *COSObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "COS.DeleteObject", cosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		deleteObjectInput := &cos.DeleteObjectInput{
			Bucket: ibm.String(c.bucketFromKey(objectKey)),
			Key:    ibm.String(objectKey),
		}

		_, err := c.cos.DeleteObjectWithContext(ctx, deleteObjectInput)
		return err
	})
}

func (c *COSObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := c.objectAttributes(ctx, objectKey, "COS.ObjectExists"); err != nil {
		if c.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *COSObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return c.objectAttributes(ctx, objectKey, "COS.GetAttributes")
}

func (c *COSObjectClient) objectAttributes(ctx context.Context, objectKey, source string) (client.ObjectAttributes, error) {
	bucket := c.bucketFromKey(objectKey)
	var objectSize int64
	err := instrument.CollectedRequest(ctx, source, cosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		headOutput, requestErr := c.hedgedCOS.HeadObject(&cos.HeadObjectInput{
			Bucket: ibm.String(bucket),
			Key:    ibm.String(objectKey),
		})
		if requestErr != nil {
			return requestErr
		}
		if headOutput != nil && headOutput.ContentLength != nil {
			objectSize = *headOutput.ContentLength
		}
		return nil
	})
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: objectSize}, nil
}

// GetObject returns a reader and the size for the specified object key from the configured S3 bucket.
func (c *COSObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var resp *cos.GetObjectOutput

	// Map the key into a bucket
	bucket := c.bucketFromKey(objectKey)

	retries := backoff.New(ctx, c.cfg.BackoffConfig)
	err := ctx.Err()
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return nil, 0, errors.Wrap(ctx.Err(), "ctx related error during cos getObject")
		}
		err = instrument.CollectedRequest(ctx, "COS.GetObject", cosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = c.hedgedCOS.GetObjectWithContext(ctx, &cos.GetObjectInput{
				Bucket: ibm.String(bucket),
				Key:    ibm.String(objectKey),
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
	return nil, 0, errors.Wrap(err, "failed to get cos object")
}

// GetObject returns a reader and the size for the specified object key from the configured S3 bucket.
func (c *COSObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var resp *cos.GetObjectOutput

	// Map the key into a bucket
	bucket := c.bucketFromKey(objectKey)

	retries := backoff.New(ctx, c.cfg.BackoffConfig)
	err := ctx.Err()
	for retries.Ongoing() {
		if ctx.Err() != nil {
			return nil, errors.Wrap(ctx.Err(), "ctx related error during cos getObject")
		}
		err = instrument.CollectedRequest(ctx, "COS.GetObject", cosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = c.hedgedCOS.GetObjectWithContext(ctx, &cos.GetObjectInput{
				Bucket: ibm.String(bucket),
				Key:    ibm.String(objectKey),
				Range:  ibm.String(fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)),
			})
			return requestErr
		})
		if err == nil && resp.Body != nil {
			return resp.Body, nil
		}
		retries.Wait()
	}
	return nil, errors.Wrap(err, "failed to get cos object")
}

// PutObject into the store
func (c *COSObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return instrument.CollectedRequest(ctx, "COS.PutObject", cosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		readSeeker, err := util.ReadSeeker(object)
		if err != nil {
			return err
		}
		putObjectInput := &cos.PutObjectInput{
			Body:   readSeeker,
			Bucket: ibm.String(c.bucketFromKey(objectKey)),
			Key:    ibm.String(objectKey),
		}

		_, err = c.cos.PutObjectWithContext(ctx, putObjectInput)
		return err
	})
}

// List implements chunk.ObjectClient.
func (c *COSObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	for i := range c.bucketNames {
		err := instrument.CollectedRequest(ctx, "COS.List", cosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := cos.ListObjectsV2Input{
				Bucket:    ibm.String(c.bucketNames[i]),
				Prefix:    ibm.String(prefix),
				Delimiter: ibm.String(delimiter),
			}

			for {
				output, err := c.cos.ListObjectsV2WithContext(ctx, &input)
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
					commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(ibm.StringValue(commonPrefix.Prefix)))
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
func (c *COSObjectClient) IsObjectNotFoundErr(err error) bool {
	if aerr, ok := errors.Cause(err).(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
		return true
	}

	return false
}

// TODO(dannyk): implement for client
func (c *COSObjectClient) IsRetryableErr(error) bool { return false }
