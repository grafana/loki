package azure

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/mattn/go-ieproxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/log"
)

const (
	// Environment
	azureGlobal       = "AzureGlobal"
	azureChinaCloud   = "AzureChinaCloud"
	azureGermanCloud  = "AzureGermanCloud"
	azureUSGovernment = "AzureUSGovernment"
)

var (
	supportedEnvironments = []string{azureGlobal, azureChinaCloud, azureGermanCloud, azureUSGovernment}
	endpoints             = map[string]struct{ blobURLFmt string }{
		azureGlobal: {
			"https://%s.blob.core.windows.net/",
		},
		azureChinaCloud: {
			"https://%s.blob.core.chinacloudapi.cn/",
		},
		azureGermanCloud: {
			"https://%s.blob.core.cloudapi.de/",
		},
		azureUSGovernment: {
			"https://%s.blob.core.usgovcloudapi.net/",
		},
	}

	// default Azure http client.
	defaultClientFactory = func() *http.Client {
		return &http.Client{
			Transport: &http.Transport{
				Proxy: ieproxy.GetProxyFunc(),
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).Dial,
				MaxIdleConns:           200,
				MaxIdleConnsPerHost:    200,
				IdleConnTimeout:        90 * time.Second,
				TLSHandshakeTimeout:    10 * time.Second,
				ExpectContinueTimeout:  1 * time.Second,
				DisableKeepAlives:      false,
				DisableCompression:     false,
				MaxResponseHeaderBytes: 0,
			},
		}
	}
)

// BlobStorage is used to interact with azure blob storage for setting or getting time series chunks.
// Implements ObjectStorage
type BlobStorage struct {
	// blobService storage.Serv
	cfg *BlobStorageConfig

	metrics BlobStorageMetrics

	containerClient        azblob.ContainerClient
	hedgingContainerClient azblob.ContainerClient
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig, metrics BlobStorageMetrics, hedgingCfg hedging.Config) (*BlobStorage, error) {
	log.WarnExperimentalUse("Azure Blob Storage", log.Logger)

	cc, err := newContainerClient(cfg, hedgingCfg, false)
	if err != nil {
		return nil, err
	}

	hcc, err := newContainerClient(cfg, hedgingCfg, true)
	if err != nil {
		return nil, err
	}

	return &BlobStorage{
		cfg:                    cfg,
		metrics:                metrics,
		containerClient:        cc,
		hedgingContainerClient: hcc,
	}, nil
}

// Stop is a no op, as there are no background workers with this driver currently
func (b *BlobStorage) Stop() {}

// GetObject returns a reader and the size for the specified object key.
func (b *BlobStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var cancel context.CancelFunc = func() {}
	if b.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.cfg.RequestTimeout)
	}

	var (
		size int64
		rc   io.ReadCloser
	)
	err := instrument.CollectedRequest(ctx, "azure.GetObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		rc, size, err = b.getObject(ctx, objectKey)
		return err
	})
	b.metrics.egressBytesTotal.Add(float64(size))
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, 0, err
	}
	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return chunk_util.NewReadCloserWithContextCancelFunc(rc, cancel), size, nil
}

func (b *BlobStorage) getObject(ctx context.Context, objectKey string) (rc io.ReadCloser, size int64, err error) {
	client := b.getBlobClient(objectKey, true)
	resp, err := client.Download(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	return resp.Body(azblob.RetryReaderOptions{MaxRetryRequests: b.cfg.MaxRetries}), *resp.ContentLength, nil
}

func (b *BlobStorage) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "azure.PutObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		client := b.getBlobClient(objectKey, false)

		_, err := client.UploadStreamToBlockBlob(
			ctx,
			object,
			azblob.UploadStreamToBlockBlobOptions{
				BufferSize: b.cfg.UploadBufferSize,
				MaxBuffers: b.cfg.UploadBufferCount,
			},
		)

		return err
	})
}

func (b *BlobStorage) getBlobClient(blobID string, hedging bool) azblob.BlockBlobClient {
	blobID = strings.ReplaceAll(blobID, ":", "-")

	if hedging {
		return b.hedgingContainerClient.NewBlockBlobClient(blobID)
	}
	return b.containerClient.NewBlockBlobClient(blobID)
}

func newContainerClient(cfg *BlobStorageConfig, hedgingCfg hedging.Config, hedging bool) (azblob.ContainerClient, error) {
	var makeService serviceConstructor = sharedKeyService
	if cfg.UseManagedIdentity {
		makeService = managedService
	}

	client, err := getHTTPClient(hedgingCfg, hedging)
	if err != nil {
		return azblob.ContainerClient{}, err
	}

	maxRetries := (int32)(cfg.MaxRetries)
	if maxRetries == 0 {
		// The SDK uses the default value to mean 3 retries and -1 to mean 0 retries
		maxRetries = -1
	}

	service, err := makeService(cfg,
		&azblob.ClientOptions{
			Transporter: client,
			Retry: policy.RetryOptions{
				MaxRetries:    -1,
				TryTimeout:    cfg.RequestTimeout,
				RetryDelay:    cfg.MinRetryDelay,
				MaxRetryDelay: cfg.MaxRetryDelay,
			},
		},
	)
	if err != nil {
		return azblob.ContainerClient{}, err
	}
	return service.NewContainerClient(cfg.ContainerName), nil
}

type serviceConstructor func(cfg *BlobStorageConfig, opts *azblob.ClientOptions) (azblob.ServiceClient, error)

func managedService(cfg *BlobStorageConfig, opts *azblob.ClientOptions) (azblob.ServiceClient, error) {
	// DefaultAzureCredential will check the env for creds or create a new Managed Identity
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return azblob.ServiceClient{}, nil
	}

	return azblob.NewServiceClient(fmt.Sprintf(accountURL(cfg.Environment), cfg.AccountName), cred, opts)
}

func sharedKeyService(cfg *BlobStorageConfig, opts *azblob.ClientOptions) (azblob.ServiceClient, error) {
	cred, err := azblob.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey.String())
	if err != nil {
		return azblob.ServiceClient{}, err
	}

	return azblob.NewServiceClientWithSharedKey(fmt.Sprintf(accountURL(cfg.Environment), cfg.AccountName), cred, opts)
}

func accountURL(env string) string {
	return endpoints[env].blobURLFmt
}

func getHTTPClient(hedgingCfg hedging.Config, hedging bool) (*http.Client, error) {
	client := defaultClientFactory()
	if hedging {
		return hedgingCfg.ClientWithRegisterer(client, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
	}
	return client, nil
}

// List implements chunk.ObjectClient.
func (b *BlobStorage) List(ctx context.Context, prefix, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	var storageObjects []chunk.StorageObject
	var commonPrefixes []chunk.StorageCommonPrefix

	err := instrument.CollectedRequest(ctx, "azure.List", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		pager := b.containerClient.ListBlobsHierarchy(delimiter, &azblob.ContainerListBlobHierarchySegmentOptions{Prefix: &prefix})
		for pager.NextPage(ctx) {
			resp := pager.PageResponse()
			for _, blobInfo := range resp.Segment.BlobItems {
				storageObjects = append(storageObjects, chunk.StorageObject{
					Key:        *blobInfo.Name,
					ModifiedAt: *blobInfo.Properties.LastModified,
				})
			}

			for _, blobPrefix := range resp.Segment.BlobPrefixes {
				commonPrefixes = append(commonPrefixes, chunk.StorageCommonPrefix(*blobPrefix.Name))
			}
		}

		return pager.Err()
	})

	if err != nil {
		return nil, nil, err
	}

	return storageObjects, commonPrefixes, nil
}

func (b *BlobStorage) DeleteObject(ctx context.Context, blobID string) error {
	return instrument.CollectedRequest(ctx, "azure.DeleteObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		client := b.getBlobClient(blobID, false)
		_, err := client.Delete(ctx, nil)
		return err
	})
}

// Validate the config.
func (c *BlobStorageConfig) Validate() error {
	if !util.StringsContain(supportedEnvironments, c.Environment) {
		return fmt.Errorf("unsupported Azure blob storage environment: %s, please select one of: %s ", c.Environment, strings.Join(supportedEnvironments, ", "))
	}
	return nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (b *BlobStorage) IsObjectNotFoundErr(err error) bool {
	var e azblob.StorageError
	return errors.As(err, &e) && e.ErrorCode == azblob.StorageErrorCodeBlobNotFound
}
