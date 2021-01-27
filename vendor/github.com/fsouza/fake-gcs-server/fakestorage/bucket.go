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
	s.mtx.Lock()
	defer s.mtx.Unlock()
	err := s.backend.CreateBucket(name)
	if err != nil {
		panic(err)
	}
}

func (s *Server) listBuckets(w http.ResponseWriter, r *http.Request) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

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
	s.mtx.RLock()
	defer s.mtx.RUnlock()
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
