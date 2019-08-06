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
	backend     backend.Storage
	uploads     sync.Map
	transport   http.RoundTripper
	ts          *httptest.Server
	mux         *mux.Router
	externalURL string
	publicHost  string
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
	StorageRoot    string
	Host           string
	Port           uint16

	// when set to true, the server will not actually start a TCP listener,
	// client requests will get processed by an internal mocked transport.
	NoListener bool

	// Optional external URL, such as https://gcs.127.0.0.1.nip.io:4443
	// Returned in the Location header for resumable uploads
	// The "real" value is https://www.googleapis.com, the JSON API
	// The default is whatever the server is bound to, such as https://0.0.0.0:4443
	ExternalURL string

	// Optional URL for public access
	// An example is "storage.gcs.127.0.0.1.nip.io:4443", which will configure
	// the server to serve objects at:
	// https://storage.gcs.127.0.0.1.nip.io:4443/<bucket>/<object>
	// https://<bucket>.storage.gcs.127.0.0.1.nip.io:4443>/<bucket>/<object>
	// If unset, the default is "storage.googleapis.com", the XML API
	PublicHost string
}

// NewServerWithOptions creates a new server with custom options
func NewServerWithOptions(options Options) (*Server, error) {
	s, err := newServer(options.InitialObjects, options.StorageRoot, options.ExternalURL, options.PublicHost)
	if err != nil {
		return nil, err
	}
	if options.NoListener {
		s.setTransportToMux()
		return s, nil
	}

	s.ts = httptest.NewUnstartedServer(s.mux)
	if options.Port != 0 {
		addr := fmt.Sprintf("%s:%d", options.Host, options.Port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		s.ts.Listener.Close()
		s.ts.Listener = l
		s.ts.StartTLS()
	} else {
		s.ts.StartTLS()
	}
	s.setTransportToAddr(s.ts.Listener.Addr().String())
	return s, nil
}

func newServer(objects []Object, storageRoot, externalURL, publicHost string) (*Server, error) {
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
	if publicHost == "" {
		publicHost = "storage.googleapis.com"
	}
	s := Server{
		backend:     backendStorage,
		uploads:     sync.Map{},
		externalURL: externalURL,
		publicHost:  publicHost,
	}
	s.buildMuxer()
	return &s, nil
}

func (s *Server) setTransportToAddr(addr string) {
	// #nosec
	tlsConfig := tls.Config{InsecureSkipVerify: true}
	s.transport = &http.Transport{
		TLSClientConfig: &tlsConfig,
		DialTLS: func(string, string) (net.Conn, error) {
			return tls.Dial("tcp", addr, &tlsConfig)
		},
	}
}

func (s *Server) setTransportToMux() {
	s.transport = &muxTransport{router: s.mux}
}

func (s *Server) buildMuxer() {
	s.mux = mux.NewRouter()
	s.mux.Host(s.publicHost).Path("/{bucketName}/{objectName:.+}").Methods("GET", "HEAD").HandlerFunc(s.downloadObject)
	bucketHost := fmt.Sprintf("{bucketName}.%s", s.publicHost)
	s.mux.Host(bucketHost).Path("/{objectName:.+}").Methods("GET", "HEAD").HandlerFunc(s.downloadObject)
	r := s.mux.PathPrefix("/storage/v1").Subrouter()
	r.Path("/b").Methods("GET").HandlerFunc(s.listBuckets)
	r.Path("/b").Methods("POST").HandlerFunc(s.createBucketByPost)
	r.Path("/b/{bucketName}").Methods("GET").HandlerFunc(s.getBucket)
	r.Path("/b/{bucketName}/o").Methods("GET").HandlerFunc(s.listObjects)
	r.Path("/b/{bucketName}/o").Methods("POST").HandlerFunc(s.insertObject)
	r.Path("/b/{bucketName}/o/{objectName:.+}").Methods("GET").HandlerFunc(s.getObject)
	r.Path("/b/{bucketName}/o/{objectName:.+}").Methods("DELETE").HandlerFunc(s.deleteObject)
	r.Path("/b/{sourceBucket}/o/{sourceObject:.+}/rewriteTo/b/{destinationBucket}/o/{destinationObject:.+}").HandlerFunc(s.rewriteObject)
	s.mux.Path("/download/storage/v1/b/{bucketName}/o/{objectName:.+}").Methods("GET").HandlerFunc(s.downloadObject)
	s.mux.Path("/upload/storage/v1/b/{bucketName}/o").Methods("POST").HandlerFunc(s.insertObject)
	s.mux.Path("/upload/resumable/{uploadId}").Methods("PUT", "POST").HandlerFunc(s.uploadFileContent)
}

// Stop stops the server, closing all connections.
func (s *Server) Stop() {
	if s.ts != nil {
		if transport, ok := s.transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
		s.ts.Close()
	}
}

// URL returns the server URL.
func (s *Server) URL() string {
	if s.externalURL != "" {
		return s.externalURL
	}
	if s.ts != nil {
		return s.ts.URL
	}
	return ""
}

// PublicURL returns the server's public download URL.
func (s *Server) PublicURL() string {
	return fmt.Sprintf("https://%s", s.publicHost)
}

// HTTPClient returns an HTTP client configured to talk to the server.
func (s *Server) HTTPClient() *http.Client {
	return &http.Client{Transport: s.transport}
}

// Client returns a GCS client configured to talk to the server.
func (s *Server) Client() *storage.Client {
	opt := option.WithHTTPClient(s.HTTPClient())
	client, _ := storage.NewClient(context.Background(), opt)
	return client
}
