package stores

import (
	"context"
	"fmt"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	cortex_local "github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
)

// NewObjectClient makes a new ObjectClient of the desired type.
func NewObjectClient(storeType string, cfg storage.Config) (chunk.ObjectClient, error) {
	switch storeType {
	case "aws", "s3":
		return aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config)
	case "gcs":
		return gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig)
	case "azure":
		return azure.NewBlobStorage(&cfg.AzureStorageConfig)
	case "filesystem":
		return cortex_local.NewFSObjectClient(cfg.FSConfig)
	default:
		return nil, fmt.Errorf("unrecognized storage client %v, choose one of: aws, s3, gcp, azure, filesystem", storeType)
	}
}
