package bench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

// DataObjStore implements Store using the dataobj format
type MultiTenantDataObjStore struct {
	dir       string
	tenantIDs []string
	builder   *logsobj.MultiTenantBuilder
	buf       *bytes.Buffer
	uploader  *uploader.MultiTenantUploader
	meta      *metastore.MultiTenantUpdater

	bucket objstore.Bucket

	logger log.Logger
	engine logql.Engine
}

// NewDataObjStore creates a new DataObjStore
func NewMultiTenantDataObjStore(dir string, tenantIDs []string) (*MultiTenantDataObjStore, error) {
	// Create store-specific directory
	storeDir := filepath.Join(dir, "dataobj")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create required directories for metastore and tenant
	tenantDir := filepath.Join(storeDir, "multi-tenant")
	metastoreDir := filepath.Join(tenantDir, "metastore")
	if err := os.MkdirAll(metastoreDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create metastore directory: %w", err)
	}

	bucket, err := filesystem.NewBucket(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())

	builder, err := logsobj.NewMultiTenantBuilder(logsobj.BuilderConfig{
		TargetPageSize:    2 * 1024 * 1024,   // 2MB
		TargetObjectSize:  128 * 1024 * 1024, // 128MB
		TargetSectionSize: 16 * 1024 * 1024,  // 16MB
		BufferSize:        16 * 1024 * 1024,  // 16MB

		SectionStripeMergeLimit: 2,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	meta := metastore.NewMultiTenantUpdater(metastore.UpdaterConfig{}, bucket, logger)
	uploader := uploader.NewMultiTenant(uploader.Config{SHAPrefixSize: 2}, bucket, logger)

	engineOpts := logql.EngineOpts{
		EnableV2Engine: true,
		BatchSize:      512,
	}
	queryEngine := engine.New(engineOpts, bucket, logql.NoLimits, nil, logger, true)

	return &MultiTenantDataObjStore{
		dir:       storeDir,
		builder:   builder,
		buf:       bytes.NewBuffer(make([]byte, 0, 128*1024*1024)), // 128MB buffer
		uploader:  uploader,
		meta:      meta,
		bucket:    bucket,
		logger:    logger,
		tenantIDs: tenantIDs,
		engine:    queryEngine,
	}, nil
}

// Write implements Store
func (s *MultiTenantDataObjStore) Write(_ context.Context, streams []logproto.Stream) error {
	for _, tenantID := range s.tenantIDs {
		for _, stream := range streams {
			if err := s.builder.Append(tenantID, stream); errors.Is(err, logsobj.ErrBuilderFull) {
				// If the builder is full, flush it and try again
				if err := s.flush(); err != nil {
					return fmt.Errorf("failed to flush builder: %w", err)
				}
				// Try appending again
				if err := s.builder.Append(tenantID, stream); err != nil {
					return fmt.Errorf("failed to append stream after flush: %w", err)
				}
			} else if err != nil {
				return fmt.Errorf("failed to append stream: %w", err)
			}
		}
	}
	return nil
}

func (s *MultiTenantDataObjStore) flush() error {
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
	err = s.meta.Update(context.Background(), s.tenantIDs, path, stats.MinTimestamp, stats.MaxTimestamp)
	if err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	// Reset the builder for reuse
	s.builder.Reset()
	return nil
}

// Name implements Store
func (s *MultiTenantDataObjStore) Name() string {
	return "mutli-tenant-dataobj"
}

// Close flushes any remaining data and closes resources
func (s *MultiTenantDataObjStore) Close() error {
	// Flush any remaining data
	if err := s.flush(); err != nil {
		return fmt.Errorf("failed to flush remaining data: %w", err)
	}

	return nil
}
