package s3

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/s3"
)

const (
	// Applied to PUT operations to denote the desired storage class for S3 Objects
	awsStorageClassHeader = "X-Amz-Storage-Class"
)

// NewBucketClient creates a new S3 bucket client
func NewBucketClient(cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	s3Cfg, err := newS3Config(cfg)
	if err != nil {
		return nil, err
	}

	return s3.NewBucketWithConfig(logger, s3Cfg, name, wrapRT)
}

// NewBucketReaderClient creates a new S3 bucket client
func NewBucketReaderClient(cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.BucketReader, error) {
	s3Cfg, err := newS3Config(cfg)
	if err != nil {
		return nil, err
	}

	return s3.NewBucketWithConfig(logger, s3Cfg, name, wrapRT)
}

func newS3Config(cfg Config) (s3.Config, error) {
	sseCfg, err := cfg.SSE.BuildThanosConfig()
	if err != nil {
		return s3.Config{}, err
	}

	putUserMetadata := map[string]string{}

	if cfg.StorageClass != "" {
		putUserMetadata[awsStorageClassHeader] = cfg.StorageClass
	}

	return s3.Config{
		Bucket:             cfg.BucketName,
		Endpoint:           cfg.Endpoint,
		Region:             cfg.Region,
		AccessKey:          cfg.AccessKeyID,
		SecretKey:          cfg.SecretAccessKey.String(),
		SessionToken:       cfg.SessionToken.String(),
		Insecure:           cfg.Insecure,
		PutUserMetadata:    putUserMetadata,
		SendContentMd5:     cfg.SendContentMd5,
		SSEConfig:          sseCfg,
		DisableDualstack:   !cfg.DualstackEnabled,
		ListObjectsVersion: cfg.ListObjectsVersion,
		BucketLookupType:   cfg.BucketLookupType,
		AWSSDKAuth:         cfg.NativeAWSAuthEnabled,
		PartSize:           cfg.PartSize,
		HTTPConfig:         newThanosHTTPConfig(cfg),
		TraceConfig: s3.TraceConfig{
			Enable: cfg.TraceConfig.Enabled,
		},
		STSEndpoint: cfg.STSEndpoint,
		MaxRetries:  cfg.MaxRetries,
	}, nil
}

// newThanosHTTPConfig translates Loki's S3 HTTP config to Thanos's exthttp.HTTPConfig.
func newThanosHTTPConfig(cfg Config) s3.HTTPConfig {
	return s3.HTTPConfig{
		IdleConnTimeout:       model.Duration(cfg.HTTP.IdleConnTimeout),
		ResponseHeaderTimeout: model.Duration(cfg.HTTP.ResponseHeaderTimeout),
		InsecureSkipVerify:    cfg.HTTP.InsecureSkipVerify,
		TLSHandshakeTimeout:   model.Duration(cfg.HTTP.TLSHandshakeTimeout),
		ExpectContinueTimeout: model.Duration(cfg.HTTP.ExpectContinueTimeout),
		MaxIdleConns:          cfg.HTTP.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.HTTP.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cfg.HTTP.MaxConnsPerHost,
		Transport:             cfg.HTTP.Transport,
		TLSConfig: exthttp.TLSConfig{
			CAFile:     cfg.HTTP.TLSConfig.CAPath,
			CertFile:   cfg.HTTP.TLSConfig.CertPath,
			KeyFile:    cfg.HTTP.TLSConfig.KeyPath,
			ServerName: cfg.HTTP.TLSConfig.ServerName,
		},
	}
}

// NewBaseHTTPTransport builds the HTTP transport (with TLS config) that the Thanos S3
// client would construct internally, so callers can wrap it (e.g. with hedging) and
// re-inject it via cfg.HTTP.Transport without losing TLS settings.
// See issue #21854 for the motivating bug.
func NewBaseHTTPTransport(cfg Config) (http.RoundTripper, error) {
	return exthttp.DefaultTransport(newThanosHTTPConfig(cfg))
}
