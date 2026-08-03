package oci

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	objstoreoci "github.com/thanos-io/objstore/providers/oci"
	"gopkg.in/yaml.v2"
)

func NewBucketClient(
	cfg Config,
	logger log.Logger,
	wrapRoundTripper func(http.RoundTripper) http.RoundTripper,
) (objstore.Bucket, error) {
	objstoreConfig := newObjstoreConfig(cfg)
	configBytes, err := yaml.Marshal(objstoreConfig)
	if err != nil {
		return nil, err
	}

	return objstoreoci.NewBucket(
		logger,
		configBytes,
		wrapRoundTripper,
	)
}

func newObjstoreConfig(cfg Config) objstoreoci.Config {
	objstoreConfig := objstoreoci.DefaultConfig

	objstoreConfig.Provider = cfg.Provider
	objstoreConfig.Bucket = cfg.Bucket
	objstoreConfig.Compartment = cfg.CompartmentOCID
	objstoreConfig.Tenancy = cfg.TenancyOCID
	objstoreConfig.User = cfg.UserOCID
	objstoreConfig.Region = cfg.Region
	objstoreConfig.PartSize = cfg.PartSize
	objstoreConfig.MaxRequestRetries = cfg.MaxRequestRetries
	objstoreConfig.RequestRetryInterval = cfg.RequestRetryInterval
	objstoreConfig.HTTPConfig = mergeHTTPConfig(
		objstoreConfig.HTTPConfig,
		cfg.HTTPConfig,
	)

	return objstoreConfig
}

func mergeHTTPConfig(
	defaults objstoreoci.HTTPConfig,
	overrides objstoreoci.HTTPConfig,
) objstoreoci.HTTPConfig {
	if overrides.IdleConnTimeout != 0 {
		defaults.IdleConnTimeout = overrides.IdleConnTimeout
	}
	if overrides.ResponseHeaderTimeout != 0 {
		defaults.ResponseHeaderTimeout = overrides.ResponseHeaderTimeout
	}
	if overrides.InsecureSkipVerify {
		defaults.InsecureSkipVerify = true
	}
	if overrides.TLSHandshakeTimeout != 0 {
		defaults.TLSHandshakeTimeout = overrides.TLSHandshakeTimeout
	}
	if overrides.ExpectContinueTimeout != 0 {
		defaults.ExpectContinueTimeout = overrides.ExpectContinueTimeout
	}
	if overrides.MaxIdleConns != 0 {
		defaults.MaxIdleConns = overrides.MaxIdleConns
	}
	if overrides.MaxIdleConnsPerHost != 0 {
		defaults.MaxIdleConnsPerHost = overrides.MaxIdleConnsPerHost
	}
	if overrides.MaxConnsPerHost != 0 {
		defaults.MaxConnsPerHost = overrides.MaxConnsPerHost
	}
	if overrides.DisableCompression {
		defaults.DisableCompression = true
	}
	if overrides.ClientTimeout != 0 {
		defaults.ClientTimeout = overrides.ClientTimeout
	}
	if overrides.Transport != nil {
		defaults.Transport = overrides.Transport
	}

	return defaults
}
