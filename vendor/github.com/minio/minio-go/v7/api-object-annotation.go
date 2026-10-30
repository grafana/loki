/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2026 MinIO, Inc.
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
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7/pkg/s3utils"
)

// Object annotations are named payloads (1 byte to 1 MiB of UTF-8 text)
// attached to a specific object version, independent of the object's data.
// Up to 1,000 annotations may be attached per object version.

const (
	amzObjectIfMatchHeader = "x-amz-object-if-match"

	// maxAnnotationPayloadBytes is the maximum size of a single annotation payload (1 MiB).
	maxAnnotationPayloadBytes = 1 << 20

	// maxAnnotationNameBytes is the maximum length of an annotation name.
	maxAnnotationNameBytes = 512
)

// validateAnnotationName performs a minimal client-side check on the annotation
// name (non-empty, within the length limit). The server enforces the full
// naming rules (allowed characters, reserved prefixes).
func validateAnnotationName(name string) error {
	if name == "" {
		return errInvalidArgument("annotation name must not be empty")
	}
	if len(name) > maxAnnotationNameBytes {
		return errInvalidArgument("annotation name exceeds 512 bytes")
	}
	return nil
}

// PutObjectAnnotationOptions configures a PutObjectAnnotation request.
type PutObjectAnnotationOptions struct {
	// VersionID targets a specific object version (versioned buckets).
	VersionID string
	// IfMatch, when set, only applies the annotation if the parent object's
	// ETag matches this value (sent as x-amz-object-if-match).
	IfMatch string
}

// GetObjectAnnotationOptions configures a GetObjectAnnotation request.
type GetObjectAnnotationOptions struct {
	VersionID string
}

// ListObjectAnnotationsOptions configures a ListObjectAnnotations request.
type ListObjectAnnotationsOptions struct {
	VersionID string
}

// RemoveObjectAnnotationOptions configures a DeleteObjectAnnotation request.
type RemoveObjectAnnotationOptions struct {
	VersionID string
	IfMatch   string
}

// ObjectAnnotation describes a single annotation as returned by ListObjectAnnotations.
type ObjectAnnotation struct {
	Name         string
	Size         int64
	ETag         string
	LastModified time.Time
}

// listObjectAnnotationsOutput maps the ListObjectAnnotations XML response.
type listObjectAnnotationsOutput struct {
	XMLName     xml.Name `xml:"ListObjectAnnotationsOutput"`
	Annotations []struct {
		AnnotationName string `xml:"AnnotationName"`
		Size           int64  `xml:"Size"`
		ETag           string `xml:"ETag"`
		LastModified   string `xml:"LastModified"`
	} `xml:"Annotation"`
}

func annotationQueryValues(name, versionID string) url.Values {
	urlValues := make(url.Values)
	urlValues.Set("annotation", "")
	if name != "" {
		urlValues.Set("annotationName", name)
	}
	if versionID != "" {
		urlValues.Set("versionId", versionID)
	}
	return urlValues
}

// PutObjectAnnotation creates or overwrites a named annotation on an object
// version. The payload (1 byte to 1 MiB) is streamed directly from the supplied
// ReadSeeker: its size is taken from a seek to the end, so the body is sent with
// an exact Content-Length and never buffered in memory. It returns the
// annotation's ETag. The parent object's ETag is never modified.
func (c *Client) PutObjectAnnotation(ctx context.Context, bucketName, objectName, annotationName string, payload io.ReadSeeker, opts PutObjectAnnotationOptions) (string, error) {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return "", err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return "", err
	}
	if err := validateAnnotationName(annotationName); err != nil {
		return "", err
	}
	if payload == nil {
		return "", errInvalidArgument("annotation payload must not be nil")
	}

	// Derive the payload size from the seeker and enforce the limits before
	// uploading; the server reads the body raw, so it is sent with UNSIGNED-PAYLOAD
	// (no streaming-chunked signature) and streamed without buffering.
	size, err := payload.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if size == 0 {
		return "", errInvalidArgument("annotation payload must be at least 1 byte")
	}
	if size > maxAnnotationPayloadBytes {
		return "", errInvalidArgument("annotation payload exceeds the 1 MiB maximum")
	}

	headers := make(http.Header)
	if opts.IfMatch != "" {
		headers.Set(amzObjectIfMatchHeader, opts.IfMatch)
	}

	resp, err := c.executeMethod(ctx, http.MethodPut, requestMetadata{
		bucketName:    bucketName,
		objectName:    objectName,
		queryValues:   annotationQueryValues(annotationName, opts.VersionID),
		contentBody:   payload,
		contentLength: size,
		customHeader:  headers,
	})
	defer closeResponse(resp)
	if err != nil {
		return "", err
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		return "", httpRespToErrorResponse(resp, bucketName, objectName)
	}
	return trimEtag(resp.Header.Get("ETag")), nil
}

// GetObjectAnnotation returns the payload of a single named annotation as a
// stream. The returned ReadCloser is the response body; the caller must Close it
// once the payload has been read so the underlying connection can be reused. The
// payload is server-capped at 1 MiB.
func (c *Client) GetObjectAnnotation(ctx context.Context, bucketName, objectName, annotationName string, opts GetObjectAnnotationOptions) (io.ReadCloser, error) {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, err
	}
	if err := validateAnnotationName(annotationName); err != nil {
		return nil, err
	}

	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		objectName:  objectName,
		queryValues: annotationQueryValues(annotationName, opts.VersionID),
	})
	if err != nil {
		closeResponse(resp)
		return nil, err
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		defer closeResponse(resp)
		return nil, httpRespToErrorResponse(resp, bucketName, objectName)
	}
	return resp.Body, nil
}

// ListObjectAnnotations returns all annotations attached to an object version.
func (c *Client) ListObjectAnnotations(ctx context.Context, bucketName, objectName string, opts ListObjectAnnotationsOptions) ([]ObjectAnnotation, error) {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return nil, err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return nil, err
	}

	resp, err := c.executeMethod(ctx, http.MethodGet, requestMetadata{
		bucketName:  bucketName,
		objectName:  objectName,
		queryValues: annotationQueryValues("", opts.VersionID),
	})
	defer closeResponse(resp)
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		return nil, httpRespToErrorResponse(resp, bucketName, objectName)
	}

	var out listObjectAnnotationsOutput
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	annotations := make([]ObjectAnnotation, 0, len(out.Annotations))
	for _, a := range out.Annotations {
		// LastModified is an RFC3339 timestamp. A malformed value from the
		// server leaves LastModified as the zero time rather than failing the
		// entire listing for one bad entry.
		var lastModified time.Time
		if a.LastModified != "" {
			lastModified, _ = time.Parse(time.RFC3339, a.LastModified)
		}
		annotations = append(annotations, ObjectAnnotation{
			Name:         a.AnnotationName,
			Size:         a.Size,
			ETag:         trimEtag(a.ETag),
			LastModified: lastModified,
		})
	}
	return annotations, nil
}

// RemoveObjectAnnotation permanently deletes a single named annotation. Deletion
// is irreversible: annotations have no version history.
func (c *Client) RemoveObjectAnnotation(ctx context.Context, bucketName, objectName, annotationName string, opts RemoveObjectAnnotationOptions) error {
	if err := s3utils.CheckValidBucketName(bucketName); err != nil {
		return err
	}
	if err := s3utils.CheckValidObjectName(objectName); err != nil {
		return err
	}
	if err := validateAnnotationName(annotationName); err != nil {
		return err
	}

	headers := make(http.Header)
	if opts.IfMatch != "" {
		headers.Set(amzObjectIfMatchHeader, opts.IfMatch)
	}

	resp, err := c.executeMethod(ctx, http.MethodDelete, requestMetadata{
		bucketName:   bucketName,
		objectName:   objectName,
		queryValues:  annotationQueryValues(annotationName, opts.VersionID),
		customHeader: headers,
	})
	defer closeResponse(resp)
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return httpRespToErrorResponse(resp, bucketName, objectName)
	}
	return nil
}
