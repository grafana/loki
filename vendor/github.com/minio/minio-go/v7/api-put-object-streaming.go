/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2017 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// putObjectMultipartStream - upload a large object using
// multipart upload and streaming signature for signing payload.
// Comprehensive put object operation involving multipart uploads.
//
// Following code handles these types of readers.
//
//   - *minio.Object
//   - Any reader which has a method 'ReadAt()'
func (c *Client) putObjectMultipartStream(ctx context.Context, bucketName, objectName string,
	reader io.Reader, size int64, opts PutObjectOptions,
) (info UploadInfo, err error) {
	if opts.ConcurrentStreamParts && opts.NumThreads > 1 {
		info, err = c.putObjectMultipartStreamParallel(ctx, bucketName, objectName, reader, opts)
	} else if !isObject(reader) && isReadAt(reader) && !opts.SendContentMd5 {
		// Verify if the reader implements ReadAt and it is not a *minio.Object then we will use parallel uploader.
		info, err = c.putObjectMultipartStreamFromReadAt(ctx, bucketName, objectName, reader.(io.ReaderAt), size, opts)
	} else {
		info, err = c.putObjectMultipartStreamOptionalChecksum(ctx, bucketName, objectName, reader, size, opts)
	}
	if err != nil && s3utils.IsGoogleEndpoint(*c.endpointURL) {
		errResp := ToErrorResponse(err)
		// Verify if multipart functionality is not available, if not
		// fall back to single PutObject operation.
		if errResp.Code == "AccessDenied" && strings.Contains(errResp.Message, "Access Denied") {
			// Verify if size of reader is greater than '5GiB'.
			if size > maxSinglePutObjectSize {
				return UploadInfo{}, errEntityTooLarge(size, maxSinglePutObjectSize, bucketName, objectName)
			}
			// Fall back to uploading as single PutObject operation.
			return c.putObject(ctx, bucketName, objectName, reader, size, opts)
		}
	}
	return info, err
}

// uploadedPartRes - the response received from a part upload.
type uploadedPartRes struct {
	Error   error // Any error encountered while uploading the part.
	PartNum int   // Number of the part uploaded.
	Size    int64 // Size of the part uploaded.
	Part    ObjectPart
}

type uploadPartReq struct {
	PartNum int        // Number of the part uploaded.
	Part    ObjectPart // Size of the part uploaded.
}

// putObjectMultipartFromReadAt - Uploads files bigger than 128MiB.
// Supports all readers which implements io.ReaderAt interface
// (ReadAt method).
//
// NOTE: This function is meant to be used for all readers which
// implement io.ReaderAt which allows us for resuming multipart
// uploads but reading at an offset, which would avoid re-read the
// data which was already uploaded. Internally this function uses
// temporary files for staging all the data, these temporary files are
// cleaned automatically when the caller i.e http client closes the
// stream after uploading all the contents successfully.
func (c *Client) putObjectMultipartStreamFromReadAt(ctx context.Context, bucketName, objectName string,
	reader io.ReaderAt, size int64, opts PutObjectOptions,
) (info UploadInfo, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err = s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}

	// Calculate the optimal parts info for a given size.
	totalPartsCount, partSize, lastPartSize, err := OptimalPartInfo(size, opts.PartSize)
	if err != nil {
		return UploadInfo{}, err
	}
	if opts.Checksum.IsSet() {
		opts.AutoChecksum = opts.Checksum
	}
	withChecksum := c.trailingHeaderSupport
	if withChecksum {
		addAutoChecksumHeaders(&opts)
	}
	// Initiate a new multipart upload.
	uploadID, err := c.newUploadID(ctx, bucketName, objectName, opts)
	if err != nil {
		return UploadInfo{}, err
	}
	delete(opts.UserMetadata, "X-Amz-Checksum-Algorithm")

	// Aborts the multipart upload in progress, if the
	// function returns any error, since we do not resume
	// we should purge the parts which have been uploaded
	// to relinquish storage space.
	defer func() {
		if err != nil {
			c.abortMultipartUpload(ctx, bucketName, objectName, uploadID)
		}
	}()

	// Total data read and written to server. should be equal to 'size' at the end of the call.
	var totalUploadedSize int64

	// Complete multipart upload.
	var complMultipartUpload completeMultipartUpload

	// Declare a channel that sends the next part number to be uploaded.
	uploadPartsCh := make(chan uploadPartReq)

	// Declare a channel that sends back the response of a part upload.
	uploadedPartsCh := make(chan uploadedPartRes)

	// Used for readability, lastPartNumber is always totalPartsCount.
	lastPartNumber := totalPartsCount

	partitionCtx, partitionCancel := context.WithCancel(ctx)
	defer partitionCancel()
	// Send each part number to the channel to be processed.
	go func() {
		defer close(uploadPartsCh)

		for p := 1; p <= totalPartsCount; p++ {
			select {
			case <-partitionCtx.Done():
				return
			case uploadPartsCh <- uploadPartReq{PartNum: p}:
			}
		}
	}()

	// Receive each part number from the channel allowing three parallel uploads.
	for w := 1; w <= opts.getNumThreads(); w++ {
		go func(partSize int64) {
			for {
				var uploadReq uploadPartReq
				var ok bool
				select {
				case <-ctx.Done():
					return
				case uploadReq, ok = <-uploadPartsCh:
					if !ok {
						return
					}
					// Each worker will draw from the part channel and upload in parallel.
				}

				// If partNumber was not uploaded we calculate the missing
				// part offset and size. For all other part numbers we
				// calculate offset based on multiples of partSize.
				readOffset := int64(uploadReq.PartNum-1) * partSize

				// As a special case if partNumber is lastPartNumber, we
				// calculate the offset based on the last part size.
				if uploadReq.PartNum == lastPartNumber {
					readOffset = size - lastPartSize
					partSize = lastPartSize
				}

				sectionReader := newHook(io.NewSectionReader(reader, readOffset, partSize), opts.Progress)
				trailer := make(http.Header, 1)
				if withChecksum {
					crc := opts.AutoChecksum.Hasher()
					trailer.Set(opts.AutoChecksum.Key(), base64.StdEncoding.EncodeToString(crc.Sum(nil)))
					sectionReader = newHashReaderWrapper(sectionReader, crc, func(hash []byte) {
						trailer.Set(opts.AutoChecksum.Key(), base64.StdEncoding.EncodeToString(hash))
					})
				}

				// Proceed to upload the part.
				p := uploadPartParams{
					bucketName:   bucketName,
					objectName:   objectName,
					uploadID:     uploadID,
					reader:       sectionReader,
					partNumber:   uploadReq.PartNum,
					size:         partSize,
					sse:          opts.ServerSideEncryption,
					streamSha256: !opts.DisableContentSha256,
					sha256Hex:    "",
					trailer:      trailer,
				}
				objPart, err := c.uploadPart(ctx, p)
				if err != nil {
					uploadedPartsCh <- uploadedPartRes{
						Error: err,
					}
					// Exit the goroutine.
					return
				}

				// Save successfully uploaded part metadata.
				uploadReq.Part = objPart

				// Send successful part info through the channel.
				uploadedPartsCh <- uploadedPartRes{
					Size:    objPart.Size,
					PartNum: uploadReq.PartNum,
					Part:    uploadReq.Part,
				}
			}
		}(partSize)
	}

	// Gather the responses as they occur and update any
	// progress bar.
	allParts := make([]ObjectPart, 0, totalPartsCount)
	for u := 1; u <= totalPartsCount; u++ {
		select {
		case <-ctx.Done():
			return UploadInfo{}, ctx.Err()
		case uploadRes := <-uploadedPartsCh:
			if uploadRes.Error != nil {
				return UploadInfo{}, uploadRes.Error
			}
			allParts = append(allParts, uploadRes.Part)
			// Update the totalUploadedSize.
			totalUploadedSize += uploadRes.Size
			complMultipartUpload.Parts = append(complMultipartUpload.Parts, CompletePart{
				ETag:              uploadRes.Part.ETag,
				PartNumber:        uploadRes.Part.PartNumber,
				ChecksumCRC32:     uploadRes.Part.ChecksumCRC32,
				ChecksumCRC32C:    uploadRes.Part.ChecksumCRC32C,
				ChecksumSHA1:      uploadRes.Part.ChecksumSHA1,
				ChecksumSHA256:    uploadRes.Part.ChecksumSHA256,
				ChecksumCRC64NVME: uploadRes.Part.ChecksumCRC64NVME,
			})
		}
	}

	// Verify if we uploaded all the data.
	if totalUploadedSize != size {
		return UploadInfo{}, errUnexpectedEOF(totalUploadedSize, size, bucketName, objectName)
	}

	// Sort all completed parts.
	sort.Sort(completedParts(complMultipartUpload.Parts))

	opts = PutObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
		AutoChecksum:         opts.AutoChecksum,
	}
	if withChecksum {
		applyAutoChecksum(&opts, allParts)
	}

	uploadInfo, err := c.completeMultipartUpload(ctx, bucketName, objectName, uploadID, complMultipartUpload, opts)
	if err != nil {
		return UploadInfo{}, err
	}

	uploadInfo.Size = totalUploadedSize
	return uploadInfo, nil
}

func (c *Client) putObjectMultipartStreamOptionalChecksum(ctx context.Context, bucketName, objectName string,
	reader io.Reader, size int64, opts PutObjectOptions,
) (info UploadInfo, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err = s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}

	if opts.Checksum.IsSet() {
		opts.AutoChecksum = opts.Checksum
		opts.SendContentMd5 = false
	}

	if !opts.SendContentMd5 {
		addAutoChecksumHeaders(&opts)
	}

	// Calculate the optimal parts info for a given size.
	totalPartsCount, partSize, lastPartSize, err := OptimalPartInfo(size, opts.PartSize)
	if err != nil {
		return UploadInfo{}, err
	}
	// Initiates a new multipart request
	uploadID, err := c.newUploadID(ctx, bucketName, objectName, opts)
	if err != nil {
		return UploadInfo{}, err
	}
	delete(opts.UserMetadata, "X-Amz-Checksum-Algorithm")

	// Aborts the multipart upload if the function returns
	// any error, since we do not resume we should purge
	// the parts which have been uploaded to relinquish
	// storage space.
	defer func() {
		if err != nil {
			c.abortMultipartUpload(ctx, bucketName, objectName, uploadID)
		}
	}()

	// Create checksums
	// CRC32C is ~50% faster on AMD64 @ 30GB/s
	customHeader := make(http.Header)
	crc := opts.AutoChecksum.Hasher()
	md5Hash := c.md5Hasher()
	defer md5Hash.Close()

	// Total data read and written to server. should be equal to 'size' at the end of the call.
	var totalUploadedSize int64

	// Initialize parts uploaded map.
	partsInfo := make(map[int]ObjectPart)

	// Create a buffer.
	buf := make([]byte, partSize)

	// Avoid declaring variables in the for loop
	var md5Base64 string

	// Part number always starts with '1'.
	var partNumber int
	for partNumber = 1; partNumber <= totalPartsCount; partNumber++ {
		// Proceed to upload the part.
		if partNumber == totalPartsCount {
			partSize = lastPartSize
		}

		length, rerr := readFull(reader, buf)
		if rerr == io.EOF && partNumber > 1 {
			break
		}

		if rerr != nil && rerr != io.ErrUnexpectedEOF && err != io.EOF {
			return UploadInfo{}, rerr
		}

		// Calculate md5sum.
		if opts.SendContentMd5 {
			md5Hash.Reset()
			md5Hash.Write(buf[:length])
			md5Base64 = base64.StdEncoding.EncodeToString(md5Hash.Sum(nil))
		} else {
			// Add CRC32C instead.
			crc.Reset()
			crc.Write(buf[:length])
			cSum := crc.Sum(nil)
			customHeader.Set(opts.AutoChecksum.KeyCapitalized(), base64.StdEncoding.EncodeToString(cSum))
		}

		// Update progress reader appropriately to the latest offset
		// as we read from the source.
		hooked := newHook(bytes.NewReader(buf[:length]), opts.Progress)
		p := uploadPartParams{bucketName: bucketName, objectName: objectName, uploadID: uploadID, reader: hooked, partNumber: partNumber, md5Base64: md5Base64, size: partSize, sse: opts.ServerSideEncryption, streamSha256: !opts.DisableContentSha256, customHeader: customHeader}
		objPart, uerr := c.uploadPart(ctx, p)
		if uerr != nil {
			return UploadInfo{}, uerr
		}

		// Save successfully uploaded part metadata.
		partsInfo[partNumber] = objPart

		// Save successfully uploaded size.
		totalUploadedSize += partSize
	}

	// Verify if we uploaded all the data.
	if size > 0 {
		if totalUploadedSize != size {
			return UploadInfo{}, errUnexpectedEOF(totalUploadedSize, size, bucketName, objectName)
		}
	}

	// Complete multipart upload.
	var complMultipartUpload completeMultipartUpload

	// Loop over total uploaded parts to save them in
	// Parts array before completing the multipart request.
	allParts := make([]ObjectPart, 0, len(partsInfo))
	for i := 1; i < partNumber; i++ {
		part, ok := partsInfo[i]
		if !ok {
			return UploadInfo{}, errInvalidArgument(fmt.Sprintf("Missing part number %d", i))
		}
		allParts = append(allParts, part)
		complMultipartUpload.Parts = append(complMultipartUpload.Parts, CompletePart{
			ETag:              part.ETag,
			PartNumber:        part.PartNumber,
			ChecksumCRC32:     part.ChecksumCRC32,
			ChecksumCRC32C:    part.ChecksumCRC32C,
			ChecksumSHA1:      part.ChecksumSHA1,
			ChecksumSHA256:    part.ChecksumSHA256,
			ChecksumCRC64NVME: part.ChecksumCRC64NVME,
		})
	}

	// Sort all completed parts.
	sort.Sort(completedParts(complMultipartUpload.Parts))

	opts = PutObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
		AutoChecksum:         opts.AutoChecksum,
	}
	applyAutoChecksum(&opts, allParts)
	uploadInfo, err := c.completeMultipartUpload(ctx, bucketName, objectName, uploadID, complMultipartUpload, opts)
	if err != nil {
		return UploadInfo{}, err
	}

	uploadInfo.Size = totalUploadedSize
	return uploadInfo, nil
}

// putObjectMultipartStreamParallel uploads opts.NumThreads parts in parallel.
// This is expected to take opts.PartSize * opts.NumThreads * (GOGC / 100) bytes of buffer.
func (c *Client) putObjectMultipartStreamParallel(ctx context.Context, bucketName, objectName string,
	reader io.Reader, opts PutObjectOptions,
) (info UploadInfo, err error) {
	// Input validation.
	if err = s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}

	if err = s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}
	if opts.Checksum.IsSet() {
		opts.SendContentMd5 = false
		opts.AutoChecksum = opts.Checksum
	}
	if !opts.SendContentMd5 {
		addAutoChecksumHeaders(&opts)
	}

	// Cancel all when an error occurs.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Calculate the optimal parts info for a given size.
	totalPartsCount, partSize, _, err := OptimalPartInfo(-1, opts.PartSize)
	if err != nil {
		return UploadInfo{}, err
	}

	// Initiates a new multipart request
	uploadID, err := c.newUploadID(ctx, bucketName, objectName, opts)
	if err != nil {
		return UploadInfo{}, err
	}
	delete(opts.UserMetadata, "X-Amz-Checksum-Algorithm")

	// Aborts the multipart upload if the function returns
	// any error, since we do not resume we should purge
	// the parts which have been uploaded to relinquish
	// storage space.
	defer func() {
		if err != nil {
			c.abortMultipartUpload(ctx, bucketName, objectName, uploadID)
		}
	}()

	// Create checksums
	// CRC32C is ~50% faster on AMD64 @ 30GB/s
	crc := opts.AutoChecksum.Hasher()

	// Total data read and written to server. should be equal to 'size' at the end of the call.
	var totalUploadedSize int64

	// Initialize parts uploaded map.
	partsInfo := make(map[int]ObjectPart)

	// Create a buffer.
	nBuffers := int64(opts.NumThreads)
	bufs := make(chan []byte, nBuffers)
	all := make([]byte, nBuffers*partSize)
	for i := int64(0); i < nBuffers; i++ {
		bufs <- all[i*partSize : i*partSize+partSize]
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	errCh := make(chan error, opts.NumThreads)

	reader = newHook(reader, opts.Progress)

	// Part number always starts with '1'.
	var partNumber int
	for partNumber = 1; partNumber <= totalPartsCount; partNumber++ {
		// Proceed to upload the part.
		var buf []byte
		select {
		case buf = <-bufs:
		case err = <-errCh:
			cancel()
			wg.Wait()
			return UploadInfo{}, err
		}

		if int64(len(buf)) != partSize {
			return UploadInfo{}, fmt.Errorf("read buffer < %d than expected partSize: %d", len(buf), partSize)
		}

		length, rerr := readFull(reader, buf)
		if rerr == io.EOF && partNumber > 1 {
			// Done
			break
		}

		if rerr != nil && rerr != io.ErrUnexpectedEOF && err != io.EOF {
			cancel()
			wg.Wait()
			return UploadInfo{}, rerr
		}

		// Calculate md5sum.
		customHeader := make(http.Header)
		if !opts.SendContentMd5 {
			// Add Checksum instead.
			crc.Reset()
			crc.Write(buf[:length])
			cSum := crc.Sum(nil)
			customHeader.Set(opts.AutoChecksum.Key(), base64.StdEncoding.EncodeToString(cSum))
		}

		wg.Add(1)
		go func(partNumber int) {
			// Avoid declaring variables in the for loop
			var md5Base64 string

			if opts.SendContentMd5 {
				md5Hash := c.md5Hasher()
				md5Hash.Write(buf[:length])
				md5Base64 = base64.StdEncoding.EncodeToString(md5Hash.Sum(nil))
				md5Hash.Close()
			}

			defer wg.Done()
			p := uploadPartParams{
				bucketName:   bucketName,
				objectName:   objectName,
				uploadID:     uploadID,
				reader:       bytes.NewReader(buf[:length]),
				partNumber:   partNumber,
				md5Base64:    md5Base64,
				size:         int64(length),
				sse:          opts.ServerSideEncryption,
				streamSha256: !opts.DisableContentSha256,
				customHeader: customHeader,
			}
			objPart, uerr := c.uploadPart(ctx, p)
			if uerr != nil {
				errCh <- uerr
				return
			}

			// Save successfully uploaded part metadata.
			mu.Lock()
			partsInfo[partNumber] = objPart
			mu.Unlock()

			// Send buffer back so it can be reused.
			bufs <- buf
		}(partNumber)

		// Save successfully uploaded size.
		totalUploadedSize += int64(length)
	}
	wg.Wait()

	// Collect any error
	select {
	case err = <-errCh:
		return UploadInfo{}, err
	default:
	}

	// Complete multipart upload.
	var complMultipartUpload completeMultipartUpload

	// Loop over total uploaded parts to save them in
	// Parts array before completing the multipart request.
	allParts := make([]ObjectPart, 0, len(partsInfo))
	for i := 1; i < partNumber; i++ {
		part, ok := partsInfo[i]
		if !ok {
			return UploadInfo{}, errInvalidArgument(fmt.Sprintf("Missing part number %d", i))
		}
		allParts = append(allParts, part)
		complMultipartUpload.Parts = append(complMultipartUpload.Parts, CompletePart{
			ETag:              part.ETag,
			PartNumber:        part.PartNumber,
			ChecksumCRC32:     part.ChecksumCRC32,
			ChecksumCRC32C:    part.ChecksumCRC32C,
			ChecksumSHA1:      part.ChecksumSHA1,
			ChecksumSHA256:    part.ChecksumSHA256,
			ChecksumCRC64NVME: part.ChecksumCRC64NVME,
		})
	}

	// Sort all completed parts.
	sort.Sort(completedParts(complMultipartUpload.Parts))

	opts = PutObjectOptions{
		ServerSideEncryption: opts.ServerSideEncryption,
		AutoChecksum:         opts.AutoChecksum,
	}
	applyAutoChecksum(&opts, allParts)

	uploadInfo, err := c.completeMultipartUpload(ctx, bucketName, objectName, uploadID, complMultipartUpload, opts)
	if err != nil {
		return UploadInfo{}, err
	}

	uploadInfo.Size = totalUploadedSize
	return uploadInfo, nil
}

// putObject special function used Google Cloud Storage. This special function
// is used for Google Cloud Storage since Google's multipart API is not S3 compatible.
func (c *Client) putObject(ctx context.Context, bucketName, objectName string, reader io.Reader, size int64, opts PutObjectOptions) (info UploadInfo, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}

	// Size -1 is only supported on Google Cloud Storage, we error
	// out in all other situations.
	if size < 0 && !s3utils.IsGoogleEndpoint(*c.endpointURL) {
		return UploadInfo{}, errEntityTooSmall(size, bucketName, objectName)
	}

	if opts.SendContentMd5 && s3utils.IsGoogleEndpoint(*c.endpointURL) && size < 0 {
		return UploadInfo{}, errInvalidArgument("MD5Sum cannot be calculated with size '-1'")
	}
	if opts.Checksum.IsSet() {
		opts.SendContentMd5 = false
	}

	var readSeeker io.Seeker
	if size > 0 {
		if isReadAt(reader) && !isObject(reader) {
			seeker, ok := reader.(io.Seeker)
			if ok {
				offset, err := seeker.Seek(0, io.SeekCurrent)
				if err != nil {
					return UploadInfo{}, errInvalidArgument(err.Error())
				}
				reader = io.NewSectionReader(reader.(io.ReaderAt), offset, size)
				readSeeker = reader.(io.Seeker)
			}
		}
	}

	var md5Base64 string
	if opts.SendContentMd5 {
		// Calculate md5sum.
		hash := c.md5Hasher()

		if readSeeker != nil {
			if _, err := io.Copy(hash, reader); err != nil {
				return UploadInfo{}, err
			}
			// Seek back to beginning of io.NewSectionReader's offset.
			_, err = readSeeker.Seek(0, io.SeekStart)
			if err != nil {
				return UploadInfo{}, errInvalidArgument(err.Error())
			}
		} else {
			// Create a buffer.
			buf := make([]byte, size)

			length, err := readFull(reader, buf)
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				return UploadInfo{}, err
			}

			hash.Write(buf[:length])
			reader = bytes.NewReader(buf[:length])
		}

		md5Base64 = base64.StdEncoding.EncodeToString(hash.Sum(nil))
		hash.Close()
	}

	// Update progress reader appropriately to the latest offset as we
	// read from the source.
	progressReader := newHook(reader, opts.Progress)

	// This function does not calculate sha256 and md5sum for payload.
	// Execute put object.
	return c.putObjectDo(ctx, bucketName, objectName, progressReader, md5Base64, "", size, opts)
}

// putObjectDo - executes the put object http operation.
// NOTE: You must have WRITE permissions on a bucket to add an object to it.
func (c *Client) putObjectDo(ctx context.Context, bucketName, objectName string, reader io.Reader, md5Base64, sha256Hex string, size int64, opts PutObjectOptions) (UploadInfo, error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return UploadInfo{}, err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return UploadInfo{}, err
	}
	// Set headers.
	customHeader := opts.Header()

	// Populate request metadata.
	reqMetadata := requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		customHeader:     customHeader,
		contentBody:      reader,
		contentLength:    size,
		contentMD5Base64: md5Base64,
		contentSHA256Hex: sha256Hex,
		streamSha256:     !opts.DisableContentSha256,
	}
	// Add CRC when client supports it, MD5 is not set, not Google and we don't add SHA256 to chunks.
	addCrc := c.trailingHeaderSupport && md5Base64 == "" && !s3utils.IsGoogleEndpoint(*c.endpointURL) && (opts.DisableContentSha256 || c.secure)
	if opts.Checksum.IsSet() {
		reqMetadata.addCrc = &opts.Checksum
	} else if addCrc {
		// If user has added checksums, don't add them ourselves.
		for k := range opts.UserMetadata {
			if strings.HasPrefix(strings.ToLower(k), "x-amz-checksum-") {
				addCrc = false
			}
		}
		if addCrc {
			opts.AutoChecksum.SetDefault(ChecksumCRC32C)
			reqMetadata.addCrc = &opts.AutoChecksum
		}
	}

	if opts.Internal.SourceVersionID != "" {
		if opts.Internal.SourceVersionID != nullVersionID {
			if _, err := uuid.Parse(opts.Internal.SourceVersionID); err != nil {
				return UploadInfo{}, errInvalidArgument(err.Error())
			}
		}
		urlValues := make(url.Values)
		urlValues.Set("versionId", opts.Internal.SourceVersionID)
		reqMetadata.queryValues = urlValues
	}

	// Execute PUT an objectName.
	resp, err := c.executeMethod(ctx, http.MethodPut, reqMetadata)
	defer closeResponse(resp)
	if err != nil {
		return UploadInfo{}, err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusOK {
			return UploadInfo{}, httpRespToErrorResponse(resp, bucketName, objectName)
		}
	}

	// extract lifecycle expiry date and rule ID
	expTime, ruleID := amzExpirationToExpiryDateRuleID(resp.Header.Get(amzExpiration))
	h := resp.Header
	return UploadInfo{
		Bucket:           bucketName,
		Key:              objectName,
		ETag:             trimEtag(h.Get("ETag")),
		VersionID:        h.Get(amzVersionID),
		Size:             size,
		Expiration:       expTime,
		ExpirationRuleID: ruleID,

		// Checksum values
		ChecksumCRC32:     h.Get(ChecksumCRC32.Key()),
		ChecksumCRC32C:    h.Get(ChecksumCRC32C.Key()),
		ChecksumSHA1:      h.Get(ChecksumSHA1.Key()),
		ChecksumSHA256:    h.Get(ChecksumSHA256.Key()),
		ChecksumCRC64NVME: h.Get(ChecksumCRC64NVME.Key()),
	}, nil
}
