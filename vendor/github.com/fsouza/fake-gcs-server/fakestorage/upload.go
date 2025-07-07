// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/internal/backend"
	"github.com/fsouza/fake-gcs-server/internal/checksum"
	"github.com/gorilla/mux"
)

const (
	contentTypeHeader  = "Content-Type"
	cacheControlHeader = "Cache-Control"
)

const (
	uploadTypeMedia     = "media"
	uploadTypeMultipart = "multipart"
	uploadTypeResumable = "resumable"
)

// per RFC 2045, double quotes should be used whenever parameters have a value
// that includes some special character - anything in the set: ()<>@,;:\"/[]?=
// (including space). gsutil likes to use `=` in the boundary, but incorrectly
// quotes it using single quotes.
//
// We do exclude \ and " from the regexp because those are not supported by the
// mime package.
//
// This has been reported to gsutil
// (https://github.com/GoogleCloudPlatform/gsutil/issues/1466). If that issue
// ever gets closed, we should be able to get rid of this hack.
var gsutilBoundary = regexp.MustCompile(`boundary='([^']*[()<>@,;:"/\[\]?= ]+[^']*)'`)

type multipartMetadata struct {
	ContentType        string            `json:"contentType"`
	ContentEncoding    string            `json:"contentEncoding"`
	ContentDisposition string            `json:"contentDisposition"`
	ContentLanguage    string            `json:"ContentLanguage"`
	CacheControl       string            `json:"cacheControl"`
	CustomTime         time.Time         `json:"customTime,omitempty"`
	Name               string            `json:"name"`
	StorageClass       string            `json:"storageClass"`
	Metadata           map[string]string `json:"metadata"`
}

type contentRange struct {
	KnownRange bool // Is the range known, or "*"?
	KnownTotal bool // Is the total known, or "*"?
	Start      int  // Start of the range, -1 if unknown
	End        int  // End of the range, -1 if unknown
	Total      int  // Total bytes expected, -1 if unknown
}

type generationCondition struct {
	ifGenerationMatch    *int64
	ifGenerationNotMatch *int64
}

func (c generationCondition) ConditionsMet(activeGeneration int64) bool {
	if c.ifGenerationMatch != nil && *c.ifGenerationMatch != activeGeneration {
		return false
	}
	if c.ifGenerationNotMatch != nil && *c.ifGenerationNotMatch == activeGeneration {
		return false
	}
	return true
}

func (s *Server) insertObject(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]

	if _, err := s.backend.GetBucket(bucketName); err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	uploadType := r.URL.Query().Get("uploadType")
	if uploadType == "" && r.Header.Get("X-Goog-Upload-Protocol") == uploadTypeResumable {
		uploadType = uploadTypeResumable
	}

	switch uploadType {
	case uploadTypeMedia:
		return s.simpleUpload(bucketName, r)
	case uploadTypeMultipart:
		return s.multipartUpload(bucketName, r)
	case uploadTypeResumable:
		return s.resumableUpload(bucketName, r)
	default:
		// Support Signed URL Uploads
		if r.URL.Query().Get("X-Goog-Algorithm") != "" {
			switch r.Method {
			case http.MethodPost:
				return s.resumableUpload(bucketName, r)
			case http.MethodPut:
				return s.signedUpload(bucketName, r)
			}
		}
		return jsonResponse{errorMessage: "invalid uploadType", status: http.StatusBadRequest}
	}
}

func (s *Server) insertFormObject(r *http.Request) xmlResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]

	if err := r.ParseMultipartForm(32 << 20); nil != err {
		return xmlResponse{errorMessage: "invalid form", status: http.StatusBadRequest}
	}

	// Load metadata
	var name string
	if keys, ok := r.MultipartForm.Value["key"]; ok {
		name = keys[0]
	}
	if name == "" {
		return xmlResponse{errorMessage: "missing key", status: http.StatusBadRequest}
	}
	var predefinedACL string
	if acls, ok := r.MultipartForm.Value["acl"]; ok {
		predefinedACL = acls[0]
	}
	var contentEncoding string
	if contentEncodings, ok := r.MultipartForm.Value["Content-Encoding"]; ok {
		contentEncoding = contentEncodings[0]
	}
	var contentType string
	if contentTypes, ok := r.MultipartForm.Value["Content-Type"]; ok {
		contentType = contentTypes[0]
	}
	successActionStatus := http.StatusNoContent
	if successActionStatuses, ok := r.MultipartForm.Value["success_action_status"]; ok {
		successInt, err := strconv.Atoi(successActionStatuses[0])
		if err != nil {
			return xmlResponse{errorMessage: err.Error(), status: http.StatusBadRequest}
		}
		if successInt != http.StatusOK && successInt != http.StatusCreated && successInt != http.StatusNoContent {
			return xmlResponse{errorMessage: "invalid success action status", status: http.StatusBadRequest}
		}
		successActionStatus = successInt
	}
	metaData := make(map[string]string)
	for key := range r.MultipartForm.Value {
		lowerKey := strings.ToLower(key)
		if metaDataKey := strings.TrimPrefix(lowerKey, "x-goog-meta-"); metaDataKey != lowerKey {
			metaData[metaDataKey] = r.MultipartForm.Value[key][0]
		}
	}

	// Load file
	var file *multipart.FileHeader
	if files, ok := r.MultipartForm.File["file"]; ok {
		file = files[0]
	}
	if file == nil {
		return xmlResponse{errorMessage: "missing file", status: http.StatusBadRequest}
	}
	infile, err := file.Open()
	if err != nil {
		return xmlResponse{errorMessage: err.Error()}
	}
	obj := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:      bucketName,
			Name:            name,
			ContentType:     contentType,
			ContentEncoding: contentEncoding,
			ACL:             getObjectACL(predefinedACL),
			Metadata:        metaData,
		},
		Content: infile,
	}
	obj, err = s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return xmlResponse{errorMessage: err.Error()}
	}
	defer obj.Close()

	if successActionStatus == 201 {
		objectURI := fmt.Sprintf("%s/%s%s", s.URL(), bucketName, name)
		xmlBody := createXmlResponseBody(bucketName, obj.Etag, strings.TrimPrefix(name, "/"), objectURI)
		return xmlResponse{status: successActionStatus, data: xmlBody}
	}
	return xmlResponse{status: successActionStatus}
}

func (s *Server) wrapUploadPreconditions(r *http.Request, bucketName string, objectName string) (generationCondition, error) {
	result := generationCondition{
		ifGenerationMatch:    nil,
		ifGenerationNotMatch: nil,
	}
	ifGenerationMatch := r.URL.Query().Get("ifGenerationMatch")

	if ifGenerationMatch != "" {
		gen, err := strconv.ParseInt(ifGenerationMatch, 10, 64)
		if err != nil {
			return generationCondition{}, err
		}
		result.ifGenerationMatch = &gen
	}

	ifGenerationNotMatch := r.URL.Query().Get("ifGenerationNotMatch")

	if ifGenerationNotMatch != "" {
		gen, err := strconv.ParseInt(ifGenerationNotMatch, 10, 64)
		if err != nil {
			return generationCondition{}, err
		}
		result.ifGenerationNotMatch = &gen
	}

	return result, nil
}

func (s *Server) simpleUpload(bucketName string, r *http.Request) jsonResponse {
	defer r.Body.Close()
	name := r.URL.Query().Get("name")
	predefinedACL := r.URL.Query().Get("predefinedAcl")
	contentEncoding := r.URL.Query().Get("contentEncoding")
	customTime := r.URL.Query().Get("customTime")
	if name == "" {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "name is required for simple uploads",
		}
	}
	obj := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:      bucketName,
			Name:            name,
			ContentType:     r.Header.Get(contentTypeHeader),
			CacheControl:    r.Header.Get(cacheControlHeader),
			ContentEncoding: contentEncoding,
			CustomTime:      convertTimeWithoutError(customTime),
			ACL:             getObjectACL(predefinedACL),
		},
		Content: notImplementedSeeker{r.Body},
	}
	obj, err := s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return errToJsonResponse(err)
	}
	obj.Close()
	return jsonResponse{data: newObjectResponse(obj.ObjectAttrs, s.externalURL)}
}

type notImplementedSeeker struct {
	io.ReadCloser
}

func (s notImplementedSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.New("not implemented")
}

func (s *Server) signedUpload(bucketName string, r *http.Request) jsonResponse {
	defer r.Body.Close()
	name := unescapeMuxVars(mux.Vars(r))["objectName"]
	predefinedACL := r.URL.Query().Get("predefinedAcl")
	contentEncoding := r.URL.Query().Get("contentEncoding")
	customTime := r.URL.Query().Get("customTime")

	// Load data from HTTP Headers
	if contentEncoding == "" {
		contentEncoding = r.Header.Get("Content-Encoding")
	}

	metaData := make(map[string]string)
	for key := range r.Header {
		lowerKey := strings.ToLower(key)
		if metaDataKey := strings.TrimPrefix(lowerKey, "x-goog-meta-"); metaDataKey != lowerKey {
			metaData[metaDataKey] = r.Header.Get(key)
		}
	}

	obj := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:      bucketName,
			Name:            name,
			ContentType:     r.Header.Get(contentTypeHeader),
			ContentEncoding: contentEncoding,
			CustomTime:      convertTimeWithoutError(customTime),
			ACL:             getObjectACL(predefinedACL),
			Metadata:        metaData,
		},
		Content: notImplementedSeeker{r.Body},
	}
	obj, err := s.createObject(obj, backend.NoConditions{})
	if err != nil {
		return errToJsonResponse(err)
	}
	obj.Close()
	return jsonResponse{data: newObjectResponse(obj.ObjectAttrs, s.externalURL)}
}

func getObjectACL(predefinedACL string) []storage.ACLRule {
	if predefinedACL == "publicRead" {
		return []storage.ACLRule{
			{
				Entity: "allUsers",
				Role:   "READER",
			},
		}
	}

	return []storage.ACLRule{
		{
			Entity: "projectOwner-test-project",
			Role:   "OWNER",
		},
	}
}

func (s *Server) multipartUpload(bucketName string, r *http.Request) jsonResponse {
	defer r.Body.Close()
	params, err := parseContentTypeParams(r.Header.Get(contentTypeHeader))
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "invalid Content-Type header",
		}
	}
	var (
		metadata *multipartMetadata
		content  []byte
	)
	var contentType string
	reader := multipart.NewReader(r.Body, params["boundary"])

	var partReaders []io.Reader

	part, err := reader.NextPart()
	for ; err == nil; part, err = reader.NextPart() {
		if metadata == nil {
			metadata, err = loadMetadata(part)
			contentType = metadata.ContentType
		} else {
			contentType = part.Header.Get(contentTypeHeader)
			content, err = loadContent(part)
			partReaders = append(partReaders, bytes.NewReader(content))
		}
		if err != nil {
			break
		}
	}
	if err != io.EOF {
		return jsonResponse{errorMessage: err.Error()}
	}

	objName := r.URL.Query().Get("name")
	predefinedACL := r.URL.Query().Get("predefinedAcl")
	if objName == "" {
		objName = metadata.Name
	}

	conditions, err := s.wrapUploadPreconditions(r, bucketName, objName)
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: err.Error(),
		}
	}

	obj := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:         bucketName,
			Name:               objName,
			StorageClass:       metadata.StorageClass,
			ContentType:        contentType,
			CacheControl:       metadata.CacheControl,
			ContentEncoding:    metadata.ContentEncoding,
			ContentDisposition: metadata.ContentDisposition,
			ContentLanguage:    metadata.ContentLanguage,
			CustomTime:         metadata.CustomTime,
			ACL:                getObjectACL(predefinedACL),
			Metadata:           metadata.Metadata,
		},
		Content: notImplementedSeeker{io.NopCloser(io.MultiReader(partReaders...))},
	}

	obj, err = s.createObject(obj, conditions)
	if err != nil {
		return errToJsonResponse(err)
	}
	defer obj.Close()
	return jsonResponse{data: newObjectResponse(obj.ObjectAttrs, s.externalURL)}
}

func parseContentTypeParams(requestContentType string) (map[string]string, error) {
	requestContentType = gsutilBoundary.ReplaceAllString(requestContentType, `boundary="$1"`)
	_, params, err := mime.ParseMediaType(requestContentType)
	return params, err
}

func (s *Server) resumableUpload(bucketName string, r *http.Request) jsonResponse {
	if r.URL.Query().Has("upload_id") {
		return s.uploadFileContent(r)
	}
	predefinedACL := r.URL.Query().Get("predefinedAcl")
	contentEncoding := r.URL.Query().Get("contentEncoding")
	metadata := new(multipartMetadata)
	if r.Body != http.NoBody {
		var err error
		metadata, err = loadMetadata(r.Body)
		if err != nil {
			return jsonResponse{errorMessage: err.Error()}
		}
	}
	objName := r.URL.Query().Get("name")
	if objName == "" {
		objName = metadata.Name
	}
	if contentEncoding == "" {
		contentEncoding = metadata.ContentEncoding
	}
	obj := Object{
		ObjectAttrs: ObjectAttrs{
			BucketName:      bucketName,
			Name:            objName,
			ContentType:     metadata.ContentType,
			CacheControl:    metadata.CacheControl,
			ContentEncoding: contentEncoding,
			CustomTime:      metadata.CustomTime,
			ACL:             getObjectACL(predefinedACL),
			Metadata:        metadata.Metadata,
		},
	}
	uploadID, err := generateUploadID()
	if err != nil {
		return jsonResponse{errorMessage: err.Error()}
	}
	s.uploads.Store(uploadID, obj)
	header := make(http.Header)
	location := fmt.Sprintf(
		"%s/upload/storage/v1/b/%s/o?uploadType=resumable&name=%s&upload_id=%s",
		s.URL(),
		bucketName,
		url.PathEscape(objName),
		uploadID,
	)
	header.Set("Location", location)
	if r.Header.Get("X-Goog-Upload-Command") == "start" {
		header.Set("X-Goog-Upload-URL", location)
		header.Set("X-Goog-Upload-Status", "active")
	}
	return jsonResponse{
		data:   newObjectResponse(obj.ObjectAttrs, s.externalURL),
		header: header,
	}
}

// uploadFileContent accepts a chunk of a resumable upload
//
// A resumable upload is sent in one or more chunks. The request's
// "Content-Range" header is used to determine if more data is expected.
//
// When sending streaming content, the total size is unknown until the stream
// is exhausted. The Go client always sends streaming content. The sequence of
// "Content-Range" headers for 2600-byte content sent in 1000-byte chunks are:
//
//	Content-Range: bytes 0-999/*
//	Content-Range: bytes 1000-1999/*
//	Content-Range: bytes 2000-2599/*
//	Content-Range: bytes */2600
//
// When sending chunked content of a known size, the total size is sent as
// well. The Python client uses this method to upload files and in-memory
// content. The sequence of "Content-Range" headers for the 2600-byte content
// sent in 1000-byte chunks are:
//
//	Content-Range: bytes 0-999/2600
//	Content-Range: bytes 1000-1999/2600
//	Content-Range: bytes 2000-2599/2600
//
// The server collects the content, analyzes the "Content-Range", and returns a
// "308 Permanent Redirect" response if more chunks are expected, and a
// "200 OK" response if the upload is complete (the Go client also accepts a
// "201 Created" response). The "Range" header in the response should be set to
// the size of the content received so far, such as:
//
//	Range: bytes 0-2000
//
// The client (such as the Go client) can send a header "X-Guploader-No-308" if
// it can't process a native "308 Permanent Redirect". The in-process response
// then has a status of "200 OK", with a header "X-Http-Status-Code-Override"
// set to "308".
func (s *Server) uploadFileContent(r *http.Request) jsonResponse {
	uploadID := r.URL.Query().Get("upload_id")
	rawObj, ok := s.uploads.Load(uploadID)
	if !ok {
		return jsonResponse{status: http.StatusNotFound}
	}
	obj := rawObj.(Object)
	// TODO: stream upload file content to and from disk (when using the FS
	// backend, at least) instead of loading the entire content into memory.
	content, err := loadContent(r.Body)
	if err != nil {
		return jsonResponse{errorMessage: err.Error()}
	}
	commit := true
	status := http.StatusOK
	obj.Content = append(obj.Content, content...)
	obj.Crc32c = checksum.EncodedCrc32cChecksum(obj.Content)
	obj.Md5Hash = checksum.EncodedMd5Hash(obj.Content)
	obj.Etag = obj.Md5Hash
	contentTypeHeader := r.Header.Get(contentTypeHeader)
	if contentTypeHeader != "" {
		obj.ContentType = contentTypeHeader
	} else {
		obj.ContentType = "application/octet-stream"
	}
	responseHeader := make(http.Header)
	if contentRange := r.Header.Get("Content-Range"); contentRange != "" {
		parsed, err := parseContentRange(contentRange)
		if err != nil {
			return jsonResponse{errorMessage: err.Error(), status: http.StatusBadRequest}
		}
		if parsed.KnownRange {
			// Middle of streaming request, or any part of chunked request
			responseHeader.Set("Range", fmt.Sprintf("bytes=0-%d", parsed.End))
			// Complete if the range covers the known total
			commit = parsed.KnownTotal && (parsed.End+1 >= parsed.Total)
		} else {
			// End of a streaming request
			responseHeader.Set("Range", fmt.Sprintf("bytes=0-%d", len(obj.Content)))
		}
	}
	if commit {
		s.uploads.Delete(uploadID)
		streamingObject, err := s.createObject(obj.StreamingObject(), backend.NoConditions{})
		if err != nil {
			return errToJsonResponse(err)
		}
		defer streamingObject.Close()
		obj, err = streamingObject.BufferedObject()
		if err != nil {
			return errToJsonResponse(err)
		}
	} else {
		if _, no308 := r.Header["X-Guploader-No-308"]; no308 {
			// Go client
			responseHeader.Set("X-Http-Status-Code-Override", "308")
		} else {
			// Python client
			status = http.StatusPermanentRedirect
		}
		s.uploads.Store(uploadID, obj)
	}
	if r.Header.Get("X-Goog-Upload-Command") == "upload, finalize" {
		responseHeader.Set("X-Goog-Upload-Status", "final")
	}
	return jsonResponse{
		status: status,
		data:   newObjectResponse(obj.ObjectAttrs, s.externalURL),
		header: responseHeader,
	}
}

// Parse a Content-Range header
// Some possible valid header values:
//
//	bytes 0-1023/4096 (first 1024 bytes of a 4096-byte document)
//	bytes 1024-2047/* (second 1024 bytes of a streaming document)
//	bytes */4096      (The end of 4096 byte streaming document)
//	bytes 0-*/*       (start and end of a streaming document as sent by nodeJS client lib)
//	bytes */*         (start and end of a streaming document as sent by the C++ SDK)
func parseContentRange(r string) (parsed contentRange, err error) {
	invalidErr := fmt.Errorf("invalid Content-Range: %v", r)

	// Require that units == "bytes"
	const bytesPrefix = "bytes "
	if !strings.HasPrefix(r, bytesPrefix) {
		return parsed, invalidErr
	}

	// Split range from total length
	parts := strings.SplitN(r[len(bytesPrefix):], "/", 2)
	if len(parts) != 2 {
		return parsed, invalidErr
	}

	// Process range
	if parts[0] == "*" {
		parsed.Start = -1
		parsed.End = -1
	} else {
		rangeParts := strings.SplitN(parts[0], "-", 2)
		if len(rangeParts) != 2 {
			return parsed, invalidErr
		}

		parsed.Start, err = strconv.Atoi(rangeParts[0])
		if err != nil {
			return parsed, invalidErr
		}

		if rangeParts[1] == "*" {
			parsed.End = -1
		} else {
			parsed.KnownRange = true
			parsed.End, err = strconv.Atoi(rangeParts[1])
			if err != nil {
				return parsed, invalidErr
			}
		}
	}

	// Process total length
	if parts[1] == "*" {
		parsed.Total = -1
	} else {
		parsed.KnownTotal = true
		parsed.Total, err = strconv.Atoi(parts[1])
		if err != nil {
			return parsed, invalidErr
		}
	}

	return parsed, nil
}

func (s *Server) deleteResumableUpload(r *http.Request) jsonResponse {
	return jsonResponse{status: 499}
}

func loadMetadata(rc io.ReadCloser) (*multipartMetadata, error) {
	defer rc.Close()
	var m multipartMetadata
	err := json.NewDecoder(rc).Decode(&m)
	return &m, err
}

func loadContent(rc io.ReadCloser) ([]byte, error) {
	defer rc.Close()
	return io.ReadAll(rc)
}

func generateUploadID() (string, error) {
	var raw [16]byte
	_, err := rand.Read(raw[:])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", raw[:]), nil
}
