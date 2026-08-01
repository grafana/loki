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
	objstoreConfig := objstoreoci.Config{
		Provider:             cfg.Provider,
		Bucket:               cfg.Bucket,
		Compartment:          cfg.CompartmentOCID,
		Tenancy:              cfg.TenancyOCID,
		User:                 cfg.UserOCID,
		Region:               cfg.Region,
		Fingerprint:          cfg.Fingerprint,
		PrivateKey:           cfg.PrivateKey,
		Passphrase:           cfg.Passphrase,
		PartSize:             cfg.PartSize,
		MaxRequestRetries:    cfg.MaxRequestRetries,
		RequestRetryInterval: cfg.RequestRetryInterval,
		HTTPConfig:           cfg.HTTPConfig,
	}

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
