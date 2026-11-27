/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2017 MinIO, Inc.
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
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/dustin/go-humanize"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

const nullVersionID = "null"

// Verify if reader is *minio.Object
func isObject(reader io.Reader) (ok bool) {
	_, ok = reader.(*Object)
	return ok
}

// Verify if reader is a generic ReaderAt
func isReadAt(reader io.Reader) (ok bool) {
	var v *os.File
	v, ok = reader.(*os.File)
	if ok {
		// Stdin, Stdout and Stderr all have *os.File type
		// which happen to also be io.ReaderAt compatible
		// we need to add special conditions for them to
		// be ignored by this function.
		for _, f := range []string{
			"/dev/stdin",
			"/dev/stdout",
			"/dev/stderr",
		} {
			if f == v.Name() {
				ok = false
				break
			}
		}
	} else {
		_, ok = reader.(io.ReaderAt)
	}
	return ok
}

// OptimalPartInfo - calculate the optimal part info for a given
// object size, using Amazon S3's upload limits.
//
// NOTE: Assumption here is that for any object to be uploaded to any S3 compatible
// object storage it will have the following parameters as constants.
//
//	maxPartsCount - 10000
//	minPartSize - 16MiB
//	maxObjectSize - ~48.83TiB (maxPartSize * maxPartsCount)
//
// A Client created with Options.UploadLimits uses its own limits instead of
// these, so its part layout may differ from what this function returns.
func OptimalPartInfo(objectSize int64, configuredPartSize uint64) (totalPartsCount int, partSize, lastPartSize int64, err error) {
	return UploadLimits{}.optimalPartInfo(objectSize, configuredPartSize)
}

func (c *Client) optimalPartInfo(objectSize int64, configuredPartSize uint64) (totalPartsCount int, partSize, lastPartSize int64, err error) {
	return c.limits.optimalPartInfo(objectSize, configuredPartSize)
}

// optimalPartInfo - calculate the optimal part info for a given object size
// within these limits.
func (l UploadLimits) optimalPartInfo(objectSize int64, configuredPartSize uint64) (totalPartsCount int, partSize, lastPartSize int64, err error) {
	maxPartsCount := l.maxPartsCount()
	maxObjectSize := l.maxObjectSize()

	// When object size is unknown (-1), default to 5TiB (or the maximum object
	// size, when lower) to limit memory usage. This results in ~537MiB part
	// sizes. For larger objects (up to the maximum object size), callers should
	// set configuredPartSize explicitly to control memory usage.
	var unknownSize bool
	if objectSize == -1 {
		unknownSize = true
		objectSize = min(maxMultipartPutObjectSize, maxObjectSize)
	}

	// object size is larger than supported maximum.
	if objectSize > maxObjectSize {
		err = errEntityTooLarge(objectSize, maxObjectSize, "", "")
		return totalPartsCount, partSize, lastPartSize, err
	}

	// An empty object has no parts; the minimum part size below would otherwise
	// report a part size and a last part size for a layout with no parts in it.
	if objectSize == 0 && configuredPartSize == 0 {
		return 0, 0, 0, nil
	}

	var partSizeFlt float64
	if configuredPartSize > 0 {
		// Compared unsigned and up front, so the int64 conversions below cannot
		// wrap a caller-supplied part size into a negative that slips past them.
		if configuredPartSize > uint64(l.maxPartSize()) {
			err = errInvalidArgument(fmt.Sprintf("Input part size is bigger than allowed maximum of %s.", humanize.IBytes(uint64(l.maxPartSize()))))
			return totalPartsCount, partSize, lastPartSize, err
		}

		if int64(configuredPartSize) > objectSize {
			err = errEntityTooLarge(int64(configuredPartSize), objectSize, "", "")
			return totalPartsCount, partSize, lastPartSize, err
		}

		if !unknownSize {
			if objectSize > (int64(configuredPartSize) * maxPartsCount) {
				err = errInvalidArgument(fmt.Sprintf("Part size * max_parts(%d) is lesser than input objectSize.", maxPartsCount))
				return totalPartsCount, partSize, lastPartSize, err
			}
		}

		if int64(configuredPartSize) < l.minPartSize() {
			err = errInvalidArgument(fmt.Sprintf("Input part size is smaller than allowed minimum of %s.", humanize.IBytes(uint64(l.minPartSize()))))
			return totalPartsCount, partSize, lastPartSize, err
		}

		partSizeFlt = float64(configuredPartSize)
		if unknownSize {
			// If input has unknown size and part size is configured
			// keep it to maximum allowed as per the max parts count.
			objectSize = int64(configuredPartSize) * maxPartsCount
		}
	} else {
		// Round to a multiple of the internal threshold, but never below a
		// MinPartSize that was raised above it, or the generated non-final
		// parts would be rejected by the remote.
		configuredPartSize = uint64(max(minPartSize, l.minPartSize()))
		// Round the exact ceiling of objectSize/maxPartsCount, not the truncated
		// quotient: a truncated quotient that already sits on a
		// configuredPartSize multiple stays put and needs maxPartsCount+1 parts.
		smallestPartSize := objectSize / maxPartsCount
		if objectSize%maxPartsCount != 0 {
			smallestPartSize++
		}
		// Use floats for part size for all calculations to avoid
		// overflows during float64 to int64 conversions.
		partSizeFlt = math.Ceil(float64(smallestPartSize)/float64(configuredPartSize)) * float64(configuredPartSize)
		// An object smaller than maxPartsCount rounds down to a zero part size,
		// and a non-final part must never fall below MinPartSize either.
		if minPS := float64(l.minPartSize()); partSizeFlt < minPS {
			partSizeFlt = minPS
		}
		// Rounding up to a minPartSize multiple can overshoot a MaxPartSize
		// that was lowered below, or is not a multiple of, minPartSize.
		if maxPS := float64(l.maxPartSize()); partSizeFlt > maxPS {
			partSizeFlt = maxPS
		}
	}

	// Total parts count.
	totalPartsCount = int(math.Ceil(float64(objectSize) / partSizeFlt))
	// Part size.
	partSize = int64(partSizeFlt)
	// Last part size.
	lastPartSize = objectSize - int64(totalPartsCount-1)*partSize
	return totalPartsCount, partSize, lastPartSize, nil
}

// errIfMoreData reports errUploadTooLarge when reader still holds data after
// the last part allowed by the upload limits was consumed. Unknown length
// uploads would otherwise silently complete a truncated object.
//
// This deliberately fails an upload whose bytes were all transferred: the probe
// runs after the final part, so a reader that reports anything other than a
// clean io.EOF — a closed file, a reset connection, a wrapper with its own
// sentinel — aborts the multipart upload instead of completing it. Silently
// storing a possibly truncated object is the worse outcome, but callers should
// expect this error to arrive late and to look like a transport fault.
func errIfMoreData(reader io.Reader, uploadedSize, totalPartsCount int64, bucketName, objectName string) error {
	var b [1]byte
	n, err := readFull(reader, b[:])
	if n > 0 {
		return errUploadTooLarge(uploadedSize, totalPartsCount, bucketName, objectName)
	}
	// Only a clean EOF proves the reader was drained; anything else has to
	// surface rather than complete a possibly truncated object.
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

// getUploadID - fetch upload id if already present for an object name
// or initiate a new request to fetch a new upload id.
func (c *Client) newUploadID(ctx context.Context, bucketName, objectName string, opts PutObjectOptions) (uploadID string, err error) {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return "", err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return "", err
	}

	// Initiate multipart upload for an object.
	initMultipartUploadResult, err := c.initiateMultipartUpload(ctx, bucketName, objectName, opts)
	if err != nil {
		return "", err
	}
	return initMultipartUploadResult.UploadID, nil
}
