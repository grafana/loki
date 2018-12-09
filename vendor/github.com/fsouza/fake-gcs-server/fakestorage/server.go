// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/internal/backend"
	"github.com/gorilla/mux"
	"google.golang.org/api/option"
)

// Server is the fake server.
//
// It provides a fake implementation of the Google Cloud Storage API.
type Server struct {
	backend   backend.Storage
	uploads   map[string]Object
	transport *http.Transport
	ts        *httptest.Server
	mux       *mux.Router
	mtx       sync.RWMutex
}

// NewServer creates a new instance of the server, pre-loaded with the given
// objects.
func NewServer(objects []Object) *Server {
	s, _ := NewServerWithOptions(Options{
		InitialObjects: objects,
	})
	return s
}

// NewServerWithHostPort creates a new server that listens on a custom host and port
func NewServerWithHostPort(objects []Object, host string, port uint16) (*Server, error) {
	return NewServerWithOptions(Options{
		InitialObjects: objects,
		Host:           host,
		Port:           port,
	})
}

// Options are used to configure the server on creation
type Options struct {
	InitialObjects []Object
	Host           string
	Port           uint16
	StorageRoot    string
}

// NewServerWithOptions creates a new server with custom options
func NewServerWithOptions(options Options) (*Server, error) {
	s, err := newUnstartedServer(options.InitialObjects, options.StorageRoot)
	if err != nil {
		return nil, err
	}
	var addr string
	if options.Port != 0 {
		addr = fmt.Sprintf("%s:%d", options.Host, options.Port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		s.ts.Listener.Close()
		s.ts.Listener = l
		s.ts.StartTLS()
		s.setTransportToAddr(addr)
	} else {
		s.setTransportToAddr(s.ts.Listener.Addr().String())
		s.ts.StartTLS()
	}
	return s, nil
}

func newUnstartedServer(objects []Object, storageRoot string) (*Server, error) {
	backendObjects := toBackendObjects(objects)
	var backendStorage backend.Storage
	var err error
	if storageRoot != "" {
		backendStorage, err = backend.NewStorageFS(backendObjects, storageRoot)
	} else {
		backendStorage = backend.NewStorageMemory(backendObjects)
	}
	if err != nil {
		return nil, err
	}
	s := Server{
		backend: backendStorage,
		uploads: make(map[string]Object),
	}
	s.buildMuxer()
	s.ts = httptest.NewUnstartedServer(s.mux)
	return &s, nil
}

func (s *Server) setTransportToAddr(addr string) {
	tlsConfig := tls.Config{InsecureSkipVerify: true}
	s.transport = &http.Transport{
		TLSClientConfig: &tlsConfig,
		DialTLS: func(string, string) (net.Conn, error) {
			return tls.Dial("tcp", addr, &tlsConfig)
		},
	}
}

func (s *Server) buildMuxer() {
	s.mux = mux.NewRouter()
	s.mux.Host("storage.googleapis.com").Path("/{bucketName}/{objectName:.+}").Methods("GET").HandlerFunc(s.downloadObject)
	r := s.mux.PathPrefix("/storage/v1").Subrouter()
	r.Path("/b").Methods("GET").HandlerFunc(s.listBuckets)
	r.Path("/b/{bucketName}").Methods("GET").HandlerFunc(s.getBucket)
	r.Path("/b/{bucketName}/o").Methods("GET").HandlerFunc(s.listObjects)
	r.Path("/b/{bucketName}/o").Methods("POST").HandlerFunc(s.insertObject)
	r.Path("/b/{bucketName}/o/{objectName:.+}").Methods("GET").HandlerFunc(s.getObject)
	r.Path("/b/{bucketName}/o/{objectName:.+}").Methods("DELETE").HandlerFunc(s.deleteObject)
	r.Path("/b/{sourceBucket}/o/{sourceObject:.+}/rewriteTo/b/{destinationBucket}/o/{destinationObject:.+}").HandlerFunc(s.rewriteObject)
	s.mux.Path("/upload/storage/v1/b/{bucketName}/o").Methods("POST").HandlerFunc(s.insertObject)
	s.mux.Path("/upload/resumable/{uploadId}").Methods("PUT", "POST").HandlerFunc(s.uploadFileContent)
}

// Stop stops the server, closing all connections.
func (s *Server) Stop() {
	s.transport.CloseIdleConnections()
	s.ts.Close()
}

// URL returns the server URL.
func (s *Server) URL() string {
	return s.ts.URL
}

// Client returns a GCS client configured to talk to the server.
func (s *Server) Client() *storage.Client {
	opt := option.WithHTTPClient(&http.Client{Transport: s.transport})
	client, _ := storage.NewClient(context.Background(), opt)
	return client
}
