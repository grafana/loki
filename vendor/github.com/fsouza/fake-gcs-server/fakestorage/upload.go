// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"crypto/md5" // #nosec G501
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

const contentTypeHeader = "Content-Type"

type multipartMetadata struct {
	Name string `json:"name"`
}

type contentRange struct {
	KnownRange bool // Is the range known, or "*"?
	KnownTotal bool // Is the total known, or "*"?
	Start      int  // Start of the range, -1 if unknown
	End        int  // End of the range, -1 if unknown
	Total      int  // Total bytes expected, -1 if unknown
}

func (s *Server) insertObject(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucketName"]
	if err := s.backend.GetBucket(bucketName); err != nil {
		w.WriteHeader(http.StatusNotFound)
		err := newErrorResponse(http.StatusNotFound, "Not found", nil)
		json.NewEncoder(w).Encode(err)
		return
	}
	uploadType := r.URL.Query().Get("uploadType")
	switch uploadType {
	case "media":
		s.simpleUpload(bucketName, w, r)
	case "multipart":
		s.multipartUpload(bucketName, w, r)
	case "resumable":
		s.resumableUpload(bucketName, w, r)
	default:
		http.Error(w, "invalid uploadType", http.StatusBadRequest)
	}
}

func (s *Server) simpleUpload(bucketName string, w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name is required for simple uploads", http.StatusBadRequest)
		return
	}
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	obj := Object{
		BucketName:  bucketName,
		Name:        name,
		Content:     data,
		ContentType: r.Header.Get(contentTypeHeader),
		Crc32c:      encodedCrc32cChecksum(data),
		Md5Hash:     encodedMd5Hash(data),
	}
	err = s.createObject(obj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(obj)
}

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func crc32cChecksum(content []byte) []byte {
	checksummer := crc32.New(crc32cTable)
	checksummer.Write(content)
	return checksummer.Sum(make([]byte, 0, 4))
}

func encodedChecksum(checksum []byte) string {
	return base64.StdEncoding.EncodeToString(checksum)
}

func encodedCrc32cChecksum(content []byte) string {
	return encodedChecksum(crc32cChecksum(content))
}

func md5Hash(b []byte) []byte {
	/* #nosec G401 */
	h := md5.New()
	h.Write(b)
	return h.Sum(nil)
}

func encodedHash(hash []byte) string {
	return base64.StdEncoding.EncodeToString(hash)
}

func encodedMd5Hash(content []byte) string {
	return encodedHash(md5Hash(content))
}

func (s *Server) multipartUpload(bucketName string, w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	_, params, err := mime.ParseMediaType(r.Header.Get(contentTypeHeader))
	if err != nil {
		http.Error(w, "invalid Content-Type header", http.StatusBadRequest)
		return
	}
	var (
		metadata *multipartMetadata
		content  []byte
	)
	var contentType string
	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	for ; err == nil; part, err = reader.NextPart() {
		if metadata == nil {
			metadata, err = loadMetadata(part)
		} else {
			contentType = part.Header.Get(contentTypeHeader)
			content, err = loadContent(part)
		}
		if err != nil {
			break
		}
	}
	if err != io.EOF {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	obj := Object{
		BucketName:  bucketName,
		Name:        metadata.Name,
		Content:     content,
		ContentType: contentType,
		Crc32c:      encodedCrc32cChecksum(content),
		Md5Hash:     encodedMd5Hash(content),
	}
	err = s.createObject(obj)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(obj)
}

func (s *Server) resumableUpload(bucketName string, w http.ResponseWriter, r *http.Request) {
	objName := r.URL.Query().Get("name")
	if objName == "" {
		metadata, err := loadMetadata(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		objName = metadata.Name
	}
	obj := Object{
		BucketName: bucketName,
		Name:       objName,
	}
	uploadID, err := generateUploadID()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.uploads.Store(uploadID, obj)
	w.Header().Set("Location", s.URL()+"/upload/resumable/"+uploadID)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(obj)
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
//   Content-Range: bytes 0-999/*
//   Content-Range: bytes 1000-1999/*
//   Content-Range: bytes 2000-2599/*
//   Content-Range: bytes */2600
//
// When sending chunked content of a known size, the total size is sent as
// well. The Python client uses this method to upload files and in-memory
// content. The sequence of "Content-Range" headers for the 2600-byte content
// sent in 1000-byte chunks are:
//
//   Content-Range: bytes 0-999/2600
//   Content-Range: bytes 1000-1999/2600
//   Content-Range: bytes 2000-2599/2600
//
// The server collects the content, analyzes the "Content-Range", and returns a
// "308 Permanent Redirect" response if more chunks are expected, and a
// "200 OK" response if the upload is complete (the Go client also accepts a
// "201 Created" response). The "Range" header in the response should be set to
// the size of the content received so far, such as:
//
//   Range: bytes 0-2000
//
// The client (such as the Go client) can send a header "X-Guploader-No-308" if
// it can't process a native "308 Permanent Redirect". The in-process response
// then has a status of "200 OK", with a header "X-Http-Status-Code-Override"
// set to "308".
func (s *Server) uploadFileContent(w http.ResponseWriter, r *http.Request) {
	uploadID := mux.Vars(r)["uploadId"]
	rawObj, ok := s.uploads.Load(uploadID)
	if !ok {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}
	obj := rawObj.(Object)
	content, err := loadContent(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	commit := true
	status := http.StatusOK
	obj.Content = append(obj.Content, content...)
	obj.Crc32c = encodedCrc32cChecksum(obj.Content)
	obj.Md5Hash = encodedMd5Hash(obj.Content)
	obj.ContentType = r.Header.Get(contentTypeHeader)
	if contentRange := r.Header.Get("Content-Range"); contentRange != "" {
		parsed, err := parseContentRange(contentRange)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if parsed.KnownRange {
			// Middle of streaming request, or any part of chunked request
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", parsed.End))
			// Complete if the range covers the known total
			commit = parsed.KnownTotal && (parsed.End+1 >= parsed.Total)
		} else {
			// End of a streaming request
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(obj.Content)))
		}
	}
	if commit {
		s.uploads.Delete(uploadID)
		err = s.createObject(obj)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if _, no308 := r.Header["X-Guploader-No-308"]; no308 {
			// Go client
			w.Header().Set("X-Http-Status-Code-Override", "308")
		} else {
			// Python client
			status = http.StatusPermanentRedirect
		}
		s.uploads.Store(uploadID, obj)
	}
	data, _ := json.Marshal(obj)
	w.Header().Set(contentTypeHeader, "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	w.Write(data)
}

// Parse a Content-Range header
// Some possible valid header values:
//   bytes 0-1023/4096 (first 1024 bytes of a 4096-byte document)
//   bytes 1024-2047/* (second 1024 bytes of a streaming document)
//   bytes */4096      (The end of 4096 byte streaming document)
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
		parsed.KnownRange = true
		parsed.Start, err = strconv.Atoi(rangeParts[0])
		if err != nil {
			return parsed, invalidErr
		}
		parsed.End, err = strconv.Atoi(rangeParts[1])
		if err != nil {
			return parsed, invalidErr
		}
	}

	// Process total length
	if parts[1] == "*" {
		parsed.Total = -1
		if !parsed.KnownRange {
			// Must know either range or total
			return parsed, invalidErr
		}
	} else {
		parsed.KnownTotal = true
		parsed.Total, err = strconv.Atoi(parts[1])
		if err != nil {
			return parsed, invalidErr
		}
	}

	return parsed, nil
}

func loadMetadata(rc io.ReadCloser) (*multipartMetadata, error) {
	defer rc.Close()
	var m multipartMetadata
	err := json.NewDecoder(rc).Decode(&m)
	return &m, err
}

func loadContent(rc io.ReadCloser) ([]byte, error) {
	defer rc.Close()
	return ioutil.ReadAll(rc)
}

func generateUploadID() (string, error) {
	var raw [16]byte
	_, err := rand.Read(raw[:])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", raw[:]), nil
}
