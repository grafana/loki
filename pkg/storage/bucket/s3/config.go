package s3

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	s3_service "github.com/aws/aws-sdk-go/service/s3"
	"github.com/grafana/dskit/flagext"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/pkg/errors"
	"github.com/thanos-io/objstore/providers/s3"

	"github.com/grafana/loki/v3/pkg/util"
)

const (
	// SSEKMS config type constant to configure S3 server side encryption using KMS
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingKMSEncryption.html
	SSEKMS = "SSE-KMS"

	// SSES3 config type constant to configure S3 server side encryption with AES-256
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingServerSideEncryption.html
	SSES3 = "SSE-S3"
)

var (
	supportedSSETypes          = []string{SSEKMS, SSES3}
	supportedStorageClasses    = s3_service.ObjectStorageClass_Values()
	supportedBucketLookupTypes = thanosS3BucketLookupTypesValues()

	errUnsupportedSSEType      = errors.New("unsupported S3 SSE type")
	errUnsupportedStorageClass = fmt.Errorf("unsupported S3 storage class (supported values: %s)", strings.Join(supportedStorageClasses, ", "))
	errInvalidSSEContext       = errors.New("invalid S3 SSE encryption context")
	errInvalidEndpointPrefix   = errors.New("the endpoint must not prefixed with the bucket name")
	errInvalidSTSEndpoint      = errors.New("sts-endpoint must be a valid url")
)

var thanosS3BucketLookupTypes = map[string]s3.BucketLookupType{
	s3.AutoLookup.String():        s3.AutoLookup,
	s3.VirtualHostLookup.String(): s3.VirtualHostLookup,
	s3.PathLookup.String():        s3.PathLookup,
}

func thanosS3BucketLookupTypesValues() (list []string) {
	for k := range thanosS3BucketLookupTypes {
		list = append(list, k)
	}
	// sort the list for consistent output in help, where it's used
	slices.Sort(list)
	return list
}

// HTTPConfig stores the http.Transport configuration for the s3 minio client.
type HTTPConfig struct {
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout" category:"advanced"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout" category:"advanced"`
	InsecureSkipVerify    bool          `yaml:"insecure_skip_verify" category:"advanced"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout" category:"advanced"`
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout" category:"advanced"`
	MaxIdleConns          int           `yaml:"max_idle_connections" category:"advanced"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_connections_per_host" category:"advanced"`
	MaxConnsPerHost       int           `yaml:"max_connections_per_host" category:"advanced"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`

	TLSConfig TLSConfig `yaml:",inline"`
}

// TLSConfig configures the options for TLS connections.
type TLSConfig struct {
	CAPath     string `yaml:"tls_ca_path" category:"advanced"`
	CertPath   string `yaml:"tls_cert_path" category:"advanced"`
	KeyPath    string `yaml:"tls_key_path" category:"advanced"`
	ServerName string `yaml:"tls_server_name" category:"advanced"`
}

// RegisterFlagsWithPrefix registers the flags for s3 storage with the provided prefix
func (cfg *HTTPConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.IdleConnTimeout, prefix+"s3.http.idle-conn-timeout", 90*time.Second, "The time an idle connection will remain idle before closing.")
	f.DurationVar(&cfg.ResponseHeaderTimeout, prefix+"s3.http.response-header-timeout", 2*time.Minute, "The amount of time the client will wait for a servers response headers.")
	f.BoolVar(&cfg.InsecureSkipVerify, prefix+"s3.http.insecure-skip-verify", false, "If the client connects to S3 via HTTPS and this option is enabled, the client will accept any certificate and hostname.")
	f.DurationVar(&cfg.TLSHandshakeTimeout, prefix+"s3.tls-handshake-timeout", 10*time.Second, "Maximum time to wait for a TLS handshake. 0 means no limit.")
	f.DurationVar(&cfg.ExpectContinueTimeout, prefix+"s3.expect-continue-timeout", 1*time.Second, "The time to wait for a server's first response headers after fully writing the request headers if the request has an Expect header. 0 to send the request body immediately.")
	f.IntVar(&cfg.MaxIdleConns, prefix+"s3.max-idle-connections", 100, "Maximum number of idle (keep-alive) connections across all hosts. 0 means no limit.")
	f.IntVar(&cfg.MaxIdleConnsPerHost, prefix+"s3.max-idle-connections-per-host", 100, "Maximum number of idle (keep-alive) connections to keep per-host. If 0, a built-in default value is used.")
	f.IntVar(&cfg.MaxConnsPerHost, prefix+"s3.max-connections-per-host", 0, "Maximum number of connections per host. 0 means no limit.")
	cfg.TLSConfig.RegisterFlagsWithPrefix(prefix, f)
}

// RegisterFlagsWithPrefix registers the flags for s3 storage with the provided prefix.
func (cfg *TLSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.CAPath, prefix+"s3.http.tls-ca-path", "", "Path to the CA certificates to validate server certificate against. If not set, the host's root CA certificates are used.")
	f.StringVar(&cfg.CertPath, prefix+"s3.http.tls-cert-path", "", "Path to the client certificate, which will be used for authenticating with the server. Also requires the key path to be configured.")
	f.StringVar(&cfg.KeyPath, prefix+"s3.http.tls-key-path", "", "Path to the key for the client certificate. Also requires the client certificate to be configured.")
	f.StringVar(&cfg.ServerName, prefix+"s3.http.tls-server-name", "", "Override the expected name on the server certificate.")
}

// Config holds the config options for an S3 backend
type Config struct {
	Endpoint             string              `yaml:"endpoint"`
	Region               string              `yaml:"region"`
	BucketName           string              `yaml:"bucket_name"`
	SecretAccessKey      flagext.Secret      `yaml:"secret_access_key"`
	AccessKeyID          string              `yaml:"access_key_id"`
	SessionToken         flagext.Secret      `yaml:"session_token"`
	Insecure             bool                `yaml:"insecure" category:"advanced"`
	ListObjectsVersion   string              `yaml:"list_objects_version" category:"advanced"`
	BucketLookupType     s3.BucketLookupType `yaml:"bucket_lookup_type" category:"advanced"`
	DualstackEnabled     bool                `yaml:"dualstack_enabled" category:"experimental"`
	StorageClass         string              `yaml:"storage_class" category:"experimental"`
	NativeAWSAuthEnabled bool                `yaml:"native_aws_auth_enabled" category:"experimental"`
	PartSize             uint64              `yaml:"part_size" category:"experimental"`
	SendContentMd5       bool                `yaml:"send_content_md5" category:"experimental"`
	STSEndpoint          string              `yaml:"sts_endpoint"`

	SSE         SSEConfig   `yaml:"sse"`
	HTTP        HTTPConfig  `yaml:"http"`
	TraceConfig TraceConfig `yaml:"trace"`
}

// RegisterFlags registers the flags for s3 storage with the provided prefix
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for s3 storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.AccessKeyID, prefix+"s3.access-key-id", "", "S3 access key ID")
	f.Var(&cfg.SecretAccessKey, prefix+"s3.secret-access-key", "S3 secret access key")
	f.Var(&cfg.SessionToken, prefix+"s3.session-token", "S3 session token")
	f.StringVar(&cfg.BucketName, prefix+"s3.bucket-name", "", "S3 bucket name")
	f.StringVar(&cfg.Region, prefix+"s3.region", "", "S3 region. If unset, the client will issue a S3 GetBucketLocation API call to autodetect it.")
	f.StringVar(&cfg.Endpoint, prefix+"s3.endpoint", "", "The S3 bucket endpoint. It could be an AWS S3 endpoint listed at https://docs.aws.amazon.com/general/latest/gr/s3.html or the address of an S3-compatible service in hostname:port format.")
	f.BoolVar(&cfg.Insecure, prefix+"s3.insecure", false, "If enabled, use http:// for the S3 endpoint instead of https://. This could be useful in local dev/test environments while using an S3-compatible backend storage, like Minio.")
	f.StringVar(&cfg.ListObjectsVersion, prefix+"s3.list-objects-version", "", "Use a specific version of the S3 list object API. Supported values are v1 or v2. Default is unset.")
	f.StringVar(&cfg.StorageClass, prefix+"s3.storage-class", "", "The S3 storage class to use, not set by default. Details can be found at https://aws.amazon.com/s3/storage-classes/. Supported values are: "+strings.Join(supportedStorageClasses, ", "))
	f.BoolVar(&cfg.NativeAWSAuthEnabled, prefix+"s3.native-aws-auth-enabled", false, "If enabled, it will use the default authentication methods of the AWS SDK for go based on known environment variables and known AWS config files.")
	f.Uint64Var(&cfg.PartSize, prefix+"s3.part-size", 0, "The minimum file size in bytes used for multipart uploads. If 0, the value is optimally computed for each object.")
	f.BoolVar(&cfg.SendContentMd5, prefix+"s3.send-content-md5", false, "If enabled, a Content-MD5 header is sent with S3 Put Object requests. Consumes more resources to compute the MD5, but may improve compatibility with object storage services that do not support checksums.")
	f.Var(newBucketLookupTypeValue(s3.AutoLookup, &cfg.BucketLookupType), prefix+"s3.bucket-lookup-type", fmt.Sprintf("Bucket lookup style type, used to access bucket in S3-compatible service. Default is auto. Supported values are: %s.", strings.Join(supportedBucketLookupTypes, ", ")))
	f.BoolVar(&cfg.DualstackEnabled, prefix+"s3.dualstack-enabled", true, "When enabled, direct all AWS S3 requests to the dual-stack IPv4/IPv6 endpoint for the configured region.")
	f.StringVar(&cfg.STSEndpoint, prefix+"s3.sts-endpoint", "", "Accessing S3 resources using temporary, secure credentials provided by AWS Security Token Service.")
	cfg.SSE.RegisterFlagsWithPrefix(prefix+"s3.sse.", f)
	cfg.HTTP.RegisterFlagsWithPrefix(prefix, f)
	cfg.TraceConfig.RegisterFlagsWithPrefix(prefix+"s3.trace.", f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if cfg.Endpoint != "" {
		endpoint := strings.Split(cfg.Endpoint, ".")
		if cfg.BucketName != "" && endpoint[0] != "" && endpoint[0] == cfg.BucketName {
			return errInvalidEndpointPrefix
		}
	}
	if cfg.STSEndpoint != "" && !util.IsValidURL(cfg.STSEndpoint) {
		return errInvalidSTSEndpoint
	}
	if !slices.Contains(supportedStorageClasses, cfg.StorageClass) && cfg.StorageClass != "" {
		return errUnsupportedStorageClass
	}

	return cfg.SSE.Validate()
}

// SSEConfig configures S3 server side encryption
// struct that is going to receive user input (through config file or CLI)
type SSEConfig struct {
	Type                 string `yaml:"type"`
	KMSKeyID             string `yaml:"kms_key_id"`
	KMSEncryptionContext string `yaml:"kms_encryption_context"`
}

func (cfg *SSEConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *SSEConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Type, prefix+"type", "", fmt.Sprintf("Enable AWS Server Side Encryption. Supported values: %s.", strings.Join(supportedSSETypes, ", ")))
	f.StringVar(&cfg.KMSKeyID, prefix+"kms-key-id", "", "KMS Key ID used to encrypt objects in S3")
	f.StringVar(&cfg.KMSEncryptionContext, prefix+"kms-encryption-context", "", "KMS Encryption Context used for object encryption. It expects JSON formatted string.")
}

func (cfg *SSEConfig) Validate() error {
	if cfg.Type != "" && !util.StringsContain(supportedSSETypes, cfg.Type) {
		return errUnsupportedSSEType
	}

	if _, err := parseKMSEncryptionContext(cfg.KMSEncryptionContext); err != nil {
		return errInvalidSSEContext
	}

	return nil
}

// BuildThanosConfig builds the SSE config expected by the Thanos client.
func (cfg *SSEConfig) BuildThanosConfig() (s3.SSEConfig, error) {
	switch cfg.Type {
	case "":
		return s3.SSEConfig{}, nil
	case SSEKMS:
		encryptionCtx, err := parseKMSEncryptionContext(cfg.KMSEncryptionContext)
		if err != nil {
			return s3.SSEConfig{}, err
		}

		return s3.SSEConfig{
			Type:                 s3.SSEKMS,
			KMSKeyID:             cfg.KMSKeyID,
			KMSEncryptionContext: encryptionCtx,
		}, nil
	case SSES3:
		return s3.SSEConfig{
			Type: s3.SSES3,
		}, nil
	default:
		return s3.SSEConfig{}, errUnsupportedSSEType
	}
}

// BuildMinioConfig builds the SSE config expected by the Minio client.
func (cfg *SSEConfig) BuildMinioConfig() (encrypt.ServerSide, error) {
	switch cfg.Type {
	case "":
		return nil, nil
	case SSEKMS:
		encryptionCtx, err := parseKMSEncryptionContext(cfg.KMSEncryptionContext)
		if err != nil {
			return nil, err
		}

		if encryptionCtx == nil {
			// To overcome a limitation in Minio which checks interface{} == nil.
			return encrypt.NewSSEKMS(cfg.KMSKeyID, nil)
		}
		return encrypt.NewSSEKMS(cfg.KMSKeyID, encryptionCtx)
	case SSES3:
		return encrypt.NewSSE(), nil
	default:
		return nil, errUnsupportedSSEType
	}
}

func parseKMSEncryptionContext(data string) (map[string]string, error) {
	if data == "" {
		return nil, nil
	}

	decoded := map[string]string{}
	err := errors.Wrap(json.Unmarshal([]byte(data), &decoded), "unable to parse KMS encryption context")
	return decoded, err
}

type TraceConfig struct {
	Enabled bool `yaml:"enabled" category:"advanced"`
}

func (cfg *TraceConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "When enabled, low-level S3 HTTP operation information is logged at the debug level.")
}

// bucketLookupTypeValue is an adapter between s3.BucketLookupType and flag.Value.
type bucketLookupTypeValue s3.BucketLookupType

func newBucketLookupTypeValue(value s3.BucketLookupType, p *s3.BucketLookupType) *bucketLookupTypeValue {
	*p = value
	return (*bucketLookupTypeValue)(p)
}

func (v *bucketLookupTypeValue) String() string {
	if v == nil {
		return s3.AutoLookup.String()
	}
	return s3.BucketLookupType(*v).String()
}

func (v *bucketLookupTypeValue) Set(s string) error {
	t, ok := thanosS3BucketLookupTypes[s]
	if !ok {
		return fmt.Errorf("unsupported bucket lookup type: %s", s)
	}
	*v = bucketLookupTypeValue(t)
	return nil
}
