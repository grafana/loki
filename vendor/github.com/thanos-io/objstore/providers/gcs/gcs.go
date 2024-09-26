// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package gcs implements common object storage abstractions against Google Cloud Storage.
package gcs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/version"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/experimental"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

var DefaultConfig = Config{
	HTTPConfig: exthttp.DefaultHTTPConfig,
}

// Config stores the configuration for gcs bucket.
type Config struct {
	Bucket         string `yaml:"bucket"`
	ServiceAccount string `yaml:"service_account"`
	UseGRPC        bool   `yaml:"use_grpc"`
	// GRPCConnPoolSize controls the size of the gRPC connection pool and should only be used
	// when direct path is not enabled.
	// See https://pkg.go.dev/cloud.google.com/go/storage#hdr-Experimental_gRPC_API for more details
	// on how to enable direct path.
	GRPCConnPoolSize int                `yaml:"grpc_conn_pool_size"`
	HTTPConfig       exthttp.HTTPConfig `yaml:"http_config"`

	// ChunkSizeBytes controls the maximum number of bytes of the object that the
	// Writer will attempt to send to the server in a single request
	// Used as storage.Writer.ChunkSize of https://pkg.go.dev/google.golang.org/cloud/storage#Writer
	ChunkSizeBytes int `yaml:"chunk_size_bytes"`
}

// Bucket implements the store.Bucket and shipper.Bucket interfaces against GCS.
type Bucket struct {
	logger    log.Logger
	bkt       *storage.BucketHandle
	name      string
	chunkSize int

	closer io.Closer
}

// parseConfig unmarshals a buffer into a Config with default values.
func parseConfig(conf []byte) (Config, error) {
	config := DefaultConfig
	if err := yaml.UnmarshalStrict(conf, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// NewBucket returns a new Bucket against the given bucket handle.
func NewBucket(ctx context.Context, logger log.Logger, conf []byte, component string) (*Bucket, error) {
	config, err := parseConfig(conf)
	if err != nil {
		return nil, err
	}
	return NewBucketWithConfig(ctx, logger, config, component)
}

// NewBucketWithConfig returns a new Bucket with gcs Config struct.
func NewBucketWithConfig(ctx context.Context, logger log.Logger, gc Config, component string) (*Bucket, error) {
	if gc.Bucket == "" {
		return nil, errors.New("missing Google Cloud Storage bucket name for stored blocks")
	}

	var opts []option.ClientOption

	// If ServiceAccount is provided, use them in GCS client, otherwise fallback to Google default logic.
	if gc.ServiceAccount != "" {
		credentials, err := google.CredentialsFromJSON(ctx, []byte(gc.ServiceAccount), storage.ScopeFullControl)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create credentials from JSON")
		}
		opts = append(opts, option.WithCredentials(credentials))
	}

	opts = append(opts,
		option.WithUserAgent(fmt.Sprintf("thanos-%s/%s (%s)", component, version.Version, runtime.Version())),
	)

	if !gc.UseGRPC {
		var err error
		opts, err = appendHttpOptions(gc, opts)
		if err != nil {
			return nil, err
		}
	}

	return newBucket(ctx, logger, gc, opts)
}

func appendHttpOptions(gc Config, opts []option.ClientOption) ([]option.ClientOption, error) {
	// Check if a roundtripper has been set in the config
	// otherwise build the default transport.
	var rt http.RoundTripper
	if gc.HTTPConfig.Transport != nil {
		rt = gc.HTTPConfig.Transport
	} else {
		var err error
		rt, err = exthttp.DefaultTransport(gc.HTTPConfig)
		if err != nil {
			return nil, err
		}
	}

	// GCS uses some defaults when "options.WithHTTPClient" is not used that are important when we call
	// htransport.NewTransport namely the scopes that are then used for OAth authentication. So to build our own
	// http client we need to se those defaults
	opts = append(opts, option.WithScopes(storage.ScopeFullControl, "https://www.googleapis.com/auth/cloud-platform"))
	gRT, err := htransport.NewTransport(context.Background(), rt, opts...)
	if err != nil {
		return nil, err
	}

	httpCli := &http.Client{
		Transport: gRT,
		Timeout:   time.Duration(gc.HTTPConfig.IdleConnTimeout),
	}
	return append(opts, option.WithHTTPClient(httpCli)), nil
}

func newBucket(ctx context.Context, logger log.Logger, gc Config, opts []option.ClientOption) (*Bucket, error) {
	var (
		err       error
		gcsClient *storage.Client
	)
	if gc.UseGRPC {
		opts = append(opts,
			option.WithGRPCDialOption(experimental.WithRecvBufferPool(grpc.NewSharedBufferPool())),
			option.WithGRPCConnectionPool(gc.GRPCConnPoolSize),
		)
		gcsClient, err = storage.NewGRPCClient(ctx, opts...)
	} else {
		gcsClient, err = storage.NewClient(ctx, opts...)
	}
	if err != nil {
		return nil, err
	}
	bkt := &Bucket{
		logger:    logger,
		bkt:       gcsClient.Bucket(gc.Bucket),
		closer:    gcsClient,
		name:      gc.Bucket,
		chunkSize: gc.ChunkSizeBytes,
	}
	return bkt, nil
}

// Name returns the bucket name for gcs.
func (b *Bucket) Name() string {
	return b.name
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	// Ensure the object name actually ends with a dir suffix. Otherwise we'll just iterate the
	// object itself as one prefix item.
	if dir != "" {
		dir = strings.TrimSuffix(dir, DirDelim) + DirDelim
	}

	// If recursive iteration is enabled we should pass an empty delimiter.
	delimiter := DirDelim
	if objstore.ApplyIterOptions(options...).Recursive {
		delimiter = ""
	}

	query := &storage.Query{
		Prefix:    dir,
		Delimiter: delimiter,
	}
	err := query.SetAttrSelection([]string{"Name"})
	if err != nil {
		return err
	}

	it := b.bkt.Objects(ctx, query)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		attrs, err := it.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		if err := f(attrs.Prefix + attrs.Name); err != nil {
			return err
		}
	}
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.bkt.Object(name).NewReader(ctx)
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.bkt.Object(name).NewRangeReader(ctx, off, length)
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	attrs, err := b.bkt.Object(name).Attrs(ctx)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         attrs.Size,
		LastModified: attrs.Updated,
	}, nil
}

// Handle returns the underlying GCS bucket handle.
// Used for testing purposes (we return handle, so it is not instrumented).
func (b *Bucket) Handle() *storage.BucketHandle {
	return b.bkt
}

// Exists checks if the given object exists.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	if _, err := b.bkt.Object(name).Attrs(ctx); err == nil {
		return true, nil
	} else if err != storage.ErrObjectNotExist {
		return false, err
	}
	return false, nil
}

// Upload writes the file specified in src to remote GCS location specified as target.
func (b *Bucket) Upload(ctx context.Context, name string, r io.Reader) error {
	w := b.bkt.Object(name).NewWriter(ctx)

	// if `chunkSize` is 0, we don't set any custom value for writer's ChunkSize.
	// It uses whatever the default value https://pkg.go.dev/google.golang.org/cloud/storage#Writer
	if b.chunkSize > 0 {
		w.ChunkSize = b.chunkSize
	}

	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return w.Close()
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(ctx context.Context, name string) error {
	return b.bkt.Object(name).Delete(ctx)
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	return errors.Is(err, storage.ErrObjectNotExist)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *Bucket) IsAccessDeniedErr(err error) bool {
	if s, ok := status.FromError(err); ok && s.Code() == codes.PermissionDenied {
		return true
	}
	return false
}

func (b *Bucket) Close() error {
	return b.closer.Close()
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB, project string) (objstore.Bucket, func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gTestConfig := Config{
		Bucket: objstore.CreateTemporaryTestBucketName(t),
	}

	bc, err := yaml.Marshal(gTestConfig)
	if err != nil {
		return nil, nil, err
	}

	b, err := NewBucket(ctx, log.NewNopLogger(), bc, "thanos-e2e-test")
	if err != nil {
		return nil, nil, err
	}

	if err = b.bkt.Create(ctx, project, nil); err != nil {
		_ = b.Close()
		return nil, nil, err
	}

	t.Log("created temporary GCS bucket for GCS tests with name", b.name, "in project", project)
	return b, func() {
		objstore.EmptyBucket(t, ctx, b)
		if err := b.bkt.Delete(ctx); err != nil {
			t.Logf("deleting bucket failed: %s", err)
		}
		if err := b.Close(); err != nil {
			t.Logf("closing bucket failed: %s", err)
		}
	}, nil
}
