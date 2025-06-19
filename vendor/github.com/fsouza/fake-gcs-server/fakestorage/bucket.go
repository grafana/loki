// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/fsouza/fake-gcs-server/internal/backend"
	"github.com/gorilla/mux"
)

// https://cloud.google.com/storage/docs/buckets#naming
var bucketRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$`)

// CreateBucket creates a bucket inside the server, so any API calls that
// require the bucket name will recognize this bucket.
//
// If the bucket already exists, this method does nothing.
//
// Deprecated: use CreateBucketWithOpts.
func (s *Server) CreateBucket(name string) {
	err := s.backend.CreateBucket(name, backend.BucketAttrs{VersioningEnabled: false, DefaultEventBasedHold: false})
	if err != nil {
		panic(err)
	}
}

func (s *Server) updateBucket(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]
	attrsToUpdate := getBucketAttrsToUpdate(r.Body)
	err := s.backend.UpdateBucket(bucketName, attrsToUpdate)
	if err != nil {
		panic(err)
	}
	return jsonResponse{}
}

func getBucketAttrsToUpdate(body io.ReadCloser) backend.BucketAttrs {
	var data struct {
		DefaultEventBasedHold bool             `json:"defaultEventBasedHold,omitempty"`
		Versioning            bucketVersioning `json:"versioning,omitempty"`
	}
	err := json.NewDecoder(body).Decode(&data)
	if err != nil {
		panic(err)
	}
	attrsToUpdate := backend.BucketAttrs{
		DefaultEventBasedHold: data.DefaultEventBasedHold,
		VersioningEnabled:     data.Versioning.Enabled,
	}
	return attrsToUpdate
}

// CreateBucketOpts defines the properties of a bucket you can create with
// CreateBucketWithOpts.
type CreateBucketOpts struct {
	Name                  string
	VersioningEnabled     bool
	DefaultEventBasedHold bool
}

// CreateBucketWithOpts creates a bucket inside the server, so any API calls that
// require the bucket name will recognize this bucket. Use CreateBucketOpts to
// customize the options for this bucket
//
// If the underlying backend returns an error, this method panics.
func (s *Server) CreateBucketWithOpts(opts CreateBucketOpts) {
	err := s.backend.CreateBucket(opts.Name, backend.BucketAttrs{VersioningEnabled: opts.VersioningEnabled, DefaultEventBasedHold: opts.DefaultEventBasedHold})
	if err != nil {
		panic(err)
	}
}

func (s *Server) createBucketByPost(r *http.Request) jsonResponse {
	// Minimal version of Bucket from google.golang.org/api/storage/v1

	var data struct {
		Name                  string            `json:"name,omitempty"`
		Versioning            *bucketVersioning `json:"versioning,omitempty"`
		DefaultEventBasedHold bool              `json:"defaultEventBasedHold,omitempty"`
	}

	// Read the bucket props from the request body JSON
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return jsonResponse{errorMessage: err.Error(), status: http.StatusBadRequest}
	}
	name := data.Name
	versioning := false
	if data.Versioning != nil {
		versioning = data.Versioning.Enabled
	}
	defaultEventBasedHold := data.DefaultEventBasedHold
	if err := validateBucketName(name); err != nil {
		return jsonResponse{errorMessage: err.Error(), status: http.StatusBadRequest}
	}

	_, err := s.backend.GetBucket(name)
	if err == nil {
		return jsonResponse{
			errorMessage: fmt.Sprintf(
				"A Cloud Storage bucket named '%s' already exists. "+
					"Try another name. Bucket names must be globally unique "+
					"across all Google Cloud projects, including those "+
					"outside of your organization.", name),
			status: http.StatusConflict,
		}
	}

	// Create the named bucket
	if err := s.backend.CreateBucket(name, backend.BucketAttrs{VersioningEnabled: versioning, DefaultEventBasedHold: defaultEventBasedHold}); err != nil {
		return jsonResponse{errorMessage: err.Error()}
	}

	// Return the created bucket:
	bucket, err := s.backend.GetBucket(name)
	if err != nil {
		return jsonResponse{errorMessage: err.Error()}
	}
	return jsonResponse{data: newBucketResponse(bucket, s.options.BucketsLocation)}
}

func (s *Server) listBuckets(r *http.Request) jsonResponse {
	buckets, err := s.backend.ListBuckets()
	if err != nil {
		return jsonResponse{errorMessage: err.Error()}
	}
	return jsonResponse{data: newListBucketsResponse(buckets, s.options.BucketsLocation)}
}

func (s *Server) getBucket(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]
	bucket, err := s.backend.GetBucket(bucketName)
	if err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}
	return jsonResponse{data: newBucketResponse(bucket, s.options.BucketsLocation)}
}

func (s *Server) deleteBucket(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]
	err := s.backend.DeleteBucket(bucketName)
	if err == backend.BucketNotFound {
		return jsonResponse{status: http.StatusNotFound}
	}
	if err == backend.BucketNotEmpty {
		return jsonResponse{status: http.StatusPreconditionFailed, errorMessage: err.Error()}
	}
	if err != nil {
		return jsonResponse{status: http.StatusInternalServerError, errorMessage: err.Error()}
	}
	return jsonResponse{}
}

func validateBucketName(bucketName string) error {
	if !bucketRegexp.MatchString(bucketName) {
		return errors.New("invalid bucket name")
	}
	return nil
}
