package bench

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/querier"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

// DataObjStore implements Store using the dataobj format
type DataObjStore struct {
	dir      string
	tenantID string
	builder  *logsobj.Builder
	buf      *bytes.Buffer
	uploader *uploader.Uploader
	meta     *metastore.TableOfContentsWriter

	bucket objstore.Bucket

	indexWriterBucket objstore.Bucket
	indexMetastoreToc *metastore.TableOfContentsWriter

	logger log.Logger
}

// NewDataObjStore creates a new DataObjStore
func NewDataObjStore(dir, tenantID string) (*DataObjStore, error) {
	// Create store-specific directory
	storeDir := filepath.Join(dir, "dataobj")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create required directories for metastore and tenant
	tenantDir := filepath.Join(storeDir, "tenant-"+tenantID)
	metastoreDir := filepath.Join(tenantDir, "metastore")
	if err := os.MkdirAll(metastoreDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create metastore directory: %w", err)
	}

	// Create required directories for index
	indexDirPrefix := "index/v0"
	indexDir := filepath.Join(storeDir, indexDirPrefix)
	tenantIndexDir := filepath.Join(indexDir, "tenant-"+tenantID)
	metastoreIndexDir := filepath.Join(tenantIndexDir, "metastore")
	if err := os.MkdirAll(metastoreIndexDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create index directory: %w", err)
	}

	bucket, err := filesystem.NewBucket(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:    2 * 1024 * 1024,   // 2MB
		TargetObjectSize:  128 * 1024 * 1024, // 128MB
		TargetSectionSize: 16 * 1024 * 1024,  // 16MB
		BufferSize:        16 * 1024 * 1024,  // 16MB

		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())
	meta := metastore.NewTableOfContentsWriter(metastore.Config{}, bucket, tenantID, logger)
	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID, logger)

	// Create prefixed bucket & metastore for indexes
	indexWriterBucket := objstore.NewPrefixedBucket(bucket, indexDirPrefix)
	indexMetastoreToc := metastore.NewTableOfContentsWriter(metastore.Config{}, indexWriterBucket, tenantID, logger)

	return &DataObjStore{
		dir:               storeDir,
		tenantID:          tenantID,
		builder:           builder,
		buf:               bytes.NewBuffer(make([]byte, 0, 128*1024*1024)), // 128MB buffer
		uploader:          uploader,
		meta:              meta,
		bucket:            bucket,
		logger:            logger,
		indexWriterBucket: indexWriterBucket,
		indexMetastoreToc: indexMetastoreToc,
	}, nil
}

// Write implements Store
func (s *DataObjStore) Write(_ context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		if err := s.builder.Append(stream); errors.Is(err, logsobj.ErrBuilderFull) {
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

func (s *DataObjStore) Querier() (logql.Querier, error) {
	return querier.NewStore(s.bucket, s.logger, metastore.NewObjectMetastore(metastore.StorageConfig{}, s.bucket, s.logger, prometheus.DefaultRegisterer)), nil
}

func (s *DataObjStore) flush() error {
	// Reset the buffer
	s.buf.Reset()

	minTime, maxTime := s.builder.TimeRange()
	obj, closer, err := s.builder.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}
	defer closer.Close()

	// Upload the data object using the uploader
	path, err := s.uploader.Upload(context.Background(), obj)
	if err != nil {
		return fmt.Errorf("failed to upload data object: %w", err)
	}

	// Update metastore with the new data object
	err = s.meta.WriteEntry(context.Background(), path, minTime, maxTime)
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

	if err := s.buildIndex(); err != nil {
		return fmt.Errorf("failed to build index: %w", err)
	}

	return nil
}

func (s *DataObjStore) buildIndex() error {
	flushAndUpload := func(calculator *index.Calculator) error {
		minTime, maxTime := calculator.TimeRange()
		obj, closer, err := calculator.Flush()
		if err != nil {
			return fmt.Errorf("failed to flush index: %w", err)
		}
		defer closer.Close()

		key, err := index.ObjectKey(context.Background(), s.tenantID, obj)
		if err != nil {
			return fmt.Errorf("failed to create object key: %w", err)
		}

		reader, err := obj.Reader(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create reader for index object: %w", err)
		}
		defer reader.Close()

		err = s.indexWriterBucket.Upload(context.Background(), key, reader)
		if err != nil {
			return fmt.Errorf("failed to upload index: %w", err)
		}

		err = s.indexMetastoreToc.WriteEntry(context.Background(), key, minTime, maxTime)
		if err != nil {
			return fmt.Errorf("failed to update metastore: %w", err)
		}
		calculator.Reset()
		return nil
	}

	builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
		TargetPageSize:    128 * 1024,        // 128KB
		TargetObjectSize:  128 * 1024 * 1024, // 128MB
		TargetSectionSize: 16 * 1024 * 1024,  // 16MB
		BufferSize:        16 * 1024 * 1024,  // 16MB

		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to create index builder: %w", err)
	}

	calculator := index.NewCalculator(builder)
	cnt := 0
	objectsPerIndex := 16
	err = s.bucket.Iter(context.Background(), "", func(name string) error {
		if !strings.Contains(name, "objects") {
			return nil
		}

		reader, err := dataobj.FromBucket(context.Background(), s.bucket, name)
		if err != nil {
			return fmt.Errorf("failed to read object: %w", err)
		}

		err = calculator.Calculate(context.Background(), s.logger, reader, name)
		if err != nil {
			return fmt.Errorf("failed to calculate index: %w", err)
		}
		cnt++

		if cnt%objectsPerIndex == 0 {
			if err := flushAndUpload(calculator); err != nil {
				return fmt.Errorf("failed to flush and upload index: %w", err)
			}
		}
		return nil
	}, objstore.WithRecursiveIter())
	if err != nil {
		return fmt.Errorf("failed to iterate over objects: %w", err)
	}

	if cnt%objectsPerIndex != 0 {
		if err := flushAndUpload(calculator); err != nil {
			return fmt.Errorf("failed to flush and upload index: %w", err)
		}
	}
	return nil
}
