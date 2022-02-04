// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal"

	"bytes"
	"errors"
	"os"
)

// HighLevelUploadToBlockBlobOption identifies options used by the UploadBufferToBlockBlob and UploadFileToBlockBlob functions.
type HighLevelUploadToBlockBlobOption struct {
	// BlockSize specifies the block size to use; the default (and maximum size) is BlockBlobMaxStageBlockBytes.
	BlockSize int64

	// Progress is a function that is invoked periodically as bytes are sent to the BlockBlobClient.
	// Note that the progress reporting is not always increasing; it can go down when retrying a request.
	Progress func(bytesTransferred int64)

	// HTTPHeaders indicates the HTTP headers to be associated with the blob.
	HTTPHeaders *BlobHTTPHeaders

	// Metadata indicates the metadata to be associated with the blob when PutBlockList is called.
	Metadata map[string]string

	// BlobAccessConditions indicates the access conditions for the block blob.
	BlobAccessConditions *BlobAccessConditions

	// AccessTier indicates the tier of blob
	AccessTier *AccessTier

	// TagsMap
	TagsMap map[string]string

	// ClientProvidedKeyOptions indicates the client provided key by name and/or by value to encrypt/decrypt data.
	CpkInfo      *CpkInfo
	CpkScopeInfo *CpkScopeInfo

	// Parallelism indicates the maximum number of blocks to upload in parallel (0=default)
	Parallelism uint16
	// Optional header, Specifies the transactional crc64 for the body, to be validated by the service.
	TransactionalContentCRC64 *[]byte
	// Specify the transactional md5 for the body, to be validated by the service.
	TransactionalContentMD5 *[]byte
}

func (o HighLevelUploadToBlockBlobOption) getStageBlockOptions() *StageBlockOptions {
	leaseAccessConditions, _ := o.BlobAccessConditions.pointers()
	return &StageBlockOptions{
		CpkInfo:               o.CpkInfo,
		CpkScopeInfo:          o.CpkScopeInfo,
		LeaseAccessConditions: leaseAccessConditions,
	}
}

func (o HighLevelUploadToBlockBlobOption) getUploadBlockBlobOptions() *UploadBlockBlobOptions {
	return &UploadBlockBlobOptions{
		TagsMap:              o.TagsMap,
		Metadata:             o.Metadata,
		Tier:                 o.AccessTier,
		HTTPHeaders:          o.HTTPHeaders,
		BlobAccessConditions: o.BlobAccessConditions,
		CpkInfo:              o.CpkInfo,
		CpkScopeInfo:         o.CpkScopeInfo,
	}
}

func (o *HighLevelUploadToBlockBlobOption) getCommitBlockListOptions() *CommitBlockListOptions {
	return &CommitBlockListOptions{
		BlobTagsMap:     o.TagsMap,
		Metadata:        o.Metadata,
		Tier:            o.AccessTier,
		BlobHTTPHeaders: o.HTTPHeaders,
		CpkInfo:         o.CpkInfo,
		CpkScopeInfo:    o.CpkScopeInfo,
	}
}

// uploadReaderAtToBlockBlob uploads a buffer in blocks to a block blob.
func (bb BlockBlobClient) uploadReaderAtToBlockBlob(ctx context.Context, reader io.ReaderAt, readerSize int64, o HighLevelUploadToBlockBlobOption) (*http.Response, error) {
	if o.BlockSize == 0 {
		// If bufferSize > (BlockBlobMaxStageBlockBytes * BlockBlobMaxBlocks), then error
		if readerSize > BlockBlobMaxStageBlockBytes*BlockBlobMaxBlocks {
			return nil, errors.New("buffer is too large to upload to a block blob")
		}
		// If bufferSize <= BlockBlobMaxUploadBlobBytes, then Upload should be used with just 1 I/O request
		if readerSize <= BlockBlobMaxUploadBlobBytes {
			o.BlockSize = BlockBlobMaxUploadBlobBytes // Default if unspecified
		} else {
			o.BlockSize = readerSize / BlockBlobMaxBlocks   // buffer / max blocks = block size to use all 50,000 blocks
			if o.BlockSize < BlobDefaultDownloadBlockSize { // If the block size is smaller than 4MB, round up to 4MB
				o.BlockSize = BlobDefaultDownloadBlockSize
			}
			// StageBlock will be called with blockSize blocks and a Parallelism of (BufferSize / BlockSize).
		}
	}

	if readerSize <= BlockBlobMaxUploadBlobBytes {
		// If the size can fit in 1 Upload call, do it this way
		var body io.ReadSeeker = io.NewSectionReader(reader, 0, readerSize)
		if o.Progress != nil {
			body = streaming.NewRequestProgress(internal.NopCloser(body), o.Progress)
		}

		uploadBlockBlobOptions := o.getUploadBlockBlobOptions()
		resp, err := bb.Upload(ctx, internal.NopCloser(body), uploadBlockBlobOptions)

		return resp.RawResponse, err
	}

	var numBlocks = uint16(((readerSize - 1) / o.BlockSize) + 1)

	blockIDList := make([]string, numBlocks) // Base-64 encoded block IDs
	progress := int64(0)
	progressLock := &sync.Mutex{}

	err := DoBatchTransfer(ctx, BatchTransferOptions{
		OperationName: "uploadReaderAtToBlockBlob",
		TransferSize:  readerSize,
		ChunkSize:     o.BlockSize,
		Parallelism:   o.Parallelism,
		Operation: func(offset int64, count int64, ctx context.Context) error {
			// This function is called once per block.
			// It is passed this block's offset within the buffer and its count of bytes
			// Prepare to read the proper block/section of the buffer
			var body io.ReadSeeker = io.NewSectionReader(reader, offset, count)
			blockNum := offset / o.BlockSize
			if o.Progress != nil {
				blockProgress := int64(0)
				body = streaming.NewRequestProgress(internal.NopCloser(body),
					func(bytesTransferred int64) {
						diff := bytesTransferred - blockProgress
						blockProgress = bytesTransferred
						progressLock.Lock() // 1 goroutine at a time gets progress report
						progress += diff
						o.Progress(progress)
						progressLock.Unlock()
					})
			}

			// Block IDs are unique values to avoid issue if 2+ clients are uploading blocks
			// at the same time causing PutBlockList to get a mix of blocks from all the clients.
			generatedUuid, err := uuid.New()
			if err != nil {
				return err
			}
			blockIDList[blockNum] = base64.StdEncoding.EncodeToString([]byte(generatedUuid.String()))
			stageBlockOptions := o.getStageBlockOptions()
			_, err = bb.StageBlock(ctx, blockIDList[blockNum], internal.NopCloser(body), stageBlockOptions)
			return err
		},
	})
	if err != nil {
		return nil, err
	}
	// All put blocks were successful, call Put Block List to finalize the blob
	commitBlockListOptions := o.getCommitBlockListOptions()
	resp, err := bb.CommitBlockList(ctx, blockIDList, commitBlockListOptions)

	return resp.RawResponse, err
}

// UploadBufferToBlockBlob uploads a buffer in blocks to a block blob.
func (bb BlockBlobClient) UploadBufferToBlockBlob(ctx context.Context, b []byte, o HighLevelUploadToBlockBlobOption) (*http.Response, error) {
	return bb.uploadReaderAtToBlockBlob(ctx, bytes.NewReader(b), int64(len(b)), o)
}

// UploadFileToBlockBlob uploads a file in blocks to a block blob.
func (bb BlockBlobClient) UploadFileToBlockBlob(ctx context.Context, file *os.File, o HighLevelUploadToBlockBlobOption) (*http.Response, error) {

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return bb.uploadReaderAtToBlockBlob(ctx, file, stat.Size(), o)
}

///////////////////////////////////////////////////////////////////////////////

const BlobDefaultDownloadBlockSize = int64(4 * 1024 * 1024) // 4MB

// HighLevelDownloadFromBlobOptions identifies options used by the DownloadBlobToBuffer and DownloadBlobToFile functions.
type HighLevelDownloadFromBlobOptions struct {
	// BlockSize specifies the block size to use for each parallel download; the default size is BlobDefaultDownloadBlockSize.
	BlockSize int64

	// Progress is a function that is invoked periodically as bytes are received.
	Progress func(bytesTransferred int64)

	// BlobAccessConditions indicates the access conditions used when making HTTP GET requests against the blob.
	BlobAccessConditions *BlobAccessConditions

	// ClientProvidedKeyOptions indicates the client provided key by name and/or by value to encrypt/decrypt data.
	CpkInfo      *CpkInfo
	CpkScopeInfo *CpkScopeInfo

	// Parallelism indicates the maximum number of blocks to download in parallel (0=default)
	Parallelism uint16

	// RetryReaderOptionsPerBlock is used when downloading each block.
	RetryReaderOptionsPerBlock RetryReaderOptions
}

func (o *HighLevelDownloadFromBlobOptions) getBlobPropertiesOptions() *GetBlobPropertiesOptions {
	return &GetBlobPropertiesOptions{
		BlobAccessConditions: o.BlobAccessConditions,
		CpkInfo:              o.CpkInfo,
	}
}

func (o *HighLevelDownloadFromBlobOptions) getDownloadBlobOptions(offSet, count int64, rangeGetContentMD5 *bool) *DownloadBlobOptions {
	return &DownloadBlobOptions{
		BlobAccessConditions: o.BlobAccessConditions,
		CpkInfo:              o.CpkInfo,
		CpkScopeInfo:         o.CpkScopeInfo,
		Offset:               &offSet,
		Count:                &count,
		RangeGetContentMD5:   rangeGetContentMD5,
	}
}

// DownloadBlobToWriterAt downloads an Azure blob to a WriterAt with parallel.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b BlobClient) DownloadBlobToWriterAt(ctx context.Context, offset int64, count int64, writer io.WriterAt, o HighLevelDownloadFromBlobOptions) error {
	if o.BlockSize == 0 {
		o.BlockSize = BlobDefaultDownloadBlockSize
	}

	if count == CountToEnd { // If size not specified, calculate it
		// If we don't have the length at all, get it
		downloadBlobOptions := o.getDownloadBlobOptions(0, CountToEnd, nil)
		dr, err := b.Download(ctx, downloadBlobOptions)
		if err != nil {
			return err
		}
		count = *dr.ContentLength - offset
	}

	if count <= 0 {
		// The file is empty, there is nothing to download.
		return nil
	}

	// Prepare and do parallel download.
	progress := int64(0)
	progressLock := &sync.Mutex{}

	err := DoBatchTransfer(ctx, BatchTransferOptions{
		OperationName: "downloadBlobToWriterAt",
		TransferSize:  count,
		ChunkSize:     o.BlockSize,
		Parallelism:   o.Parallelism,
		Operation: func(chunkStart int64, count int64, ctx context.Context) error {

			downloadBlobOptions := o.getDownloadBlobOptions(chunkStart+offset, count, nil)
			dr, err := b.Download(ctx, downloadBlobOptions)
			if err != nil {
				return err
			}
			body := dr.Body(o.RetryReaderOptionsPerBlock)
			if o.Progress != nil {
				rangeProgress := int64(0)
				body = streaming.NewResponseProgress(
					body,
					func(bytesTransferred int64) {
						diff := bytesTransferred - rangeProgress
						rangeProgress = bytesTransferred
						progressLock.Lock()
						progress += diff
						o.Progress(progress)
						progressLock.Unlock()
					})
			}
			_, err = io.Copy(newSectionWriter(writer, chunkStart, count), body)
			if err != nil {
				return err
			}
			err = body.Close()
			return err
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// DownloadBlobToBuffer downloads an Azure blob to a buffer with parallel.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b BlobClient) DownloadBlobToBuffer(ctx context.Context, offset int64, count int64, _bytes []byte, o HighLevelDownloadFromBlobOptions) error {
	return b.DownloadBlobToWriterAt(ctx, offset, count, newBytesWriter(_bytes), o)
}

// DownloadBlobToFile downloads an Azure blob to a local file.
// The file would be truncated if the size doesn't match.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b BlobClient) DownloadBlobToFile(ctx context.Context, offset int64, count int64, file *os.File, o HighLevelDownloadFromBlobOptions) error {
	// 1. Calculate the size of the destination file
	var size int64

	if count == CountToEnd {
		// Try to get Azure blob's size
		getBlobPropertiesOptions := o.getBlobPropertiesOptions()
		props, err := b.GetProperties(ctx, getBlobPropertiesOptions)
		if err != nil {
			return err
		}
		size = *props.ContentLength - offset
	} else {
		size = count
	}

	// 2. Compare and try to resize local file's size if it doesn't match Azure blob's size.
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != size {
		if err = file.Truncate(size); err != nil {
			return err
		}
	}

	if size > 0 {
		return b.DownloadBlobToWriterAt(ctx, offset, size, file, o)
	} else { // if the blob's size is 0, there is no need in downloading it
		return nil
	}
}

///////////////////////////////////////////////////////////////////////////////

// BatchTransferOptions identifies options used by DoBatchTransfer.
type BatchTransferOptions struct {
	TransferSize  int64
	ChunkSize     int64
	Parallelism   uint16
	Operation     func(offset int64, chunkSize int64, ctx context.Context) error
	OperationName string
}

// DoBatchTransfer helps to execute operations in a batch manner.
// Can be used by users to customize batch works (for other scenarios that the SDK does not provide)
func DoBatchTransfer(ctx context.Context, o BatchTransferOptions) error {
	if o.ChunkSize == 0 {
		return errors.New("ChunkSize cannot be 0")
	}

	if o.Parallelism == 0 {
		o.Parallelism = 5 // default Parallelism
	}

	// Prepare and do parallel operations.
	numChunks := uint16(((o.TransferSize - 1) / o.ChunkSize) + 1)
	operationChannel := make(chan func() error, o.Parallelism) // Create the channel that release 'Parallelism' goroutines concurrently
	operationResponseChannel := make(chan error, numChunks)    // Holds each response
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the goroutines that process each operation (in parallel).
	for g := uint16(0); g < o.Parallelism; g++ {
		//grIndex := g
		go func() {
			for f := range operationChannel {
				err := f()
				operationResponseChannel <- err
			}
		}()
	}

	// Add each chunk's operation to the channel.
	for chunkNum := uint16(0); chunkNum < numChunks; chunkNum++ {
		curChunkSize := o.ChunkSize

		if chunkNum == numChunks-1 { // Last chunk
			curChunkSize = o.TransferSize - (int64(chunkNum) * o.ChunkSize) // Remove size of all transferred chunks from total
		}
		offset := int64(chunkNum) * o.ChunkSize

		operationChannel <- func() error {
			return o.Operation(offset, curChunkSize, ctx)
		}
	}
	close(operationChannel)

	// Wait for the operations to complete.
	var firstErr error = nil
	for chunkNum := uint16(0); chunkNum < numChunks; chunkNum++ {
		responseError := <-operationResponseChannel
		// record the first error (the original error which should cause the other chunks to fail with canceled context)
		if responseError != nil && firstErr == nil {
			cancel() // As soon as any operation fails, cancel all remaining operation calls
			firstErr = responseError
		}
	}
	return firstErr
}

////////////////////////////////////////////////////////////////////////////////////////////////

// TransferManager provides a buffer and thread pool manager for certain transfer options.
// It is undefined behavior if code outside of this package call any of these methods.
type TransferManager interface {
	// Get provides a buffer that will be used to read data into and write out to the stream.
	// It is guaranteed by this package to not read or write beyond the size of the slice.
	Get() []byte
	// Put may or may not put the buffer into underlying storage, depending on settings.
	// The buffer must not be touched after this has been called.
	Put(b []byte) // nolint
	// Run will use a goroutine pool entry to run a function. This blocks until a pool
	// goroutine becomes available.
	Run(func())
	// Close shuts down all internal goroutines. This must be called when the TransferManager
	// will no longer be used. Not closing it will cause a goroutine leak.
	Close()
}

type staticBuffer struct {
	buffers    chan []byte
	size       int
	threadpool chan func()
}

// NewStaticBuffer creates a TransferManager that will use a channel as a circular buffer
// that can hold "max" buffers of "size". The goroutine pool is also sized at max. This
// can be shared between calls if you wish to control maximum memory and concurrency with
// multiple concurrent calls.
func NewStaticBuffer(size, max int) (TransferManager, error) {
	if size < 1 || max < 1 {
		return nil, fmt.Errorf("cannot be called with size or max set to < 1")
	}

	if size < _1MiB {
		return nil, fmt.Errorf("cannot have size < 1MiB")
	}

	threadpool := make(chan func(), max)
	buffers := make(chan []byte, max)
	for i := 0; i < max; i++ {
		go func() {
			for f := range threadpool {
				f()
			}
		}()

		buffers <- make([]byte, size)
	}
	return staticBuffer{
		buffers:    buffers,
		size:       size,
		threadpool: threadpool,
	}, nil
}

// Get implements TransferManager.Get().
func (s staticBuffer) Get() []byte {
	return <-s.buffers
}

// Put implements TransferManager.Put().
func (s staticBuffer) Put(b []byte) { // nolint
	select {
	case s.buffers <- b:
	default: // This shouldn't happen, but just in case they call Put() with there own buffer.
	}
}

// Run implements TransferManager.Run().
func (s staticBuffer) Run(f func()) {
	s.threadpool <- f
}

// Close implements TransferManager.Close().
func (s staticBuffer) Close() {
	close(s.threadpool)
	close(s.buffers)
}

type syncPool struct {
	threadpool chan func()
	pool       sync.Pool
}

// NewSyncPool creates a TransferManager that will use a sync.Pool
// that can hold a non-capped number of buffers constrained by concurrency. This
// can be shared between calls if you wish to share memory and concurrency.
func NewSyncPool(size, concurrency int) (TransferManager, error) {
	if size < 1 || concurrency < 1 {
		return nil, fmt.Errorf("cannot be called with size or max set to < 1")
	}

	if size < _1MiB {
		return nil, fmt.Errorf("cannot have size < 1MiB")
	}

	threadpool := make(chan func(), concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			for f := range threadpool {
				f()
			}
		}()
	}

	return &syncPool{
		threadpool: threadpool,
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		},
	}, nil
}

// Get implements TransferManager.Get().
func (s *syncPool) Get() []byte {
	return s.pool.Get().([]byte)
}

// Put implements TransferManager.Put().
// nolint
func (s *syncPool) Put(b []byte) {
	s.pool.Put(b)
}

// Run implements TransferManager.Run().
func (s *syncPool) Run(f func()) {
	s.threadpool <- f
}

// Close implements TransferManager.Close().
func (s *syncPool) Close() {
	close(s.threadpool)
}

const _1MiB = 1024 * 1024

type UploadStreamToBlockBlobOptions struct {
	// TransferManager provides a TransferManager that controls buffer allocation/reuse and
	// concurrency. This overrides BufferSize and MaxBuffers if set.
	TransferManager      TransferManager
	transferMangerNotSet bool
	// BufferSize sizes the buffer used to read data from source. If < 1 MiB, defaults to 1 MiB.
	BufferSize int
	// MaxBuffers defines the number of simultaneous uploads will be performed to upload the file.
	MaxBuffers           int
	HTTPHeaders          *BlobHTTPHeaders
	Metadata             map[string]string
	BlobAccessConditions *BlobAccessConditions
	AccessTier           *AccessTier
	BlobTagsMap          map[string]string
	CpkInfo              *CpkInfo
	CpkScopeInfo         *CpkScopeInfo
}

func (u *UploadStreamToBlockBlobOptions) defaults() error {
	if u.TransferManager != nil {
		return nil
	}

	if u.MaxBuffers == 0 {
		u.MaxBuffers = 1
	}

	if u.BufferSize < _1MiB {
		u.BufferSize = _1MiB
	}

	var err error
	u.TransferManager, err = NewStaticBuffer(u.BufferSize, u.MaxBuffers)
	if err != nil {
		return fmt.Errorf("bug: default transfer manager could not be created: %s", err)
	}
	u.transferMangerNotSet = true
	return nil
}
func (u *UploadStreamToBlockBlobOptions) getStageBlockOptions() *StageBlockOptions {
	return &StageBlockOptions{}
}

func (u *UploadStreamToBlockBlobOptions) getCommitBlockListOptions() *CommitBlockListOptions {
	return &CommitBlockListOptions{}
}

// UploadStreamToBlockBlob copies the file held in io.Reader to the Blob at blockBlobClient.
// A Context deadline or cancellation will cause this to error.
func (bb BlockBlobClient) UploadStreamToBlockBlob(ctx context.Context, body io.Reader, o UploadStreamToBlockBlobOptions) (BlockBlobCommitBlockListResponse, error) {
	if err := o.defaults(); err != nil {
		return BlockBlobCommitBlockListResponse{}, err
	}

	// If we used the default manager, we need to close it.
	if o.transferMangerNotSet {
		defer o.TransferManager.Close()
	}

	result, err := copyFromReader(ctx, body, bb, o)
	if err != nil {
		return BlockBlobCommitBlockListResponse{}, err
	}

	return result, nil
}
