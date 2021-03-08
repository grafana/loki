/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2020 MinIO, Inc.
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
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// RemoveBucket deletes the bucket name.
//
//  All objects (including all object versions and delete markers).
//  in the bucket must be deleted before successfully attempting this request.
func (c Client) RemoveBucket(ctx context.Context, bucketName string) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	// Execute DELETE on bucket.
	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:       bucketName,
		contentSHA256Hex: emptySHA256Hex,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusNoContent {
			return httpRespToErrorResponse(resp, bucketName, "")
		}
	}

	// Remove the location from cache on a successful delete.
	c.bucketLocCache.Delete(bucketName)

	return nil
}

// AdvancedRemoveOptions intended for internal use by replication
type AdvancedRemoveOptions struct {
	ReplicationDeleteMarker bool
	ReplicationStatus       ReplicationStatus
	ReplicationMTime        time.Time
}

// RemoveObjectOptions represents options specified by user for RemoveObject call
type RemoveObjectOptions struct {
	GovernanceBypass bool
	VersionID        string
	Internal         AdvancedRemoveOptions
}

// RemoveObject removes an object from a bucket.
func (c Client) RemoveObject(ctx context.Context, bucketName, objectName string, opts RemoveObjectOptions) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return err
	}

	return c.removeObject(ctx, bucketName, objectName, opts)
}

func (c Client) removeObject(ctx context.Context, bucketName, objectName string, opts RemoveObjectOptions) error {

	// Get resources properly escaped and lined up before
	// using them in http request.
	urlValues := make(url.Values)

	if opts.VersionID != "" {
		urlValues.Set("versionId", opts.VersionID)
	}

	// Build headers.
	headers := make(http.Header)

	if opts.GovernanceBypass {
		// Set the bypass goverenance retention header
		headers.Set(amzBypassGovernance, "true")
	}
	if opts.Internal.ReplicationDeleteMarker {
		headers.Set(minIOBucketReplicationDeleteMarker, "true")
	}
	if !opts.Internal.ReplicationMTime.IsZero() {
		headers.Set(minIOBucketSourceMTime, opts.Internal.ReplicationMTime.Format(time.RFC3339Nano))
	}
	if !opts.Internal.ReplicationStatus.Empty() {
		headers.Set(amzBucketReplicationStatus, string(opts.Internal.ReplicationStatus))
	}
	// Execute DELETE on objectName.
	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		contentSHA256Hex: emptySHA256Hex,
		queryValues:      urlValues,
		customHeader:     headers,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		// if some unexpected error happened and max retry is reached, we want to let client know
		if resp.StatusCode != http.StatusNoContent {
			return httpRespToErrorResponse(resp, bucketName, objectName)
		}
	}

	// DeleteObject always responds with http '204' even for
	// objects which do not exist. So no need to handle them
	// specifically.
	return nil
}

// RemoveObjectError - container of Multi Delete S3 API error
type RemoveObjectError struct {
	ObjectName string
	VersionID  string
	Err        error
}

// generateRemoveMultiObjects - generate the XML request for remove multi objects request
func generateRemoveMultiObjectsRequest(objects []ObjectInfo) []byte {
	delObjects := []deleteObject{}
	for _, obj := range objects {
		delObjects = append(delObjects, deleteObject{
			Key:       obj.Key,
			VersionID: obj.VersionID,
		})
	}
	xmlBytes, _ := xml.Marshal(deleteMultiObjects{Objects: delObjects, Quiet: true})
	return xmlBytes
}

// processRemoveMultiObjectsResponse - parse the remove multi objects web service
// and return the success/failure result status for each object
func processRemoveMultiObjectsResponse(body io.Reader, objects []ObjectInfo, errorCh chan<- RemoveObjectError) {
	// Parse multi delete XML response
	rmResult := &deleteMultiObjectsResult{}
	err := xmlDecoder(body, rmResult)
	if err != nil {
		errorCh <- RemoveObjectError{ObjectName: "", Err: err}
		return
	}

	// Fill deletion that returned an error.
	for _, obj := range rmResult.UnDeletedObjects {
		// Version does not exist is not an error ignore and continue.
		switch obj.Code {
		case "InvalidArgument", "NoSuchVersion":
			continue
		}
		errorCh <- RemoveObjectError{
			ObjectName: obj.Key,
			VersionID:  obj.VersionID,
			Err: ErrorResponse{
				Code:    obj.Code,
				Message: obj.Message,
			},
		}
	}
}

// RemoveObjectsOptions represents options specified by user for RemoveObjects call
type RemoveObjectsOptions struct {
	GovernanceBypass bool
}

// RemoveObjects removes multiple objects from a bucket while
// it is possible to specify objects versions which are received from
// objectsCh. Remove failures are sent back via error channel.
func (c Client) RemoveObjects(ctx context.Context, bucketName string, objectsCh <-chan ObjectInfo, opts RemoveObjectsOptions) <-chan RemoveObjectError {
	errorCh := make(chan RemoveObjectError, 1)

	// Validate if bucket name is valid.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		defer close(errorCh)
		errorCh <- RemoveObjectError{
			Err: err,
		}
		return errorCh
	}
	// Validate objects channel to be properly allocated.
	if objectsCh == nil {
		defer close(errorCh)
		errorCh <- RemoveObjectError{
			Err: errInvalidArgument("Objects channel cannot be nil"),
		}
		return errorCh
	}

	go c.removeObjects(ctx, bucketName, objectsCh, errorCh, opts)
	return errorCh
}

// Return true if the character is within the allowed characters in an XML 1.0 document
// The list of allowed characters can be found here: https://www.w3.org/TR/xml/#charsets
func validXMLChar(r rune) (ok bool) {
	return r == 0x09 ||
		r == 0x0A ||
		r == 0x0D ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

func hasInvalidXMLChar(str string) bool {
	for _, s := range str {
		if !validXMLChar(s) {
			return true
		}
	}
	return false
}

// Generate and call MultiDelete S3 requests based on entries received from objectsCh
func (c Client) removeObjects(ctx context.Context, bucketName string, objectsCh <-chan ObjectInfo, errorCh chan<- RemoveObjectError, opts RemoveObjectsOptions) {
	maxEntries := 1000
	finish := false
	urlValues := make(url.Values)
	urlValues.Set("delete", "")

	// Close error channel when Multi delete finishes.
	defer close(errorCh)

	// Loop over entries by 1000 and call MultiDelete requests
	for {
		if finish {
			break
		}
		count := 0
		var batch []ObjectInfo

		// Try to gather 1000 entries
		for object := range objectsCh {
			if hasInvalidXMLChar(object.Key) {
				// Use single DELETE so the object name will be in the request URL instead of the multi-delete XML document.
				err := c.removeObject(ctx, bucketName, object.Key, RemoveObjectOptions{
					VersionID:        object.VersionID,
					GovernanceBypass: opts.GovernanceBypass,
				})
				if err != nil {
					// Version does not exist is not an error ignore and continue.
					switch ToErrorResponse(err).Code {
					case "InvalidArgument", "NoSuchVersion":
						continue
					}
					errorCh <- RemoveObjectError{
						ObjectName: object.Key,
						VersionID:  object.VersionID,
						Err:        err,
					}
				}
				continue
			}

			batch = append(batch, object)
			if count++; count >= maxEntries {
				break
			}
		}
		if count == 0 {
			// Multi Objects Delete API doesn't accept empty object list, quit immediately
			break
		}
		if count < maxEntries {
			// We didn't have 1000 entries, so this is the last batch
			finish = true
		}

		// Build headers.
		headers := make(http.Header)
		if opts.GovernanceBypass {
			// Set the bypass goverenance retention header
			headers.Set(amzBypassGovernance, "true")
		}

		// Generate remove multi objects XML request
		removeBytes := generateRemoveMultiObjectsRequest(batch)
		// Execute GET on bucket to list objects.
		resp, err := c.executeMethod(ctx, http.MethodPost, requestMetadata{
			bucketName:       bucketName,
			queryValues:      urlValues,
			contentBody:      bytes.NewReader(removeBytes),
			contentLength:    int64(len(removeBytes)),
			contentMD5Base64: sumMD5Base64(removeBytes),
			contentSHA256Hex: sum256Hex(removeBytes),
			customHeader:     headers,
		})
		if resp != nil {
			if resp.StatusCode != http.StatusOK {
				e := httpRespToErrorResponse(resp, bucketName, "")
				errorCh <- RemoveObjectError{ObjectName: "", Err: e}
			}
		}
		if err != nil {
			for _, b := range batch {
				errorCh <- RemoveObjectError{
					ObjectName: b.Key,
					VersionID:  b.VersionID,
					Err:        err,
				}
			}
			continue
		}

		// Process multiobjects remove xml response
		processRemoveMultiObjectsResponse(resp.Body, batch, errorCh)

		closeResponse(resp)
	}
}

// RemoveIncompleteUpload aborts an partially uploaded object.
func (c Client) RemoveIncompleteUpload(ctx context.Context, bucketName, objectName string) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return err
	}
	// Find multipart upload ids of the object to be aborted.
	uploadIDs, err := c.findUploadIDs(ctx, bucketName, objectName)
	if err != nil {
		return err
	}

	for _, uploadID := range uploadIDs {
		// abort incomplete multipart upload, based on the upload id passed.
		err := c.abortMultipartUpload(ctx, bucketName, objectName, uploadID)
		if err != nil {
			return err
		}
	}

	return nil
}

// abortMultipartUpload aborts a multipart upload for the given
// uploadID, all previously uploaded parts are deleted.
func (c Client) abortMultipartUpload(ctx context.Context, bucketName, objectName, uploadID string) error {
	// Input validation.
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return err
	}

	// Initialize url queries.
	urlValues := make(url.Values)
	urlValues.Set("uploadId", uploadID)

	// Execute DELETE on multipart upload.
	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:       bucketName,
		objectName:       objectName,
		queryValues:      urlValues,
		contentSHA256Hex: emptySHA256Hex,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil {
		if resp.StatusCode != http.StatusNoContent {
			// Abort has no response body, handle it for any errors.
			var errorResponse ErrorResponse
			switch resp.StatusCode {
			case http.StatusNotFound:
				// This is needed specifically for abort and it cannot
				// be converged into default case.
				errorResponse = ErrorResponse{
					Code:       "NoSuchUpload",
					Message:    "The specified multipart upload does not exist.",
					BucketName: bucketName,
					Key:        objectName,
					RequestID:  resp.Header.Get("x-amz-request-id"),
					HostID:     resp.Header.Get("x-amz-id-2"),
					Region:     resp.Header.Get("x-amz-bucket-region"),
				}
			default:
				return httpRespToErrorResponse(resp, bucketName, objectName)
			}
			return errorResponse
		}
	}
	return nil
}
