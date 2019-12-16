package azure

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

const blobURLFmt = "https://%s.blob.core.windows.net/%s/%s"

// BlobStorageConfig defines the configurable flags that can be defined when using azure blob storage.
type BlobStorageConfig struct {
	ContainerName      string        `yaml:"container_name"`
	AccountName        string        `yaml:"account_name"`
	AccountKey         string        `yaml:"account_key"`
	DownloadBufferSize int           `yaml:"download_buffer_size"`
	UploadBufferSize   int           `yaml:"upload_buffer_size"`
	UploadBufferCount  int           `yaml:"upload_buffer_count"`
	RequestTimeout     time.Duration `yaml:"request_timeout"`
	MaxRetries         int           `yaml:"max_retries"`
	MinRetryDelay      time.Duration `yaml:"min_retry_delay"`
	MaxRetryDelay      time.Duration `yaml:"max_retry_delay"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.ContainerName, "azure.container-name", "cortex", "Name of the blob container used to store chunks. Defaults to `cortex`. This container must be created before running cortex.")
	f.StringVar(&c.AccountName, "azure.account-name", "", "The Microsoft Azure account name to be used")
	f.StringVar(&c.AccountKey, "azure.account-key", "", "The Microsoft Azure account key to use.")
	f.DurationVar(&c.RequestTimeout, "azure.request-timeout", 30*time.Second, "Timeout for requests made against azure blob storage. Defaults to 30 seconds.")
	f.IntVar(&c.DownloadBufferSize, "azure.download-buffer-size", 512000, "Preallocated buffer size for downloads (default is 512KB)")
	f.IntVar(&c.UploadBufferSize, "azure.upload-buffer-size", 256000, "Preallocated buffer size for up;oads (default is 256KB)")
	f.IntVar(&c.UploadBufferCount, "azure.download-buffer-count", 1, "Number of buffers used to used to upload a chunk. (defaults to 1)")
	f.IntVar(&c.MaxRetries, "azure.max-retries", 5, "Number of retries for a request which times out.")
	f.DurationVar(&c.MinRetryDelay, "azure.min-retry-delay", 10*time.Millisecond, "Minimum time to wait before retrying a request.")
	f.DurationVar(&c.MaxRetryDelay, "azure.max-retry-delay", 500*time.Millisecond, "Maximum time to wait before retrying a request.")
}

// BlobStorage is used to interact with azure blob storage for setting or getting time series chunks.
// Implements ObjectStorage
type BlobStorage struct {
	//blobService storage.Serv
	cfg *BlobStorageConfig
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig) *BlobStorage {
	return &BlobStorage{cfg: cfg}
}

// Stop is a no op, as there are no background workers with this driver currently
func (b *BlobStorage) Stop() {}

// GetChunks retrieves the requested data chunks from blob storage.
func (b *BlobStorage) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, chunks, b.getChunk)
}

func (b *BlobStorage) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
	if b.cfg.RequestTimeout > 0 {
		// The context will be cancelled with the timeout or when the parent context is cancelled, whichever occurs first.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.cfg.RequestTimeout)
		defer cancel()
	}

	blockBlobURL, err := b.getBlobURL(input.ExternalKey())
	if err != nil {
		return chunk.Chunk{}, err
	}

	buf := make([]byte, 0, b.cfg.DownloadBufferSize)

	err = azblob.DownloadBlobToBuffer(ctx, blockBlobURL.BlobURL, 0, 0, buf, azblob.DownloadFromBlobOptions{})
	if err != nil {
		return chunk.Chunk{}, err
	}

	if err := input.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, err
	}

	return input, nil
}

// PutChunks writes a set of chunks to azure blob storage using block blobs.
func (b *BlobStorage) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {

	for _, chunk := range chunks {
		buf, err := chunk.Encoded()
		if err != nil {
			return err
		}

		blockBlobURL, err := b.getBlobURL(chunk.ExternalKey())
		if err != nil {
			return err
		}

		bufferSize := b.cfg.UploadBufferSize
		maxBuffers := b.cfg.UploadBufferCount
		_, err = azblob.UploadStreamToBlockBlob(ctx, bytes.NewReader(buf), blockBlobURL,
			azblob.UploadStreamToBlockBlobOptions{BufferSize: bufferSize, MaxBuffers: maxBuffers})

		if err != nil {
			return err
		}
	}
	return nil
}

func (b *BlobStorage) getBlobURL(blobID string) (azblob.BlockBlobURL, error) {

	blobID = strings.Replace(blobID, ":", "-", -1)

	//generate url for new chunk blob
	u, err := url.Parse(fmt.Sprintf(blobURLFmt, b.cfg.AccountName, b.cfg.ContainerName, blobID))
	if err != nil {
		return azblob.BlockBlobURL{}, err
	}
	credential, err := azblob.NewSharedKeyCredential(b.cfg.AccountName, b.cfg.AccountKey)
	if err != nil {
		return azblob.BlockBlobURL{}, err
	}

	return azblob.NewBlockBlobURL(*u, azblob.NewPipeline(credential, azblob.PipelineOptions{
		Retry: azblob.RetryOptions{
			Policy:        azblob.RetryPolicyExponential,
			MaxTries:      (int32)(b.cfg.MaxRetries),
			TryTimeout:    b.cfg.RequestTimeout,
			RetryDelay:    b.cfg.MinRetryDelay,
			MaxRetryDelay: b.cfg.MaxRetryDelay,
		},
	})), nil
}
