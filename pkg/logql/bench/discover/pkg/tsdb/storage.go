package tsdb

import (
	"context"
	"fmt"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	shipperstorage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
)

var (
	newS3ObjectClient = func(cfg aws.S3Config, hedgingCfg hedging.Config) (client.ObjectClient, error) {
		return aws.NewS3ObjectClient(cfg, hedgingCfg)
	}

	newGCSObjectClient = func(ctx context.Context, cfg gcp.GCSConfig, hedgingCfg hedging.Config) (client.ObjectClient, error) {
		return gcp.NewGCSObjectClient(ctx, cfg, hedgingCfg)
	}

	newAzureObjectClient = func(cfg *azure.BlobStorageConfig, hedgingCfg hedging.Config) (client.ObjectClient, error) {
		return azure.NewBlobStorage(cfg, getAzureBlobMetrics(), hedgingCfg)
	}

	newFSObjectClient = func(cfg local.FSConfig) (client.ObjectClient, error) {
		return local.NewFSObjectClient(cfg)
	}
)

var (
	azureBlobMetricsOnce sync.Once
	azureBlobMetrics     azure.BlobStorageMetrics
)

func getAzureBlobMetrics() azure.BlobStorageMetrics {
	azureBlobMetricsOnce.Do(func() {
		azureBlobMetrics = azure.NewBlobStorageMetrics()
	})

	return azureBlobMetrics
}

func NewObjectClient(cfg StorageConfig) (client.ObjectClient, error) {
	switch cfg.StorageType {
	case StorageTypeS3:
		return newS3ObjectClient(cfg.S3, hedging.Config{})
	case StorageTypeGCS:
		return newGCSObjectClient(context.Background(), cfg.GCS, hedging.Config{})
	case StorageTypeAzure:
		return newAzureObjectClient(&cfg.Azure, hedging.Config{})
	case StorageTypeFilesystem:
		return newFSObjectClient(cfg.Filesystem)
	default:
		return nil, fmt.Errorf("unsupported --storage-type %q (supported: s3, gcs, azure, filesystem)", cfg.StorageType)
	}
}

func NewIndexStorageClient(cfg StorageConfig) (shipperstorage.Client, error) {
	if err := cfg.NormalizeAndValidate(); err != nil {
		return nil, err
	}

	objClient, err := NewObjectClient(cfg)
	if err != nil {
		return nil, err
	}

	return shipperstorage.NewIndexStorageClient(objClient, cfg.Prefix), nil
}
