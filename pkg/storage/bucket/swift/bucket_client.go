package swift

import (
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/swift"
	yaml "gopkg.in/yaml.v2"
)

// NewBucketClient creates a new Swift bucket client
func NewBucketClient(cfg Config, _ string, logger log.Logger) (objstore.Bucket, error) {
	bucketConfig := swift.Config{
		AuthVersion:       cfg.AuthVersion,
		AuthUrl:           cfg.AuthURL,
		Username:          cfg.Username,
		UserDomainName:    cfg.UserDomainName,
		UserDomainID:      cfg.UserDomainID,
		UserId:            cfg.UserID,
		Password:          cfg.Password,
		DomainId:          cfg.DomainID,
		DomainName:        cfg.DomainName,
		ProjectID:         cfg.ProjectID,
		ProjectName:       cfg.ProjectName,
		ProjectDomainID:   cfg.ProjectDomainID,
		ProjectDomainName: cfg.ProjectDomainName,
		RegionName:        cfg.RegionName,
		ContainerName:     cfg.ContainerName,
		Retries:           cfg.MaxRetries,
		ConnectTimeout:    model.Duration(cfg.ConnectTimeout),
		Timeout:           model.Duration(cfg.RequestTimeout),
		HTTPConfig: exthttp.HTTPConfig{
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
				CAFile: cfg.HTTP.CAFile,
			},
		},

		// Hard-coded defaults.
		ChunkSize:              swift.DefaultConfig.ChunkSize,
		UseDynamicLargeObjects: false,
	}

	// Thanos currently doesn't support passing the config as is, but expects a YAML,
	// so we're going to serialize it.
	serialized, err := yaml.Marshal(bucketConfig)
	if err != nil {
		return nil, err
	}

	return swift.NewContainer(logger, serialized, nil)
}
