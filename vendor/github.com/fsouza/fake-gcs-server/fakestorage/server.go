// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/internal/backend"
	"github.com/fsouza/fake-gcs-server/internal/checksum"
	"github.com/fsouza/fake-gcs-server/internal/notification"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

const defaultPublicHost = "storage.googleapis.com"

// Server is the fake server.
//
// It provides a fake implementation of the Google Cloud Storage API.
type Server struct {
	backend      backend.Storage
	uploads      sync.Map
	transport    http.RoundTripper
	ts           *httptest.Server
	handler      http.Handler
	options      Options
	externalURL  string
	publicHost   string
	eventManager notification.EventManager
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
//
// Deprecated: use NewServerWithOptions.
func NewServerWithHostPort(objects []Object, host string, port uint16) (*Server, error) {
	return NewServerWithOptions(Options{
		InitialObjects: objects,
		Host:           host,
		Port:           port,
	})
}

type EventManagerOptions = notification.EventManagerOptions

type EventNotificationOptions = notification.EventNotificationOptions

// Options are used to configure the server on creation.
type Options struct {
	InitialObjects []Object
	StorageRoot    string
	Seed           string
	Scheme         string
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
	// https://<bucket>.storage.gcs.127.0.0.1.nip.io:4443>/<object>
	// If unset, the default is "storage.googleapis.com", the XML API
	PublicHost string

	// Optional list of headers to add to the CORS header allowlist
	// An example is "X-Goog-Meta-Uploader", which will allow a
	// custom metadata header named "X-Goog-Meta-Uploader" to be
	// sent through the browser
	AllowedCORSHeaders []string

	// Destination for writing log.
	Writer io.Writer

	// EventOptions contains the events that should be published and the URL
	// of the Google cloud function such events should be published to.
	EventOptions EventManagerOptions

	// Location used for buckets in the server.
	BucketsLocation string

	CertificateLocation string

	PrivateKeyLocation string
}

// NewServerWithOptions creates a new server configured according to the
// provided options.
func NewServerWithOptions(options Options) (*Server, error) {
	s, err := newServer(options)
	if err != nil {
		return nil, err
	}

	allowedHeaders := []string{"Content-Type", "Content-Encoding", "Range", "Content-Range"}
	allowedHeaders = append(allowedHeaders, options.AllowedCORSHeaders...)

	cors := handlers.CORS(
		handlers.AllowedMethods([]string{
			http.MethodHead,
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		}),
		handlers.AllowedHeaders(allowedHeaders),
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowCredentials(),
		handlers.ExposedHeaders([]string{"Location"}),
	)

	s.handler = cors(s.handler)
	if options.Writer != nil {
		s.handler = handlers.LoggingHandler(options.Writer, s.handler)
	}
	s.handler = requestCompressHandler(s.handler)
	s.transport = &muxTransport{handler: s.handler}

	s.eventManager, err = notification.NewPubsubEventManager(options.EventOptions, options.Writer)
	if err != nil {
		return nil, err
	}

	if options.NoListener {
		return s, nil
	}

	s.ts = httptest.NewUnstartedServer(s.handler)
	startFunc := s.ts.StartTLS
	if options.Scheme == "http" {
		startFunc = s.ts.Start
	}

	if options.Port != 0 {
		addr := fmt.Sprintf("%s:%d", options.Host, options.Port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, err
		}
		s.ts.Listener.Close()
		s.ts.Listener = l
	}
	if options.CertificateLocation != "" && options.PrivateKeyLocation != "" {
		cert, err := tls.LoadX509KeyPair(options.CertificateLocation, options.PrivateKeyLocation)
		if err != nil {
			return nil, err
		}
		s.ts.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	startFunc()

	return s, nil
}

func newServer(options Options) (*Server, error) {
	if len(options.InitialObjects) > 0 && options.Seed != "" {
		return nil, errors.New("please provide either a seed directory or a list of initial objects")
	}

	var backendObjects []backend.StreamingObject
	if len(options.InitialObjects) > 0 {
		backendObjects = bufferedObjectsToBackendObjects(options.InitialObjects)
	}

	var backendStorage backend.Storage
	var err error
	if options.StorageRoot != "" {
		backendStorage, err = backend.NewStorageFS(backendObjects, options.StorageRoot)
	} else {
		backendStorage, err = backend.NewStorageMemory(backendObjects)
	}
	if err != nil {
		return nil, err
	}
	publicHost := options.PublicHost
	if publicHost == "" {
		publicHost = defaultPublicHost
	}

	s := Server{
		backend:      backendStorage,
		uploads:      sync.Map{},
		externalURL:  options.ExternalURL,
		publicHost:   publicHost,
		options:      options,
		eventManager: &notification.PubsubEventManager{},
	}
	s.buildMuxer()
	_, err = s.seed()
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func unescapeMuxVars(vars map[string]string) map[string]string {
	m := make(map[string]string)
	for k, v := range vars {
		r, err := url.PathUnescape(v)
		if err == nil {
			m[k] = r
		} else {
			m[k] = v
		}
	}
	return m
}

func (s *Server) buildMuxer() {
	const apiPrefix = "/storage/v1"
	handler := mux.NewRouter().SkipClean(true).UseEncodedPath()

	// healthcheck
	handler.Path("/_internal/healthcheck").Methods(http.MethodGet).HandlerFunc(s.healthcheck)

	routers := []*mux.Router{
		handler.PathPrefix(apiPrefix).Subrouter(),
		handler.MatcherFunc(s.publicHostMatcher).PathPrefix(apiPrefix).Subrouter(),
	}

	for _, r := range routers {
		r.Path("/b").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.listBuckets))
		r.Path("/b/").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.listBuckets))
		r.Path("/b").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.createBucketByPost))
		r.Path("/b/").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.createBucketByPost))
		r.Path("/b/{bucketName}").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.getBucket))
		r.Path("/b/{bucketName}").Methods(http.MethodPatch).HandlerFunc(jsonToHTTPHandler(s.updateBucket))
		r.Path("/b/{bucketName}").Methods(http.MethodPost).Headers("X-HTTP-Method-Override", "PATCH").HandlerFunc(jsonToHTTPHandler(s.updateBucket))
		r.Path("/b/{bucketName}").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteBucket))
		r.Path("/b/{bucketName}/o").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.listObjects))
		r.Path("/b/{bucketName}/o/").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.listObjects))
		r.Path("/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodPatch).HandlerFunc(jsonToHTTPHandler(s.patchObject))
		r.Path("/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodPost).Headers("X-HTTP-Method-Override", "PATCH").HandlerFunc(jsonToHTTPHandler(s.patchObject))
		r.Path("/b/{bucketName}/o/{objectName:.+}/acl").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.listObjectACL))
		r.Path("/b/{bucketName}/o/{objectName:.+}/acl").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.setObjectACL))
		r.Path("/b/{bucketName}/o/{objectName:.+}/acl/{entity}").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteObjectACL))
		r.Path("/b/{bucketName}/o/{objectName:.+}/acl/{entity}").Methods(http.MethodGet).HandlerFunc(jsonToHTTPHandler(s.getObjectACL))
		r.Path("/b/{bucketName}/o/{objectName:.+}/acl/{entity}").Methods(http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.setObjectACL))
		r.Path("/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.getObject)
		r.Path("/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteObject))
		r.Path("/b/{sourceBucket}/o/{sourceObject:.+}/{copyType:rewriteTo|copyTo}/b/{destinationBucket}/o/{destinationObject:.+}").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.rewriteObject))
		r.Path("/b/{bucketName}/o/{destinationObject:.+}/compose").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.composeObject))
		r.Path("/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodPut, http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.updateObject))
	}

	// Internal / update server configuration
	handler.Path("/_internal/config").Methods(http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.updateServerConfig))
	handler.MatcherFunc(s.publicHostMatcher).Path("/_internal/config").Methods(http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.updateServerConfig))
	handler.Path("/_internal/reseed").Methods(http.MethodPut, http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.reseedServer))
	handler.Path("/_internal/delete_all").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.deleteAllFiles))
	// Internal - end

	// XML API
	xmlApiRouters := []*mux.Router{
		handler.Host(fmt.Sprintf("{bucketName}.%s", s.publicHost)).Subrouter(),
		handler.MatcherFunc(s.publicHostMatcher).PathPrefix(`/{bucketName}`).Subrouter(),
	}
	for _, r := range xmlApiRouters {
		r.Path("/").Methods(http.MethodGet).HandlerFunc(xmlToHTTPHandler(s.xmlListObjects))
		r.Path("").Methods(http.MethodGet).HandlerFunc(xmlToHTTPHandler(s.xmlListObjects))
	}

	bucketHost := fmt.Sprintf("{bucketName}.%s", s.publicHost)
	handler.Host(bucketHost).Path("/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.downloadObject)
	handler.Path("/download/storage/v1/b/{bucketName}/o/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.downloadObject)
	handler.Path("/upload/storage/v1/b/{bucketName}/o").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.insertObject))
	handler.Path("/upload/storage/v1/b/{bucketName}/o/").Methods(http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.insertObject))
	handler.Path("/upload/storage/v1/b/{bucketName}/o").Methods(http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.uploadFileContent))
	handler.Path("/upload/storage/v1/b/{bucketName}/o/").Methods(http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.uploadFileContent))
	handler.Path("/upload/storage/v1/b/{bucketName}/o").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteResumableUpload))
	handler.Path("/upload/storage/v1/b/{bucketName}/o/").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteResumableUpload))
	handler.Path("/upload/resumable/{uploadId}").Methods(http.MethodPut, http.MethodPost).HandlerFunc(jsonToHTTPHandler(s.uploadFileContent))

	// Batch endpoint
	handler.MatcherFunc(s.publicHostMatcher).Path("/batch/storage/v1").Methods(http.MethodPost).HandlerFunc(s.handleBatchCall)
	handler.Path("/batch/storage/v1").Methods(http.MethodPost).HandlerFunc(s.handleBatchCall)

	handler.MatcherFunc(s.publicHostMatcher).Path("/{bucketName}/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.downloadObject)
	handler.Host("{bucketName:.+}").Path("/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.downloadObject)

	// Form Uploads
	handler.Host(s.publicHost).Path("/{bucketName}").MatcherFunc(matchFormData).Methods(http.MethodPost, http.MethodPut).HandlerFunc(xmlToHTTPHandler(s.insertFormObject))
	handler.Host(bucketHost).MatcherFunc(matchFormData).Methods(http.MethodPost, http.MethodPut).HandlerFunc(xmlToHTTPHandler(s.insertFormObject))

	// Signed URLs (upload and download)
	handler.MatcherFunc(s.publicHostMatcher).Path("/{bucketName}/{objectName:.+}").Methods(http.MethodPost, http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.insertObject))
	handler.MatcherFunc(s.publicHostMatcher).Path("/{bucketName}/{objectName:.+}").Methods(http.MethodGet, http.MethodHead).HandlerFunc(s.getObject)
	handler.MatcherFunc(s.publicHostMatcher).Path("/{bucketName}/{objectName:.+}").Methods(http.MethodDelete).HandlerFunc(jsonToHTTPHandler(s.deleteObject))
	handler.Host(bucketHost).Path("/{objectName:.+}").Methods(http.MethodPost, http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.insertObject))
	handler.Host("{bucketName:.+}").Path("/{objectName:.+}").Methods(http.MethodPost, http.MethodPut).HandlerFunc(jsonToHTTPHandler(s.insertObject))

	s.handler = handler
}

func (s *Server) seed() ([]backend.StreamingObject, error) {
	if s.options.Seed == "" {
		return nil, nil
	}

	initialObjects, emptyBuckets := generateObjectsFromFiles(s.options.Seed)

	backendObjects := bufferedObjectsToBackendObjects(initialObjects)

	var err error
	if s.options.StorageRoot != "" {
		s.backend, err = backend.NewStorageFS(backendObjects, s.options.StorageRoot)
	} else {
		s.backend, err = backend.NewStorageMemory(backendObjects)
	}
	if err != nil {
		return nil, err
	}

	for _, bucketName := range emptyBuckets {
		s.CreateBucketWithOpts(CreateBucketOpts{Name: bucketName})
	}
	return backendObjects, nil
}

func (s *Server) reseedServer(r *http.Request) jsonResponse {
	backendObjects, err := s.seed()
	if err != nil {
		return errToJsonResponse(err)
	}

	return jsonResponse{data: fromBackendObjects(backendObjects)}
}

func (s *Server) deleteAllFiles(r *http.Request) jsonResponse {
	if err := s.backend.DeleteAllFiles(); err != nil {
		return jsonResponse{
			status:       http.StatusInternalServerError,
			errorMessage: err.Error(),
		}
	}

	return jsonResponse{
		status: http.StatusOK,
		data:   map[string]string{"message": "All files deleted successfully"},
	}
}

func generateObjectsFromFiles(folder string) ([]Object, []string) {
	var objects []Object
	var emptyBuckets []string
	if files, err := os.ReadDir(folder); err == nil {
		for _, f := range files {
			if !f.IsDir() {
				continue
			}
			bucketName := f.Name()
			localBucketPath := filepath.Join(folder, bucketName)

			bucketObjects, err := objectsFromBucket(localBucketPath, bucketName)
			if err != nil {
				continue
			}

			if len(bucketObjects) < 1 {
				emptyBuckets = append(emptyBuckets, bucketName)
			}
			objects = append(objects, bucketObjects...)
		}
	}
	return objects, emptyBuckets
}

func objectsFromBucket(localBucketPath, bucketName string) ([]Object, error) {
	var objects []Object
	err := filepath.Walk(localBucketPath, func(path string, info os.FileInfo, _ error) error {
		if info.Mode().IsRegular() {
			// Rel() should never return error since path always descend from localBucketPath
			relPath, _ := filepath.Rel(localBucketPath, path)
			objectKey := filepath.ToSlash(relPath)
			fileContent, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("could not read file %q: %w", path, err)
			}
			objects = append(objects, Object{
				ObjectAttrs: ObjectAttrs{
					ACL: []storage.ACLRule{
						{
							Entity: "projectOwner-test-project",
							Role:   "OWNER",
						},
					},
					BucketName:  bucketName,
					Name:        objectKey,
					ContentType: mime.TypeByExtension(filepath.Ext(path)),
					Crc32c:      checksum.EncodedCrc32cChecksum(fileContent),
					Md5Hash:     checksum.EncodedMd5Hash(fileContent),
				},
				Content: fileContent,
			})
		}
		return nil
	})
	return objects, err
}

func (s *Server) healthcheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// publicHostMatcher matches incoming requests against the currently specified server publicHost.
func (s *Server) publicHostMatcher(r *http.Request, rm *mux.RouteMatch) bool {
	if strings.Contains(s.publicHost, ":") || !strings.Contains(r.Host, ":") {
		return r.Host == s.publicHost
	}
	idx := strings.IndexByte(r.Host, ':')
	return r.Host[:idx] == s.publicHost
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
	return fmt.Sprintf("%s://%s", s.scheme(), s.publicHost)
}

func (s *Server) Backend() backend.Storage {
	return s.backend
}

func (s *Server) scheme() string {
	if s.options.Scheme == "http" {
		return "http"
	}
	return "https"
}

// HTTPClient returns an HTTP client configured to talk to the server.
func (s *Server) HTTPClient() *http.Client {
	return &http.Client{Transport: s.transport}
}

// HTTPHandler returns an HTTP handler that behaves like GCS.
func (s *Server) HTTPHandler() http.Handler {
	return s.handler
}

// Client returns a GCS client configured to talk to the server.
func (s *Server) Client() *storage.Client {
	client, err := storage.NewClient(context.Background(), option.WithHTTPClient(s.HTTPClient()), option.WithCredentials(&google.Credentials{}))
	if err != nil {
		panic(err)
	}
	return client
}

func (s *Server) handleBatchCall(w http.ResponseWriter, r *http.Request) {
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid Content-Type header", http.StatusBadRequest)
		return
	}

	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	defer mw.Close()
	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())

	w.WriteHeader(http.StatusOK)
	part, err := reader.NextPart()
	for ; err == nil; part, err = reader.NextPart() {
		contentID := part.Header.Get("Content-ID")
		if contentID == "" {
			// missing content ID, skip
			continue
		}

		partHeaders := textproto.MIMEHeader{}
		partHeaders.Set("Content-Type", "application/http")
		partHeaders.Set("Content-ID", strings.Replace(contentID, "<", "<response-", 1))
		partWriter, err := mw.CreatePart(partHeaders)
		if err != nil {
			continue
		}

		partResponseWriter := httptest.NewRecorder()
		if part.Header.Get("Content-Type") != "application/http" {
			http.Error(partResponseWriter, "invalid Content-Type header", http.StatusBadRequest)
			writeMultipartResponse(partResponseWriter.Result(), partWriter, contentID)
			continue
		}

		content, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			http.Error(partResponseWriter, "unable to process request", http.StatusBadRequest)
			writeMultipartResponse(partResponseWriter.Result(), partWriter, contentID)
			continue
		}

		partRequest, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(content)))
		if err != nil {
			http.Error(partResponseWriter, "unable to process request", http.StatusBadRequest)
			writeMultipartResponse(partResponseWriter.Result(), partWriter, contentID)
			continue
		}

		s.handler.ServeHTTP(partResponseWriter, partRequest)
		writeMultipartResponse(partResponseWriter.Result(), partWriter, contentID)
	}
	mw.Close()

	_, err = b.WriteTo(w)
	if err != nil {
		http.Error(w, "unable to process request", http.StatusBadRequest)
	}
}

func writeMultipartResponse(r *http.Response, w io.Writer, contentId string) {
	dump, err := httputil.DumpResponse(r, true)
	if err != nil {
		fmt.Fprintf(w, "Content-Type: text/plain; charset=utf-8\r\nContent-ID: %s\r\nContent-Length: 0\r\n\r\nHTTP/1.1 500 Internal Server Error", contentId)
		return
	}
	w.Write(dump)
}

func requestCompressHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("content-encoding") == "gzip" {
			gzipReader, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			r.Body = gzipReader
		}
		h.ServeHTTP(w, r)
	})
}

func matchFormData(r *http.Request, _ *mux.RouteMatch) bool {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return contentType == "multipart/form-data"
}
