// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

// CreateBucket creates a bucket inside the server, so any API calls that
// require the bucket name will recognize this bucket.
//
// If the bucket already exists, this method does nothing.
func (s *Server) CreateBucket(name string) {
	err := s.backend.CreateBucket(name)
	if err != nil {
		panic(err)
	}
}

// createBucketByPost handles a POST request to create a bucket
func (s *Server) createBucketByPost(w http.ResponseWriter, r *http.Request) {
	// Minimal version of Bucket from google.golang.org/api/storage/v1
	var data struct {
		Name string
	}

	// Read the bucket name from the request body JSON
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := data.Name

	// Create the named bucket
	if err := s.backend.CreateBucket(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the created bucket:
	resp := newBucketResponse(name)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) listBuckets(w http.ResponseWriter, r *http.Request) {
	bucketNames, err := s.backend.ListBuckets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := newListBucketsResponse(bucketNames)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) getBucket(w http.ResponseWriter, r *http.Request) {
	bucketName := mux.Vars(r)["bucketName"]
	encoder := json.NewEncoder(w)
	if err := s.backend.GetBucket(bucketName); err != nil {
		w.WriteHeader(http.StatusNotFound)
		err := newErrorResponse(http.StatusNotFound, "Not found", nil)
		encoder.Encode(err)
		return
	}
	resp := newBucketResponse(bucketName)
	w.WriteHeader(http.StatusOK)
	encoder.Encode(resp)
}
