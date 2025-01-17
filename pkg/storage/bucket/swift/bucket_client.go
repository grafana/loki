package swift

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/swift"
)

// NewBucketClient creates a new Swift bucket client
func NewBucketClient(cfg Config, _ string, logger log.Logger, wrapper func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	bucketConfig := swift.Config{
		ApplicationCredentialID:     cfg.ApplicationCredentialID,
		ApplicationCredentialName:   cfg.ApplicationCredentialName,
		ApplicationCredentialSecret: cfg.ApplicationCredentialSecret.String(),
		AuthVersion:                 cfg.AuthVersion,
		AuthUrl:                     cfg.AuthURL,
		Username:                    cfg.Username,
		UserDomainName:              cfg.UserDomainName,
		UserDomainID:                cfg.UserDomainID,
		UserId:                      cfg.UserID,
		Password:                    cfg.Password.String(),
		DomainId:                    cfg.DomainID,
		DomainName:                  cfg.DomainName,
		ProjectID:                   cfg.ProjectID,
		ProjectName:                 cfg.ProjectName,
		ProjectDomainID:             cfg.ProjectDomainID,
		ProjectDomainName:           cfg.ProjectDomainName,
		RegionName:                  cfg.RegionName,
		ContainerName:               cfg.ContainerName,
		Retries:                     cfg.MaxRetries,
		ConnectTimeout:              model.Duration(cfg.ConnectTimeout),
		Timeout:                     model.Duration(cfg.RequestTimeout),
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
				CAFile:     cfg.HTTP.TLSConfig.CAPath,
				CertFile:   cfg.HTTP.TLSConfig.CertPath,
				KeyFile:    cfg.HTTP.TLSConfig.KeyPath,
				ServerName: cfg.HTTP.TLSConfig.ServerName,
			},
		},

		// Hard-coded defaults.
		ChunkSize:              swift.DefaultConfig.ChunkSize,
		UseDynamicLargeObjects: false,
	}

	return swift.NewContainerFromConfig(logger, &bucketConfig, false, wrapper)
}
