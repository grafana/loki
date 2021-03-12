package s3

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore/s3"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	SignatureVersionV4 = "v4"
	SignatureVersionV2 = "v2"

	// SSEKMS config type constant to configure S3 server side encryption using KMS
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingKMSEncryption.html
	SSEKMS = "SSE-KMS"

	// SSES3 config type constant to configure S3 server side encryption with AES-256
	// https://docs.aws.amazon.com/AmazonS3/latest/dev/UsingServerSideEncryption.html
	SSES3 = "SSE-S3"
)

var (
	supportedSignatureVersions     = []string{SignatureVersionV4, SignatureVersionV2}
	supportedSSETypes              = []string{SSEKMS, SSES3}
	errUnsupportedSignatureVersion = errors.New("unsupported signature version")
	errUnsupportedSSEType          = errors.New("unsupported S3 SSE type")
	errInvalidSSEContext           = errors.New("invalid S3 SSE encryption context")
)

// HTTPConfig stores the http.Transport configuration for the s3 minio client.
type HTTPConfig struct {
	IdleConnTimeout       time.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout time.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool          `yaml:"insecure_skip_verify"`
	TLSHandshakeTimeout   time.Duration `yaml:"tls_handshake_timeout"`
	ExpectContinueTimeout time.Duration `yaml:"expect_continue_timeout"`
	MaxIdleConns          int           `yaml:"max_idle_connections"`
	MaxIdleConnsPerHost   int           `yaml:"max_idle_connections_per_host"`
	MaxConnsPerHost       int           `yaml:"max_connections_per_host"`

	// Allow upstream callers to inject a round tripper
	Transport http.RoundTripper `yaml:"-"`
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
}

// Config holds the config options for an S3 backend
type Config struct {
	Endpoint         string         `yaml:"endpoint"`
	Region           string         `yaml:"region"`
	BucketName       string         `yaml:"bucket_name"`
	SecretAccessKey  flagext.Secret `yaml:"secret_access_key"`
	AccessKeyID      string         `yaml:"access_key_id"`
	Insecure         bool           `yaml:"insecure"`
	SignatureVersion string         `yaml:"signature_version"`

	SSE  SSEConfig  `yaml:"sse"`
	HTTP HTTPConfig `yaml:"http"`
}

// RegisterFlags registers the flags for s3 storage with the provided prefix
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for s3 storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.AccessKeyID, prefix+"s3.access-key-id", "", "S3 access key ID")
	f.Var(&cfg.SecretAccessKey, prefix+"s3.secret-access-key", "S3 secret access key")
	f.StringVar(&cfg.BucketName, prefix+"s3.bucket-name", "", "S3 bucket name")
	f.StringVar(&cfg.Region, prefix+"s3.region", "", "S3 region. If unset, the client will issue a S3 GetBucketLocation API call to autodetect it.")
	f.StringVar(&cfg.Endpoint, prefix+"s3.endpoint", "", "The S3 bucket endpoint. It could be an AWS S3 endpoint listed at https://docs.aws.amazon.com/general/latest/gr/s3.html or the address of an S3-compatible service in hostname:port format.")
	f.BoolVar(&cfg.Insecure, prefix+"s3.insecure", false, "If enabled, use http:// for the S3 endpoint instead of https://. This could be useful in local dev/test environments while using an S3-compatible backend storage, like Minio.")
	f.StringVar(&cfg.SignatureVersion, prefix+"s3.signature-version", SignatureVersionV4, fmt.Sprintf("The signature version to use for authenticating against S3. Supported values are: %s.", strings.Join(supportedSignatureVersions, ", ")))
	cfg.SSE.RegisterFlagsWithPrefix(prefix+"s3.sse.", f)
	cfg.HTTP.RegisterFlagsWithPrefix(prefix, f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if !util.StringsContain(supportedSignatureVersions, cfg.SignatureVersion) {
		return errUnsupportedSignatureVersion
	}

	if err := cfg.SSE.Validate(); err != nil {
		return err
	}

	return nil
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
