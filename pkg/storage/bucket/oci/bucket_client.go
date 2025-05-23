package oci

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/oci"

	"gopkg.in/yaml.v2"
)

// NewBucketClient creates a new OCI bucket client
func NewBucketClient(cfg Config, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	// The objstore library currently requires the config to be
	// passed as bytes, so this YAML->struct->YAML->struct marshal
	// chain is necessary
	bucketData, _ := yaml.Marshal(cfg)
	return oci.NewBucket(logger, bucketData, wrapRT)
}
