package gcp

import (
	"context"
	"crypto/tls"
	"flag"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	google_http "google.golang.org/api/transport/http"
	amnet "k8s.io/apimachinery/pkg/util/net"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
)

type ClientFactory func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error)

type GCSObjectClient struct {
	cfg GCSConfig

	defaultBucket *storage.BucketHandle
	getsBuckets   *storage.BucketHandle
}

// GCSConfig is config for the GCS Chunk Client.
type GCSConfig struct {
	BucketName       string         `yaml:"bucket_name"`
	ServiceAccount   flagext.Secret `yaml:"service_account"`
	ChunkBufferSize  int            `yaml:"chunk_buffer_size"`
	RequestTimeout   time.Duration  `yaml:"request_timeout"`
	EnableOpenCensus bool           `yaml:"enable_opencensus"`
	EnableHTTP2      bool           `yaml:"enable_http2"`

	// TODO(dannyk): remove this and disable GCS client retries; move a layer higher instead.
	EnableRetries bool `yaml:"enable_retries"`

	Insecure bool `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *GCSConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *GCSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"gcs.bucketname", "", "Name of GCS bucket. Please refer to https://cloud.google.com/docs/authentication/production for more information about how to configure authentication.")
	f.Var(&cfg.ServiceAccount, prefix+"gcs.service-account", "Service account key content in JSON format, refer to https://cloud.google.com/iam/docs/creating-managing-service-account-keys for creation.")
	f.IntVar(&cfg.ChunkBufferSize, prefix+"gcs.chunk-buffer-size", 0, "The size of the buffer that GCS client for each PUT request. 0 to disable buffering.")
	f.DurationVar(&cfg.RequestTimeout, prefix+"gcs.request-timeout", 0, "The duration after which the requests to GCS should be timed out.")
	f.BoolVar(&cfg.EnableOpenCensus, prefix+"gcs.enable-opencensus", true, "Enable OpenCensus (OC) instrumentation for all requests.")
	f.BoolVar(&cfg.EnableHTTP2, prefix+"gcs.enable-http2", true, "Enable HTTP2 connections.")
	f.BoolVar(&cfg.EnableRetries, prefix+"gcs.enable-retries", true, "Enable automatic retries of failed idempotent requests.")
}

// NewGCSObjectClient makes a new chunk.Client that writes chunks to GCS.
func NewGCSObjectClient(ctx context.Context, cfg GCSConfig, hedgingCfg hedging.Config) (*GCSObjectClient, error) {
	return newGCSObjectClient(ctx, cfg, hedgingCfg, storage.NewClient)
}

func newGCSObjectClient(ctx context.Context, cfg GCSConfig, hedgingCfg hedging.Config, clientFactory ClientFactory) (*GCSObjectClient, error) {
	// Disabling http2 and hedging is not allowed for POST/LIST/DELETE requests.
	// This is because there's no benefit for these requests.
	// Those requests are handled by the default bucket handle.
	bucket, err := newBucketHandle(ctx, cfg, hedgingCfg, true, false, clientFactory)
	if err != nil {
		return nil, err
	}
	getsBucket, err := newBucketHandle(ctx, cfg, hedgingCfg, cfg.EnableHTTP2, true, clientFactory)
	if err != nil {
		return nil, err
	}
	return &GCSObjectClient{
		cfg:           cfg,
		defaultBucket: bucket,
		getsBuckets:   getsBucket,
	}, nil
}

func newBucketHandle(ctx context.Context, cfg GCSConfig, hedgingCfg hedging.Config, enableHTTP2, hedging bool, clientFactory ClientFactory) (*storage.BucketHandle, error) {
	var opts []option.ClientOption
	transport, err := gcsTransport(ctx, storage.ScopeReadWrite, cfg.Insecure, enableHTTP2, cfg.ServiceAccount)
	if err != nil {
		return nil, err
	}
	httpClient := gcsInstrumentation(transport)

	if hedging {
		httpClient, err = hedgingCfg.ClientWithRegisterer(httpClient, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	opts = append(opts, option.WithHTTPClient(httpClient))
	if !cfg.EnableOpenCensus {
		opts = append(opts, option.WithTelemetryDisabled())
	}

	client, err := clientFactory(ctx, opts...)
	if err != nil {
		return nil, err
	}

	bucket := client.Bucket(cfg.BucketName)

	if !cfg.EnableRetries {
		bucket = bucket.Retryer(storage.WithPolicy(storage.RetryNever))
	}

	return bucket, nil
}

func (s *GCSObjectClient) Stop() {
}

func (s *GCSObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := s.GetAttributes(ctx, objectKey); err != nil {
		if s.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *GCSObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	attrs, err := s.getsBuckets.Object(objectKey).Attrs(ctx)
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	if attrs != nil {
		return client.ObjectAttributes{Size: attrs.Size}, nil
	}
	return client.ObjectAttributes{}, nil
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
func (s *GCSObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var cancel context.CancelFunc = func() {}
	if s.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
	}

	rc, size, err := s.getObject(ctx, objectKey)
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, 0, err
	}
	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return util.NewReadCloserWithContextCancelFunc(rc, cancel), size, nil
}

// GetObject returns a reader and the size for the specified object key from the configured GCS bucket.
func (s *GCSObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var cancel context.CancelFunc = func() {}
	if s.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
	}

	rangeReader, err := s.getsBuckets.Object(objectKey).NewRangeReader(ctx, offset, length)
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, err
	}

	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return util.NewReadCloserWithContextCancelFunc(rangeReader, cancel), nil
}

func (s *GCSObjectClient) getObject(ctx context.Context, objectKey string) (rc io.ReadCloser, size int64, err error) {
	reader, err := s.getsBuckets.Object(objectKey).NewReader(ctx)
	if err != nil {
		return nil, 0, err
	}

	return reader, reader.Attrs.Size, nil
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *GCSObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	writer := s.defaultBucket.Object(objectKey).NewWriter(ctx)
	// Default GCSChunkSize is 8M and for each call, 8M is allocated xD
	// By setting it to 0, we just upload the object in a single a request
	// which should work for our chunk sizes.
	writer.ChunkSize = s.cfg.ChunkBufferSize

	if _, err := io.Copy(writer, object); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

// List implements chunk.ObjectClient.
func (s *GCSObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	q := &storage.Query{Prefix: prefix, Delimiter: delimiter}

	// Using delimiter and selected attributes doesn't work well together -- it returns nothing.
	// Reason is that Go's API only sets "fields=items(name,updated)" parameter in the request,
	// but what we really need is "fields=prefixes,items(name,updated)". Unfortunately we cannot set that,
	// so instead we don't use attributes selection when using delimiter.
	if delimiter == "" {
		err := q.SetAttrSelection([]string{"Name", "Updated"})
		if err != nil {
			return nil, nil, err
		}
	}

	iter := s.defaultBucket.Objects(ctx, q)
	for {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		attr, err := iter.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return nil, nil, err
		}

		// When doing query with Delimiter, Prefix is the only field set for entries which represent synthetic "directory entries".
		if attr.Prefix != "" {
			commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(attr.Prefix))
			continue
		}

		storageObjects = append(storageObjects, client.StorageObject{
			Key:        attr.Name,
			ModifiedAt: attr.Updated,
		})
	}

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured GCS bucket.
func (s *GCSObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	err := s.defaultBucket.Object(objectKey).Delete(ctx)
	if err != nil {
		return err
	}

	return nil
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *GCSObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, storage.ErrObjectNotExist)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isContextErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

// IsStorageTimeoutErr returns true if error means that object cannot be retrieved right now due to server-side timeouts.
func IsStorageTimeoutErr(err error) bool {
	// TODO(dannyk): move these out to be generic
	// context errors are all client-side
	if isContextErr(err) {
		// Go 1.23 changed the type of the error returned by the http client when a timeout occurs
		// while waiting for headers.  This is a server side timeout.
		return strings.Contains(err.Error(), "Client.Timeout")
	}

	// connection misconfiguration, or writing on a closed connection
	// do NOT retry; this is not a server-side issue
	if errors.Is(err, net.ErrClosed) || amnet.IsConnectionRefused(err) {
		return false
	}

	// this is a server-side timeout
	if isTimeoutError(err) {
		return true
	}

	// connection closed (closed before established) or reset (closed after established)
	// this is a server-side issue
	if errors.Is(err, io.EOF) || amnet.IsConnectionReset(err) {
		return true
	}

	if gerr, ok := err.(*googleapi.Error); ok {
		// https://cloud.google.com/storage/docs/retry-strategy
		return gerr.Code == http.StatusRequestTimeout ||
			gerr.Code == http.StatusGatewayTimeout
	}

	return false
}

// IsStorageThrottledErr returns true if error means that object cannot be retrieved right now due to throttling.
func IsStorageThrottledErr(err error) bool {
	if gerr, ok := err.(*googleapi.Error); ok {
		// https://cloud.google.com/storage/docs/retry-strategy
		return gerr.Code == http.StatusTooManyRequests ||
			(gerr.Code/100 == 5) // all 5xx errors are retryable
	}

	return false
}

// IsRetryableErr returns true if the request failed due to some retryable server-side scenario
func IsRetryableErr(err error) bool {
	return IsStorageTimeoutErr(err) || IsStorageThrottledErr(err)
}

// IsRetryableErr returns true if the request failed due to some retryable server-side scenario
func (s *GCSObjectClient) IsRetryableErr(err error) bool {
	return IsRetryableErr(err)
}

func gcsTransport(ctx context.Context, scope string, insecure bool, http2 bool, serviceAccount flagext.Secret) (http.RoundTripper, error) {
	customTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if !http2 {
		// disable HTTP/2 by setting TLSNextProto to non-nil empty map, as per the net/http documentation.
		// see http2 section of https://pkg.go.dev/net/http
		customTransport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		customTransport.ForceAttemptHTTP2 = false
	}
	transportOptions := []option.ClientOption{option.WithScopes(scope)}
	if insecure {
		customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //#nosec G402 -- User has explicitly requested to disable TLS
		transportOptions = append(transportOptions, option.WithoutAuthentication())
	}
	if serviceAccount.String() != "" {
		transportOptions = append(transportOptions, option.WithCredentialsJSON([]byte(serviceAccount.String())))
	}
	return google_http.NewTransport(ctx, customTransport, transportOptions...)
}
