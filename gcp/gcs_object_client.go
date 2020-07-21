package gcp

import (
	"context"
	"flag"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/cortexproject/cortex/pkg/chunk"
)

type GCSObjectClient struct {
	cfg       GCSConfig
	client    *storage.Client
	bucket    *storage.BucketHandle
	delimiter string
}

// GCSConfig is config for the GCS Chunk Client.
type GCSConfig struct {
	BucketName      string        `yaml:"bucket_name"`
	ChunkBufferSize int           `yaml:"chunk_buffer_size"`
	RequestTimeout  time.Duration `yaml:"request_timeout"`
}

// RegisterFlags registers flags.
func (cfg *GCSConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *GCSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"gcs.bucketname", "", "Name of GCS bucket to put chunks in.")
	f.IntVar(&cfg.ChunkBufferSize, prefix+"gcs.chunk-buffer-size", 0, "The size of the buffer that GCS client for each PUT request. 0 to disable buffering.")
	f.DurationVar(&cfg.RequestTimeout, prefix+"gcs.request-timeout", 0, "The duration after which the requests to GCS should be timed out.")
}

// NewGCSObjectClient makes a new chunk.Client that writes chunks to GCS.
func NewGCSObjectClient(ctx context.Context, cfg GCSConfig, delimiter string) (*GCSObjectClient, error) {
	option, err := gcsInstrumentation(ctx, storage.ScopeReadWrite)
	if err != nil {
		return nil, err
	}

	client, err := storage.NewClient(ctx, option)
	if err != nil {
		return nil, err
	}
	return newGCSObjectClient(cfg, client, delimiter), nil
}

func newGCSObjectClient(cfg GCSConfig, client *storage.Client, delimiter string) *GCSObjectClient {
	bucket := client.Bucket(cfg.BucketName)
	return &GCSObjectClient{
		cfg:       cfg,
		client:    client,
		bucket:    bucket,
		delimiter: delimiter,
	}
}

func (s *GCSObjectClient) Stop() {
	s.client.Close()
}

// GetObject returns a reader for the specified object key from the configured GCS bucket. If the
// key does not exist a generic chunk.ErrStorageObjectNotFound error is returned.
func (s *GCSObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	if s.cfg.RequestTimeout > 0 {
		// The context will be cancelled with the timeout or when the parent context is cancelled, whichever occurs first.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
		defer cancel()
	}

	reader, err := s.bucket.Object(objectKey).NewReader(ctx)

	if err != nil {
		if err == storage.ErrObjectNotExist {
			return nil, chunk.ErrStorageObjectNotFound
		}
		return nil, err
	}

	return reader, nil
}

// PutObject puts the specified bytes into the configured GCS bucket at the provided key
func (s *GCSObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	writer := s.bucket.Object(objectKey).NewWriter(ctx)
	// Default GCSChunkSize is 8M and for each call, 8M is allocated xD
	// By setting it to 0, we just upload the object in a single a request
	// which should work for our chunk sizes.
	writer.ChunkSize = s.cfg.ChunkBufferSize

	if _, err := io.Copy(writer, object); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return nil
}

// List objects and common-prefixes i.e synthetic directories from the store non-recursively
func (s *GCSObjectClient) List(ctx context.Context, prefix string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	var storageObjects []chunk.StorageObject
	var commonPrefixes []chunk.StorageCommonPrefix

	iter := s.bucket.Objects(ctx, &storage.Query{Prefix: prefix, Delimiter: s.delimiter})
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
		if attr.Name == "" {
			commonPrefixes = append(commonPrefixes, chunk.StorageCommonPrefix(attr.Prefix))
			continue
		}

		storageObjects = append(storageObjects, chunk.StorageObject{
			Key:        attr.Name,
			ModifiedAt: attr.Updated,
		})
	}

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured GCS bucket. If the
// key does not exist a generic chunk.ErrStorageObjectNotFound error is returned.
func (s *GCSObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	err := s.bucket.Object(objectKey).Delete(ctx)

	if err != nil {
		if err == storage.ErrObjectNotExist {
			return chunk.ErrStorageObjectNotFound
		}
		return err
	}

	return nil
}

func (s *GCSObjectClient) PathSeparator() string {
	return s.delimiter
}
