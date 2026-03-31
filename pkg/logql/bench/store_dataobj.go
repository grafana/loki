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
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// DataObjStore implements Store using the dataobj format
type DataObjStore struct {
	path             string
	tenant           string
	builder          *logsobj.Builder
	buf              *bytes.Buffer
	uploader         *uploader.Uploader
	logsMetastoreToc *metastore.TableOfContentsWriter

	bucket objstore.Bucket

	// Index files have their own, separate, metastore directory, with their own table of contents.
	indexWriterBucket objstore.Bucket
	indexMetastoreToc *metastore.TableOfContentsWriter

	logger log.Logger
}

// NewDataObjStore creates a new DataObjStore
func NewDataObjStore(dir, tenant string) (*DataObjStore, error) {
	storageDir := filepath.Join(dir, storageDir)

	basePath := filepath.Join(storageDir, "dataobj")
	objectsPath := filepath.Join(basePath, "objects")
	if err := os.MkdirAll(objectsPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create object path: %w", err)
	}

	tocsPath := filepath.Join(basePath, "tocs")
	if err := os.MkdirAll(tocsPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create required directories for index
	indexPathPrefix := "index/v0"
	indexPath := filepath.Join(basePath, indexPathPrefix)
	tocDir := filepath.Join(indexPath, "tocs")
	if err := os.MkdirAll(tocDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create toc directory: %w", err)
	}
	indexesPath := filepath.Join(indexPath, "indexes")
	if err := os.MkdirAll(indexesPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create indexes directory: %w", err)
	}

	bucket, err := filesystem.NewBucket(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2 * 1024 * 1024, // 2MB
			MaxPageRows:             1000,
			TargetObjectSize:        128 * 1024 * 1024, // 128MB
			TargetSectionSize:       16 * 1024 * 1024,  // 16MB
			BufferSize:              16 * 1024 * 1024,  // 16MB
			SectionStripeMergeLimit: 2,
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())
	logsMetastoreToc := metastore.NewTableOfContentsWriter(bucket, logger)
	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, logger)

	// Create prefixed bucket & metastore for indexes
	indexWriterBucket := objstore.NewPrefixedBucket(bucket, indexPathPrefix)
	indexMetastoreToc := metastore.NewTableOfContentsWriter(indexWriterBucket, logger)

	return &DataObjStore{
		tenant:            tenant,
		builder:           builder,
		buf:               bytes.NewBuffer(make([]byte, 0, 128*1024*1024)), // 128MB buffer
		uploader:          uploader,
		logsMetastoreToc:  logsMetastoreToc,
		bucket:            bucket,
		logger:            logger,
		indexWriterBucket: indexWriterBucket,
		indexMetastoreToc: indexMetastoreToc,
	}, nil
}

// Write implements Store
func (s *DataObjStore) Write(_ context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		if err := s.builder.Append(s.tenant, stream); errors.Is(err, logsobj.ErrBuilderFull) {
			// If the builder is full, flush it and try again
			if err := s.flush(); err != nil {
				return fmt.Errorf("failed to flush builder: %w", err)
			}
			// Try appending again
			if err := s.builder.Append(s.tenant, stream); err != nil {
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

	timeRanges := s.builder.TimeRanges()
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

	if err = s.logsMetastoreToc.WriteEntry(context.Background(), path, timeRanges); err != nil {
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
		timeRanges := calculator.TimeRanges()
		obj, closer, err := calculator.Flush()
		if err != nil {
			return fmt.Errorf("failed to flush index: %w", err)
		}
		defer closer.Close()

		key, err := index.ObjectKey(context.Background(), obj)
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

		err = s.indexMetastoreToc.WriteEntry(context.Background(), key, timeRanges)
		if err != nil {
			return fmt.Errorf("failed to update metastore: %w", err)
		}
		calculator.Reset()
		return nil
	}

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
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

		reader, err := dataobj.FromBucket(context.Background(), s.bucket, name, 0)
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
