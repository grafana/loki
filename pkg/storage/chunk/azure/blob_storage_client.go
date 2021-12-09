package azure

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/mattn/go-ieproxy"
	"github.com/prometheus/client_golang/prometheus"

	cortex_azure "github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/log"
	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
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
	noClientKey           = azblob.ClientProvidedKeyOptions{}
	endpoints             = map[string]struct{ blobURLFmt, containerURLFmt string }{
		azureGlobal: {
			"https://%s.blob.core.windows.net/%s/%s",
			"https://%s.blob.core.windows.net/%s",
		},
		azureChinaCloud: {
			"https://%s.blob.core.chinacloudapi.cn/%s/%s",
			"https://%s.blob.core.chinacloudapi.cn/%s",
		},
		azureGermanCloud: {
			"https://%s.blob.core.cloudapi.de/%s/%s",
			"https://%s.blob.core.cloudapi.de/%s",
		},
		azureUSGovernment: {
			"https://%s.blob.core.usgovcloudapi.net/%s/%s",
			"https://%s.blob.core.usgovcloudapi.net/%s",
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
				MaxIdleConns:           0,
				MaxIdleConnsPerHost:    100,
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

// BlobStorageConfig defines the configurable flags that can be defined when using azure blob storage.
type BlobStorageConfig struct {
	Environment        string         `yaml:"environment"`
	ContainerName      string         `yaml:"container_name"`
	AccountName        string         `yaml:"account_name"`
	AccountKey         flagext.Secret `yaml:"account_key"`
	DownloadBufferSize int            `yaml:"download_buffer_size"`
	UploadBufferSize   int            `yaml:"upload_buffer_size"`
	UploadBufferCount  int            `yaml:"upload_buffer_count"`
	RequestTimeout     time.Duration  `yaml:"request_timeout"`
	MaxRetries         int            `yaml:"max_retries"`
	MinRetryDelay      time.Duration  `yaml:"min_retry_delay"`
	MaxRetryDelay      time.Duration  `yaml:"max_retry_delay"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Environment, prefix+"azure.environment", azureGlobal, fmt.Sprintf("Azure Cloud environment. Supported values are: %s.", strings.Join(supportedEnvironments, ", ")))
	f.StringVar(&c.ContainerName, prefix+"azure.container-name", "cortex", "Name of the blob container used to store chunks. This container must be created before running cortex.")
	f.StringVar(&c.AccountName, prefix+"azure.account-name", "", "The Microsoft Azure account name to be used")
	f.Var(&c.AccountKey, prefix+"azure.account-key", "The Microsoft Azure account key to use.")
	f.DurationVar(&c.RequestTimeout, prefix+"azure.request-timeout", 30*time.Second, "Timeout for requests made against azure blob storage.")
	f.IntVar(&c.DownloadBufferSize, prefix+"azure.download-buffer-size", 512000, "Preallocated buffer size for downloads.")
	f.IntVar(&c.UploadBufferSize, prefix+"azure.upload-buffer-size", 256000, "Preallocated buffer size for uploads.")
	f.IntVar(&c.UploadBufferCount, prefix+"azure.download-buffer-count", 1, "Number of buffers used to used to upload a chunk.")
	f.IntVar(&c.MaxRetries, prefix+"azure.max-retries", 5, "Number of retries for a request which times out.")
	f.DurationVar(&c.MinRetryDelay, prefix+"azure.min-retry-delay", 10*time.Millisecond, "Minimum time to wait before retrying a request.")
	f.DurationVar(&c.MaxRetryDelay, prefix+"azure.max-retry-delay", 500*time.Millisecond, "Maximum time to wait before retrying a request.")
}

func (c *BlobStorageConfig) ToCortexAzureConfig() cortex_azure.BlobStorageConfig {
	return cortex_azure.BlobStorageConfig{
		Environment:        c.Environment,
		ContainerName:      c.ContainerName,
		AccountName:        c.AccountName,
		AccountKey:         c.AccountKey,
		DownloadBufferSize: c.DownloadBufferSize,
		UploadBufferSize:   c.UploadBufferSize,
		UploadBufferCount:  c.UploadBufferCount,
		RequestTimeout:     c.RequestTimeout,
		MaxRetries:         c.MaxRetries,
		MinRetryDelay:      c.MinRetryDelay,
		MaxRetryDelay:      c.MaxRetryDelay,
	}
}

// BlobStorage is used to interact with azure blob storage for setting or getting time series chunks.
// Implements ObjectStorage
type BlobStorage struct {
	// blobService storage.Serv
	cfg *BlobStorageConfig

	containerURL azblob.ContainerURL

	pipeline        pipeline.Pipeline
	hedgingPipeline pipeline.Pipeline
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig, hedgingCfg hedging.Config) (*BlobStorage, error) {
	log.WarnExperimentalUse("Azure Blob Storage")
	blobStorage := &BlobStorage{
		cfg: cfg,
	}
	pipeline, err := blobStorage.newPipeline(hedgingCfg, false)
	if err != nil {
		return nil, err
	}
	blobStorage.pipeline = pipeline
	hedgingPipeline, err := blobStorage.newPipeline(hedgingCfg, true)
	if err != nil {
		return nil, err
	}
	blobStorage.hedgingPipeline = hedgingPipeline
	blobStorage.containerURL, err = blobStorage.buildContainerURL()
	if err != nil {
		return nil, err
	}

	return blobStorage, nil
}

// Stop is a no op, as there are no background workers with this driver currently
func (b *BlobStorage) Stop() {}

// GetObject returns a reader and the size for the specified object key.
func (b *BlobStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var cancel context.CancelFunc = func() {}
	if b.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, b.cfg.RequestTimeout)
	}

	rc, size, err := b.getObject(ctx, objectKey)
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, 0, err
	}
	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return chunk_util.NewReadCloserWithContextCancelFunc(rc, cancel), size, nil
}

func (b *BlobStorage) getObject(ctx context.Context, objectKey string) (rc io.ReadCloser, size int64, err error) {
	blockBlobURL, err := b.getBlobURL(objectKey, true)
	if err != nil {
		return nil, 0, err
	}

	// Request access to the blob
	downloadResponse, err := blockBlobURL.Download(ctx, 0, azblob.CountToEnd, azblob.BlobAccessConditions{}, false, noClientKey)
	if err != nil {
		return nil, 0, err
	}

	return downloadResponse.Body(azblob.RetryReaderOptions{MaxRetryRequests: b.cfg.MaxRetries}), downloadResponse.ContentLength(), nil
}

func (b *BlobStorage) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	blockBlobURL, err := b.getBlobURL(objectKey, false)
	if err != nil {
		return err
	}

	bufferSize := b.cfg.UploadBufferSize
	maxBuffers := b.cfg.UploadBufferCount
	_, err = azblob.UploadStreamToBlockBlob(ctx, object, blockBlobURL,
		azblob.UploadStreamToBlockBlobOptions{BufferSize: bufferSize, MaxBuffers: maxBuffers})

	return err
}

func (b *BlobStorage) getBlobURL(blobID string, hedging bool) (azblob.BlockBlobURL, error) {
	blobID = strings.Replace(blobID, ":", "-", -1)

	// generate url for new chunk blob
	u, err := url.Parse(fmt.Sprintf(b.selectBlobURLFmt(), b.cfg.AccountName, b.cfg.ContainerName, blobID))
	if err != nil {
		return azblob.BlockBlobURL{}, err
	}
	pipeline := b.pipeline
	if hedging {
		pipeline = b.hedgingPipeline
	}

	return azblob.NewBlockBlobURL(*u, pipeline), nil
}

func (b *BlobStorage) buildContainerURL() (azblob.ContainerURL, error) {
	u, err := url.Parse(fmt.Sprintf(b.selectContainerURLFmt(), b.cfg.AccountName, b.cfg.ContainerName))
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	return azblob.NewContainerURL(*u, b.pipeline), nil
}

func (b *BlobStorage) newPipeline(hedgingCfg hedging.Config, hedging bool) (pipeline.Pipeline, error) {
	credential, err := azblob.NewSharedKeyCredential(b.cfg.AccountName, b.cfg.AccountKey.Value)
	if err != nil {
		return nil, err
	}

	opts := azblob.PipelineOptions{
		Retry: azblob.RetryOptions{
			Policy:        azblob.RetryPolicyExponential,
			MaxTries:      (int32)(b.cfg.MaxRetries),
			TryTimeout:    b.cfg.RequestTimeout,
			RetryDelay:    b.cfg.MinRetryDelay,
			MaxRetryDelay: b.cfg.MaxRetryDelay,
		},
	}
	client := defaultClientFactory()

	opts.HTTPSender = pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
		return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
			resp, err := client.Do(request.WithContext(ctx))
			return pipeline.NewHTTPResponse(resp), err
		}
	})

	if hedging {
		opts.HTTPSender = pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
			client := hedgingCfg.ClientWithRegisterer(client, prometheus.WrapRegistererWithPrefix("loki", prometheus.DefaultRegisterer))
			return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
				resp, err := client.Do(request.WithContext(ctx))
				return pipeline.NewHTTPResponse(resp), err
			}
		})
	}

	return azblob.NewPipeline(credential, opts), nil
}

// List implements chunk.ObjectClient.
func (b *BlobStorage) List(ctx context.Context, prefix, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	var storageObjects []chunk.StorageObject
	var commonPrefixes []chunk.StorageCommonPrefix

	for marker := (azblob.Marker{}); marker.NotDone(); {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		listBlob, err := b.containerURL.ListBlobsHierarchySegment(ctx, marker, delimiter, azblob.ListBlobsSegmentOptions{Prefix: prefix})
		if err != nil {
			return nil, nil, err
		}

		marker = listBlob.NextMarker

		// Process the blobs returned in this result segment (if the segment is empty, the loop body won't execute)
		for _, blobInfo := range listBlob.Segment.BlobItems {
			storageObjects = append(storageObjects, chunk.StorageObject{
				Key:        blobInfo.Name,
				ModifiedAt: blobInfo.Properties.LastModified,
			})
		}

		// Process the BlobPrefixes so called commonPrefixes or synthetic directories in the listed synthetic directory
		for _, blobPrefix := range listBlob.Segment.BlobPrefixes {
			commonPrefixes = append(commonPrefixes, chunk.StorageCommonPrefix(blobPrefix.Name))
		}
	}

	return storageObjects, commonPrefixes, nil
}

func (b *BlobStorage) DeleteObject(ctx context.Context, blobID string) error {
	blockBlobURL, err := b.getBlobURL(blobID, false)
	if err != nil {
		return err
	}

	_, err = blockBlobURL.Delete(ctx, azblob.DeleteSnapshotsOptionInclude, azblob.BlobAccessConditions{})
	return err
}

// Validate the config.
func (c *BlobStorageConfig) Validate() error {
	if !util.StringsContain(supportedEnvironments, c.Environment) {
		return fmt.Errorf("unsupported Azure blob storage environment: %s, please select one of: %s ", c.Environment, strings.Join(supportedEnvironments, ", "))
	}
	return nil
}

func (b *BlobStorage) selectBlobURLFmt() string {
	return endpoints[b.cfg.Environment].blobURLFmt
}

func (b *BlobStorage) selectContainerURLFmt() string {
	return endpoints[b.cfg.Environment].containerURLFmt
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (b *BlobStorage) IsObjectNotFoundErr(err error) bool {
	var e azblob.StorageError
	if errors.As(err, &e) && e.ServiceCode() == azblob.ServiceCodeBlobNotFound {
		return true
	}

	return false
}
