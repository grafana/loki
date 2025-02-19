package bench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Store represents a storage backend for log data
type Store interface {
	// Write writes a batch of streams to the store
	Write(ctx context.Context, streams []logproto.Stream) error
	// Name returns the name of the store implementation
	Name() string
	// Close flushes any remaining data and closes resources
	Close() error
}

// DataObjStore implements Store using the dataobj format
type DataObjStore struct {
	dir      string
	tenantID string
	builder  *dataobj.Builder
	buf      *bytes.Buffer
	uploader *uploader.Uploader
	meta     *metastore.Manager
}

// NewDataObjStore creates a new DataObjStore
func NewDataObjStore(dir, tenantID string) (*DataObjStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create required directories for metastore
	metastoreDir := filepath.Join(dir, "tenant-"+tenantID, "metastore")
	if err := os.MkdirAll(metastoreDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create metastore directory: %w", err)
	}

	bucket, err := filesystem.NewBucket(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	builder, err := dataobj.NewBuilder(dataobj.BuilderConfig{
		TargetPageSize:    2 * 1024 * 1024,   // 2MB
		TargetObjectSize:  128 * 1024 * 1024, // 128MB
		TargetSectionSize: 16 * 1024 * 1024,  // 16MB
		BufferSize:        16 * 1024 * 1024,  // 16MB
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	meta := metastore.NewManager(bucket, tenantID, log.NewNopLogger())
	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID)

	return &DataObjStore{
		dir:      dir,
		tenantID: tenantID,
		builder:  builder,
		buf:      bytes.NewBuffer(make([]byte, 0, 128*1024*1024)), // 128MB buffer
		uploader: uploader,
		meta:     meta,
	}, nil
}

// Write implements Store
func (s *DataObjStore) Write(_ context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		if err := s.builder.Append(stream); err == dataobj.ErrBuilderFull {
			// If the builder is full, flush it and try again
			if err := s.flush(); err != nil {
				return fmt.Errorf("failed to flush builder: %w", err)
			}
			// Try appending again
			if err := s.builder.Append(stream); err != nil {
				return fmt.Errorf("failed to append stream after flush: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to append stream: %w", err)
		}
	}
	return nil
}

func (s *DataObjStore) flush() error {
	// Reset the buffer
	s.buf.Reset()

	// Flush the builder to the buffer
	stats, err := s.builder.Flush(s.buf)
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}

	// Upload the data object using the uploader
	path, err := s.uploader.Upload(context.Background(), s.buf)
	if err != nil {
		return fmt.Errorf("failed to upload data object: %w", err)
	}

	// Update metastore with the new data object
	err = s.meta.UpdateMetastore(context.Background(), path, stats)
	if err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	// Reset the builder for reuse
	s.builder.Reset()
	return nil
}

// Name implements Store
func (s *DataObjStore) Name() string {
	return "dataobj"
}

// Close flushes any remaining data and closes resources
func (s *DataObjStore) Close() error {
	// Flush any remaining data
	if err := s.flush(); err != nil {
		return fmt.Errorf("failed to flush remaining data: %w", err)
	}
	return nil
}

// Builder helps construct test datasets using a Store
type Builder struct {
	store Store
	gen   *Generator
}

// NewBuilder creates a new Builder
func NewBuilder(store Store, opt Opt) *Builder {
	return &Builder{
		store: store,
		gen:   NewGenerator(opt),
	}
}

// Generate generates and stores the specified amount of data
func (b *Builder) Generate(ctx context.Context, targetSize int64) error {
	// Clear the data directory first if it exists
	if store, ok := b.store.(*DataObjStore); ok {
		if err := os.RemoveAll(store.dir); err != nil {
			return fmt.Errorf("failed to clear data directory: %w", err)
		}
		if err := os.MkdirAll(store.dir, 0o755); err != nil {
			return fmt.Errorf("failed to recreate data directory: %w", err)
		}
	}

	var totalSize int64

	for totalSize < targetSize {
		var streams []logproto.Stream
		for batch := range b.gen.Batches() {
			streams = append(streams, batch.Streams...)
			totalSize += int64(batch.Size())
			if totalSize >= targetSize {
				break
			}
		}

		if err := b.store.Write(ctx, streams); err != nil {
			return fmt.Errorf("failed to write to store: %w", err)
		}
	}

	// Close the store to ensure all data is flushed
	if err := b.store.Close(); err != nil {
		return fmt.Errorf("failed to close store: %w", err)
	}

	return nil
}
