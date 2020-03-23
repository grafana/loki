package stores

import (
	"context"
	"fmt"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	cortex_local "github.com/cortexproject/cortex/pkg/chunk/local"

	"github.com/grafana/loki/pkg/storage/stores/local"
)

// NewObjectClient makes a new ObjectClient of the desired type.
func NewObjectClient(cfg local.StoreConfig) (chunk.ObjectClient, error) {
	switch cfg.Store {
	case "aws", "s3":
		return aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config)
	case "gcs":
		return gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig)
	case "azure":
		return azure.NewBlobStorage(&cfg.Azure)
	case "filesystem":
		return cortex_local.NewFSObjectClient(cfg.FSConfig)
	default:
		return nil, fmt.Errorf("unrecognized storage client %v, choose one of: aws, s3, gcp, azure, filesystem", cfg.Store)
	}
}
