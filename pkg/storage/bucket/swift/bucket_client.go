package swift

import (
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/swift"
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

		// Hard-coded defaults.
		ChunkSize:              swift.DefaultConfig.ChunkSize,
		UseDynamicLargeObjects: false,
	}

	return swift.NewContainerFromConfig(logger, &bucketConfig, false)
}
