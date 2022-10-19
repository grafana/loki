//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"context"
	"encoding/base64"
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

// uploadReaderAtToBlockBlob uploads a buffer in blocks to a block blob.
func (bb *BlockBlobClient) uploadReaderAtToBlockBlob(ctx context.Context, reader io.ReaderAt, readerSize int64, o UploadOption) (*http.Response, error) {
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

// UploadBuffer uploads a buffer in blocks to a block blob.
func (bb *BlockBlobClient) UploadBuffer(ctx context.Context, b []byte, o UploadOption) (*http.Response, error) {
	return bb.uploadReaderAtToBlockBlob(ctx, bytes.NewReader(b), int64(len(b)), o)
}

// UploadFile uploads a file in blocks to a block blob.
func (bb *BlockBlobClient) UploadFile(ctx context.Context, file *os.File, o UploadOption) (*http.Response, error) {

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return bb.uploadReaderAtToBlockBlob(ctx, file, stat.Size(), o)
}

// ---------------------------------------------------------------------------------------------------------------------

// UploadStream copies the file held in io.Reader to the Blob at blockBlobClient.
// A Context deadline or cancellation will cause this to error.
func (bb *BlockBlobClient) UploadStream(ctx context.Context, body io.Reader, o UploadStreamOptions) (BlockBlobCommitBlockListResponse, error) {
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

// ---------------------------------------------------------------------------------------------------------------------

// DownloadToWriterAt downloads an Azure blob to a WriterAt with parallel.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b *BlobClient) DownloadToWriterAt(ctx context.Context, offset int64, count int64, writer io.WriterAt, o DownloadOptions) error {
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
			body := dr.Body(&o.RetryReaderOptionsPerBlock)
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

// DownloadToBuffer downloads an Azure blob to a buffer with parallel.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b *BlobClient) DownloadToBuffer(ctx context.Context, offset int64, count int64, _bytes []byte, o DownloadOptions) error {
	return b.DownloadToWriterAt(ctx, offset, count, newBytesWriter(_bytes), o)
}

// DownloadToFile downloads an Azure blob to a local file.
// The file would be truncated if the size doesn't match.
// Offset and count are optional, pass 0 for both to download the entire blob.
func (b *BlobClient) DownloadToFile(ctx context.Context, offset int64, count int64, file *os.File, o DownloadOptions) error {
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
		return b.DownloadToWriterAt(ctx, offset, size, file, o)
	} else { // if the blob's size is 0, there is no need in downloading it
		return nil
	}
}

// ---------------------------------------------------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------------------------------------------------
