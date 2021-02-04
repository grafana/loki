package s3

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	SignatureVersionV4 = "v4"
	SignatureVersionV2 = "v2"
)

var (
	supportedSignatureVersions     = []string{SignatureVersionV4, SignatureVersionV2}
	errUnsupportedSignatureVersion = errors.New("unsupported signature version")
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
	BucketName       string         `yaml:"bucket_name"`
	SecretAccessKey  flagext.Secret `yaml:"secret_access_key"`
	AccessKeyID      string         `yaml:"access_key_id"`
	Insecure         bool           `yaml:"insecure"`
	SignatureVersion string         `yaml:"signature_version"`

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
	f.StringVar(&cfg.Endpoint, prefix+"s3.endpoint", "", "The S3 bucket endpoint. It could be an AWS S3 endpoint listed at https://docs.aws.amazon.com/general/latest/gr/s3.html or the address of an S3-compatible service in hostname:port format.")
	f.BoolVar(&cfg.Insecure, prefix+"s3.insecure", false, "If enabled, use http:// for the S3 endpoint instead of https://. This could be useful in local dev/test environments while using an S3-compatible backend storage, like Minio.")
	f.StringVar(&cfg.SignatureVersion, prefix+"s3.signature-version", SignatureVersionV4, fmt.Sprintf("The signature version to use for authenticating against S3. Supported values are: %s.", strings.Join(supportedSignatureVersions, ", ")))
	cfg.HTTP.RegisterFlagsWithPrefix(prefix, f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if !util.StringsContain(supportedSignatureVersions, cfg.SignatureVersion) {
		return errUnsupportedSignatureVersion
	}
	return nil
}
