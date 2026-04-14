// Copyright 2024 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package remote implements bindings for Prometheus Remote APIs.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/snappy"
	"google.golang.org/protobuf/proto"

	"github.com/prometheus/client_golang/exp/internal/github.com/efficientgo/core/backoff"
)

// BackoffConfig configures exponential backoff with jitter for retry operations.
type BackoffConfig struct {
	Min        time.Duration `yaml:"min_period"`  // Start backoff at this level
	Max        time.Duration `yaml:"max_period"`  // Increase exponentially to this level
	MaxRetries int           `yaml:"max_retries"` // Give up after this many; zero means infinite retries
}

// API is a client for Prometheus Remote Protocols.
// NOTE(bwplotka): Only https://prometheus.io/docs/specs/remote_write_spec_2_0/ is currently implemented,
// read protocols to be implemented if there will be a demand.
type API struct {
	baseURL *url.URL

	opts    apiOpts
	bufPool sync.Pool
}

// APIOption represents a remote API option.
type APIOption func(o *apiOpts) error

// RetryCallback is called each time Write() retries a request.
// err is the error that caused the retry.
type RetryCallback func(err error)

// TODO(bwplotka): Add "too old sample" handling one day.
type apiOpts struct {
	logger           *slog.Logger
	client           *http.Client
	backoffConfig    BackoffConfig
	compression      Compression
	path             string
	retryOnRateLimit bool
}

var defaultAPIOpts = &apiOpts{
	backoffConfig: BackoffConfig{
		Min:        1 * time.Second,
		Max:        10 * time.Second,
		MaxRetries: 10,
	},
	client:           http.DefaultClient,
	retryOnRateLimit: true,
	compression:      SnappyBlockCompression,
	path:             "api/v1/write",
}

// WithAPILogger returns APIOption that allows providing slog logger.
// By default, nothing is logged.
func WithAPILogger(logger *slog.Logger) APIOption {
	return func(o *apiOpts) error {
		o.logger = logger
		return nil
	}
}

// WithAPIHTTPClient returns APIOption that allows providing http client.
func WithAPIHTTPClient(client *http.Client) APIOption {
	return func(o *apiOpts) error {
		o.client = client
		return nil
	}
}

// WithAPIPath returns APIOption that allows providing path to send remote write requests to.
func WithAPIPath(path string) APIOption {
	return func(o *apiOpts) error {
		o.path = path
		return nil
	}
}

// WithAPINoRetryOnRateLimit returns APIOption that disables retrying on rate limit status code.
func WithAPINoRetryOnRateLimit() APIOption {
	return func(o *apiOpts) error {
		o.retryOnRateLimit = false
		return nil
	}
}

// WithAPIBackoff returns APIOption that allows configuring backoff.
// By default, exponential backoff with jitter is used (see defaultAPIOpts).
func WithAPIBackoff(cfg BackoffConfig) APIOption {
	return func(o *apiOpts) error {
		o.backoffConfig = cfg
		return nil
	}
}

type nopSlogHandler struct{}

func (n nopSlogHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (n nopSlogHandler) Handle(context.Context, slog.Record) error { return nil }
func (n nopSlogHandler) WithAttrs([]slog.Attr) slog.Handler        { return n }
func (n nopSlogHandler) WithGroup(string) slog.Handler             { return n }

// NewAPI returns a new API for the clients of Remote Write Protocol.
func NewAPI(baseURL string, opts ...APIOption) (*API, error) {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	o := *defaultAPIOpts
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			return nil, err
		}
	}

	if o.logger == nil {
		o.logger = slog.New(nopSlogHandler{})
	}

	parsedURL.Path = path.Join(parsedURL.Path, o.path)

	api := &API{
		opts:    o,
		baseURL: parsedURL,
		bufPool: sync.Pool{
			New: func() any {
				b := make([]byte, 0, 1024*16) // Initial capacity of 16KB.
				return &b
			},
		},
	}
	return api, nil
}

type retryableError struct {
	error
	retryAfter time.Duration
}

func (r retryableError) RetryAfter() time.Duration {
	return r.retryAfter
}

// WriteOption represents an option for Write method.
type WriteOption func(o *writeOpts)

type writeOpts struct {
	retryCallback RetryCallback
}

// WithWriteRetryCallback sets a retry callback for this Write request.
// The callback is invoked each time the request is retried.
func WithWriteRetryCallback(callback RetryCallback) WriteOption {
	return func(o *writeOpts) {
		o.retryCallback = callback
	}
}

type vtProtoEnabled interface {
	SizeVT() int
	MarshalToSizedBufferVT(dAtA []byte) (int, error)
}

type gogoProtoEnabled interface {
	Size() (n int)
	MarshalToSizedBuffer(dAtA []byte) (n int, err error)
}

// Write writes given, non-empty, protobuf message to a remote storage.
//
// Depending on serialization methods,
//   - https://github.com/planetscale/vtprotobuf methods will be used if your msg
//     supports those (e.g. SizeVT() and MarshalToSizedBufferVT(...)), for efficiency
//   - Otherwise https://github.com/gogo/protobuf methods (e.g. Size() and MarshalToSizedBuffer(...))
//     will be used
//   - If neither is supported, it will marshaled using generic google.golang.org/protobuf methods and
//     error out on unknown scheme.
func (r *API) Write(ctx context.Context, msgType WriteMessageType, msg any, opts ...WriteOption) (_ WriteResponseStats, err error) {
	// Parse write options.
	var writeOpts writeOpts
	for _, opt := range opts {
		opt(&writeOpts)
	}

	buf := r.bufPool.Get().(*[]byte)

	if err := msgType.Validate(); err != nil {
		return WriteResponseStats{}, err
	}

	// Encode the payload.
	switch m := msg.(type) {
	case vtProtoEnabled:
		// Use optimized vtprotobuf if supported.
		size := m.SizeVT()
		if cap(*buf) < size {
			*buf = make([]byte, size)
		} else {
			*buf = (*buf)[:size]
		}

		if _, err := m.MarshalToSizedBufferVT(*buf); err != nil {
			return WriteResponseStats{}, fmt.Errorf("encoding request %w", err)
		}
	case gogoProtoEnabled:
		// Gogo proto if supported.
		size := m.Size()
		if cap(*buf) < size {
			*buf = make([]byte, size)
		} else {
			*buf = (*buf)[:size]
		}

		if _, err := m.MarshalToSizedBuffer(*buf); err != nil {
			return WriteResponseStats{}, fmt.Errorf("encoding request %w", err)
		}
	case proto.Message:
		// Generic proto.
		*buf, err = (proto.MarshalOptions{}).MarshalAppend(*buf, m)
		if err != nil {
			return WriteResponseStats{}, fmt.Errorf("encoding request %w", err)
		}
	default:
		return WriteResponseStats{}, fmt.Errorf("unknown message type %T", m)
	}

	comprBuf := r.bufPool.Get().(*[]byte)
	payload, err := compressPayload(comprBuf, r.opts.compression, *buf)
	if err != nil {
		return WriteResponseStats{}, fmt.Errorf("compressing %w", err)
	}
	r.bufPool.Put(buf)
	defer r.bufPool.Put(comprBuf)

	// Since we retry writes we need to track the total amount of accepted data
	// across the various attempts.
	accumulatedStats := WriteResponseStats{}

	b := backoff.New(ctx, backoff.Config{
		Min:        r.opts.backoffConfig.Min,
		Max:        r.opts.backoffConfig.Max,
		MaxRetries: r.opts.backoffConfig.MaxRetries,
	})
	for {
		rs, err := r.attemptWrite(ctx, r.opts.compression, msgType, payload, b.NumRetries())
		accumulatedStats.Add(rs)
		if err == nil {
			// Check the case mentioned in PRW 2.0.
			// https://prometheus.io/docs/specs/remote_write_spec_2_0/#required-written-response-headers.
			if msgType == WriteV2MessageType && !accumulatedStats.confirmed && accumulatedStats.NoDataWritten() {
				// TODO(bwplotka): Allow users to disable this check or provide their stats for us to know if it's empty.
				return accumulatedStats, fmt.Errorf("sent v2 request; "+
					"got 2xx, but PRW 2.0 response header statistics indicate %v samples, %v histograms "+
					"and %v exemplars were accepted; assuming failure e.g. the target only supports "+
					"PRW 1.0 prometheus.WriteRequest, but does not check the Content-Type header correctly",
					accumulatedStats.Samples, accumulatedStats.Histograms, accumulatedStats.Exemplars,
				)
			}
			// Success!
			// TODO(bwplotka): Debug log with retry summary?
			return accumulatedStats, nil
		}

		var retryableErr retryableError
		if !errors.As(err, &retryableErr) {
			return accumulatedStats, err
		}

		if !b.Ongoing() {
			return accumulatedStats, err
		}

		backoffDelay := b.NextDelay() + retryableErr.RetryAfter()

		// Invoke retry callback if provided.
		if writeOpts.retryCallback != nil {
			writeOpts.retryCallback(retryableErr.error)
		}

		r.opts.logger.Error("failed to send remote write request; retrying after backoff", "err", err, "backoff", backoffDelay)
		select {
		case <-ctx.Done():
			return WriteResponseStats{}, ctx.Err()
		case <-time.After(backoffDelay):
			// Retry.
		}
	}
}

func compressPayload(tmpbuf *[]byte, enc Compression, inp []byte) (compressed []byte, _ error) {
	switch enc {
	case SnappyBlockCompression:
		if cap(*tmpbuf) < snappy.MaxEncodedLen(len(inp)) {
			*tmpbuf = make([]byte, snappy.MaxEncodedLen(len(inp)))
		} else {
			*tmpbuf = (*tmpbuf)[:snappy.MaxEncodedLen(len(inp))]
		}

		compressed = snappy.Encode(*tmpbuf, inp)
		return compressed, nil
	default:
		return compressed, fmt.Errorf("unknown compression scheme [%v]", enc)
	}
}

func (r *API) attemptWrite(ctx context.Context, compr Compression, msgType WriteMessageType, payload []byte, attempt int) (WriteResponseStats, error) {
	req, err := http.NewRequest(http.MethodPost, r.baseURL.String(), bytes.NewReader(payload))
	if err != nil {
		// Errors from NewRequest are from unparsable URLs, so are not
		// recoverable.
		return WriteResponseStats{}, err
	}

	req.Header.Add("Content-Encoding", string(compr))
	req.Header.Set("Content-Type", contentTypeHeader(msgType))
	if msgType == WriteV1MessageType {
		// Compatibility mode for 1.0.
		req.Header.Set(versionHeader, version1HeaderValue)
	} else {
		req.Header.Set(versionHeader, version20HeaderValue)
	}

	if attempt > 0 {
		req.Header.Set("Retry-Attempt", strconv.Itoa(attempt))
	}

	resp, err := r.opts.client.Do(req.WithContext(ctx))
	if err != nil {
		// Errors from Client.Do are likely network errors, so recoverable.
		return WriteResponseStats{}, retryableError{err, 0}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return WriteResponseStats{}, fmt.Errorf("reading response body: %w", err)
	}

	rs := WriteResponseStats{}
	if msgType == WriteV2MessageType {
		rs, err = parseWriteResponseStats(resp)
		if err != nil {
			r.opts.logger.Warn("parsing rw write statistics failed; partial or no stats", "err", err)
		}
	}

	if resp.StatusCode/100 == 2 {
		return rs, nil
	}

	err = fmt.Errorf("server returned HTTP status %s: %s", resp.Status, body)
	if resp.StatusCode/100 == 5 ||
		(r.opts.retryOnRateLimit && resp.StatusCode == http.StatusTooManyRequests) {
		return rs, retryableError{err, retryAfterDuration(resp.Header.Get("Retry-After"))}
	}
	return rs, err
}

// retryAfterDuration returns the duration for the Retry-After header. In case of any errors, it
// returns 0 as if the header was never supplied.
func retryAfterDuration(t string) time.Duration {
	parsedDuration, err := time.Parse(http.TimeFormat, t)
	if err == nil {
		return time.Until(parsedDuration)
	}
	// The duration can be in seconds.
	d, err := strconv.Atoi(t)
	if err != nil {
		return 0
	}
	return time.Duration(d) * time.Second
}

// writeStorage represents the storage for RemoteWriteHandler.
// This interface is intentionally private due its experimental state.
type writeStorage interface {
	// Store stores remote write metrics encoded in the given WriteContentType.
	// Provided http.Request contains the encoded bytes in the req.Body with all the HTTP information,
	// except "Content-Type" header which is provided in a separate, validated ctype.
	//
	// Other headers might be trimmed, depending on the configured middlewares
	// e.g. a default SnappyMiddleware trims "Content-Encoding" and ensures that
	// encoded body bytes are already decompressed.
	Store(req *http.Request, msgType WriteMessageType) (_ *WriteResponse, _ error)
}

type writeHandler struct {
	store                writeStorage
	acceptedMessageTypes MessageTypes
	opts                 writeHandlerOpts
}

type writeHandlerOpts struct {
	logger      *slog.Logger
	middlewares []func(http.Handler) http.Handler
}

// WriteHandlerOption represents an option for the write handler.
type WriteHandlerOption func(o *writeHandlerOpts)

// WithWriteHandlerLogger returns WriteHandlerOption that allows providing slog logger.
// By default, nothing is logged.
func WithWriteHandlerLogger(logger *slog.Logger) WriteHandlerOption {
	return func(o *writeHandlerOpts) {
		o.logger = logger
	}
}

// WithWriteHandlerMiddlewares returns WriteHandlerOption that allows providing middlewares.
// Multiple middlewares can be provided and will be applied in the order they are passed.
// This option replaces the default middlewares (SnappyDecompressorMiddleware), so if
// you want to have handler that works with the default Remote Write 2.0 protocol,
// SnappyDecompressorMiddleware (or any other decompression middleware) needs to be added explicitly.
func WithWriteHandlerMiddlewares(middlewares ...func(http.Handler) http.Handler) WriteHandlerOption {
	return func(o *writeHandlerOpts) {
		o.middlewares = middlewares
	}
}

// maxDecodedSize limits the maximum allowed bytes of decompressed snappy payloads.
// This protects against maliciously crafted payloads that could cause excessive memory
// allocation and potentially lead to out-of-memory (OOM) conditions.
// All usual payloads should be much smaller than this limit and pass without any problems.
const maxDecodedSize = 32 * 1024 * 1024

// SnappyDecodeMiddleware returns a middleware that checks if the request body is snappy-encoded and decompresses it.
// If the request body is not snappy-encoded, it returns an error.
// Used by default in NewHandler.
func SnappyDecodeMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	bufPool := sync.Pool{
		New: func() any {
			return bytes.NewBuffer(nil)
		},
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enc := r.Header.Get("Content-Encoding")
			if enc != "" && enc != string(SnappyBlockCompression) {
				err := fmt.Errorf("%v encoding (compression) is not accepted by this server; only %v is acceptable", enc, SnappyBlockCompression)
				logger.Error("Error decoding remote write request", "err", err)
				http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
				return
			}

			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()
			defer bufPool.Put(buf)

			bodyBytes, err := io.ReadAll(io.TeeReader(r.Body, buf))
			if err != nil {
				logger.Error("Error reading request body", "err", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			decodedSize, err := snappy.DecodedLen(bodyBytes)
			if err != nil {
				logger.Error("Error snappy decoding request body length", "err", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if decodedSize > maxDecodedSize {
				logger.Error("Snappy decoded size exceeds the limit", "sizeBytes", decodedSize, "limitBytes", maxDecodedSize)
				http.Error(w, fmt.Sprintf("decoded size exceeds the %v bytes limit", maxDecodedSize), http.StatusBadRequest)
				return
			}

			decompressed, err := snappy.Decode(nil, bodyBytes)
			if err != nil {
				// TODO(bwplotka): Add more context to responded error?
				logger.Error("Error snappy decoding remote write request", "err", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			// Replace the body with decompressed data and remove Content-Encoding header.
			r.Body = io.NopCloser(bytes.NewReader(decompressed))
			r.Header.Del("Content-Encoding")
			next.ServeHTTP(w, r)
		})
	}
}

// NewWriteHandler returns an HTTP handler that can receive the Remote Write 1.0 or Remote Write 2.0
// (https://prometheus.io/docs/specs/remote_write_spec_2_0/) protocol.
func NewWriteHandler(store writeStorage, acceptedMessageTypes MessageTypes, opts ...WriteHandlerOption) http.Handler {
	o := writeHandlerOpts{
		logger:      slog.New(nopSlogHandler{}),
		middlewares: []func(http.Handler) http.Handler{SnappyDecodeMiddleware(slog.New(nopSlogHandler{}))},
	}
	for _, opt := range opts {
		opt(&o)
	}

	h := &writeHandler{
		opts:                 o,
		store:                store,
		acceptedMessageTypes: acceptedMessageTypes,
	}

	// Apply all middlewares in order
	var handler http.Handler = h
	for i := len(o.middlewares) - 1; i >= 0; i-- {
		handler = o.middlewares[i](handler)
	}
	return handler
}

// ParseProtoMsg parses the content-type header and returns the proto message type.
//
// The expected content-type will be of the form,
//   - `application/x-protobuf;proto=io.prometheus.write.v2.Request` which will be treated as RW2.0 request,
//   - `application/x-protobuf;proto=prometheus.WriteRequest` which will be treated as RW1.0 request,
//   - `application/x-protobuf` which will be treated as RW1.0 request.
//
// If the content-type is not of the above forms, it will return an error.
func ParseProtoMsg(contentType string) (WriteMessageType, error) {
	contentType = strings.TrimSpace(contentType)

	parts := strings.Split(contentType, ";")
	if parts[0] != appProtoContentType {
		return "", fmt.Errorf("expected %v as the first (media) part, got %v content-type", appProtoContentType, contentType)
	}
	// Parse potential https://www.rfc-editor.org/rfc/rfc9110#parameter
	for _, p := range parts[1:] {
		pair := strings.Split(p, "=")
		if len(pair) != 2 {
			return "", fmt.Errorf("as per https://www.rfc-editor.org/rfc/rfc9110#parameter expected parameters to be key-values, got %v in %v content-type", p, contentType)
		}
		if pair[0] == "proto" {
			ret := WriteMessageType(pair[1])
			if err := ret.Validate(); err != nil {
				return "", fmt.Errorf("got %v content type; %w", contentType, err)
			}
			return ret, nil
		}
	}
	// No "proto=" parameter, assuming v1.
	return WriteV1MessageType, nil
}

func (h *writeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = appProtoContentType
	}

	msgType, err := ParseProtoMsg(contentType)
	if err != nil {
		h.opts.logger.Error("Error decoding remote write request", "err", err)
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	if !h.acceptedMessageTypes.Contains(msgType) {
		err := fmt.Errorf("%v protobuf message is not accepted by this server; only accepts %v", msgType, h.acceptedMessageTypes.String())
		h.opts.logger.Error("Unaccepted message type", "msgType", msgType, "err", err)
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	writeResponse, storeErr := h.store.Store(r, msgType)
	if writeResponse == nil {
		// User could forget to return write response; in this case we assume 0 samples
		// were written.
		writeResponse = NewWriteResponse()
	}

	// Set any necessary response headers.
	writeResponse.writeHeaders(msgType, w)

	if storeErr != nil {
		if writeResponse.statusCode == 0 {
			writeResponse.SetStatusCode(http.StatusInternalServerError)
		}
		if writeResponse.statusCode/100 == 5 { // 5xx
			h.opts.logger.Error("Error while storing the remote write request", "err", storeErr)
		}
		http.Error(w, storeErr.Error(), writeResponse.statusCode)
		return
	}
	w.WriteHeader(writeResponse.statusCode)
}
