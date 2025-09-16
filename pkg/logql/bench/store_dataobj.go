package bench

import (
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
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/ring"
)

// partitionBuilder represents a builder assigned to a specific partition.
type partitionBuilder struct {
	partitionID      int
	logger           log.Logger
	builder          *logsobj.Builder
	uploader         *uploader.Uploader
	logsMetastoreToc *metastore.TableOfContentsWriter

	// stats
	totalUploadedObjects int
}

// newPartitionBuilder creates a new internal partition store
func newPartitionBuilder(partitionID int, uploader *uploader.Uploader, tocWriter *metastore.TableOfContentsWriter, logger log.Logger) (*partitionBuilder, error) {
	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:          2 * 1024 * 1024, // 2MB
		MaxPageRows:             1000,
		TargetObjectSize:        128 * 1024 * 1024, // 128MB
		TargetSectionSize:       16 * 1024 * 1024,  // 16MB
		BufferSize:              16 * 1024 * 1024,  // 16MB
		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	return &partitionBuilder{
		logger:           logger,
		partitionID:      partitionID,
		builder:          builder,
		uploader:         uploader,
		logsMetastoreToc: tocWriter,
	}, nil
}

// write writes streams to this partition's builder
func (pb *partitionBuilder) write(_ context.Context, streams []logproto.Stream, tenant string) error {
	for _, stream := range streams {
		if err := pb.builder.Append(tenant, stream); errors.Is(err, logsobj.ErrBuilderFull) {
			// If the builder is full, flush it and try again
			if err := pb.flush(); err != nil {
				return fmt.Errorf("failed to flush builder: %w", err)
			}
			// Try appending again
			if err := pb.builder.Append(tenant, stream); err != nil {
				return fmt.Errorf("failed to append stream after flush: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to append stream: %w", err)
		}
	}

	return nil
}

// flush builds a complete data object from the builder, uploads it, and records it in the metastore
func (pb *partitionBuilder) flush() error {
	timeRanges := pb.builder.TimeRanges()
	obj, closer, err := pb.builder.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}
	defer closer.Close()

	// Upload the data object using the shared uploader
	path, err := pb.uploader.Upload(context.Background(), obj)
	if err != nil {
		return fmt.Errorf("failed to upload data object: %w", err)
	}

	if err = pb.logsMetastoreToc.WriteEntry(context.Background(), path, timeRanges); err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	pb.totalUploadedObjects++

	// Reset the builder for reuse
	pb.builder.Reset()
	return nil
}

// close flushes any remaining data and closes resources
func (pb *partitionBuilder) close() error {
	// Flush any remaining data
	if err := pb.flush(); err != nil {
		return fmt.Errorf("failed to flush remaining data: %w", err)
	}

	level.Info(pb.logger).Log(
		"msg", "partition builder summary",
		"partition_id", pb.partitionID,
		"total_objects", pb.totalUploadedObjects,
	)

	return nil
}

// DataObjStore implements Store using the dataobj format with partition support
type DataObjStore struct {
	tenant            string
	numPartitions     int
	partitionStrategy PartitionStrategy

	partitionBuilders map[int]*partitionBuilder
	logger            log.Logger

	// Shared across all partitions
	uploader          *uploader.Uploader
	logsMetastoreToc  *metastore.TableOfContentsWriter
	bucket            objstore.Bucket
	indexWriterBucket objstore.Bucket
	indexMetastoreToc *metastore.TableOfContentsWriter
}

// NewDataObjStore creates a new DataObjStore with partition support
func NewDataObjStore(dir, tenant string, partitions int, strategy PartitionStrategy) (*DataObjStore, error) {
	if partitions <= 0 {
		return nil, fmt.Errorf("number of partitions must be greater than 0, got %d", partitions)
	}

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

	// Create shared bucket
	bucket, err := filesystem.NewBucket(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())

	// Create shared components
	logsMetastoreToc := metastore.NewTableOfContentsWriter(bucket, logger)
	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, logger)
	indexWriterBucket := objstore.NewPrefixedBucket(bucket, indexPathPrefix)
	indexMetastoreToc := metastore.NewTableOfContentsWriter(indexWriterBucket, logger)

	store := &DataObjStore{
		tenant:            tenant,
		numPartitions:     partitions,
		partitionStrategy: strategy,
		partitionBuilders: make(map[int]*partitionBuilder),
		logger:            logger,
		uploader:          uploader,
		logsMetastoreToc:  logsMetastoreToc,
		bucket:            bucket,
		indexWriterBucket: indexWriterBucket,
		indexMetastoreToc: indexMetastoreToc,
	}

	return store, nil
}

// getOrCreatePartitionStore gets an existing partition store or creates a new one
func (ds *DataObjStore) getOrCreatePartitionStore(partitionID int) (*partitionBuilder, error) {
	store, ok := ds.partitionBuilders[partitionID]
	if ok {
		return store, nil
	}

	// Create new partition store
	store, err := newPartitionBuilder(partitionID, ds.uploader, ds.logsMetastoreToc, ds.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create partition store %d: %w", partitionID, err)
	}

	ds.partitionBuilders[partitionID] = store
	return store, nil
}

// Write implements Store - distributes streams to appropriate partitions
func (ds *DataObjStore) Write(ctx context.Context, streams []logproto.Stream) error {
	// Group streams by partition
	partitionStreams := make(map[int][]logproto.Stream, ds.numPartitions)
	for _, stream := range streams {
		partitionID := ds.calculatePartitionID(stream)
		partitionStreams[partitionID] = append(partitionStreams[partitionID], stream)
	}

	// Write to each partition
	for partitionID, streams := range partitionStreams {
		store, err := ds.getOrCreatePartitionStore(partitionID)
		if err != nil {
			return fmt.Errorf("failed to get partition store %d: %w", partitionID, err)
		}

		if err := store.write(ctx, streams, ds.tenant); err != nil {
			return fmt.Errorf("failed to write to partition %d: %w", partitionID, err)
		}
	}

	return nil
}

// calculatePartitionID determines which partition a stream should be assigned to
func (ds *DataObjStore) calculatePartitionID(stream logproto.Stream) int {
	switch ds.partitionStrategy {
	case PartitionByServiceName:
		return ds.calculatePartitionByServiceName(stream)
	default:
		return ds.calculatePartitionByStreamLabels(stream)
	}
}

// calculatePartitionByStreamLabels distributes streams by their labels and tenant
func (ds *DataObjStore) calculatePartitionByStreamLabels(stream logproto.Stream) int {
	return int(ring.TokenFor(ds.tenant, stream.Labels)) % ds.numPartitions
}

// calculatePartitionByServiceName distributes streams by service_name label and tenant
func (ds *DataObjStore) calculatePartitionByServiceName(stream logproto.Stream) int {
	parsedLabels, err := syntax.ParseLabels(stream.Labels)
	if err != nil {
		// If parsing fails, fall back to stream labels partitioning
		return ds.calculatePartitionByStreamLabels(stream)
	}

	serviceName := parsedLabels.Get("service_name")
	if serviceName == "" {
		// If no service_name found, fall back to stream labels partitioning
		return ds.calculatePartitionByStreamLabels(stream)
	}

	return int(ring.TokenFor(ds.tenant, fmt.Sprintf("{service_name=\"%s\"}", serviceName))) % ds.numPartitions
}

// Name implements Store
func (ds *DataObjStore) Name() string {
	return "dataobj"
}

// Close flushes any remaining data and closes all partition resources
func (ds *DataObjStore) Close() error {
	var lastErr error
	totalObjects := 0

	for partitionID, store := range ds.partitionBuilders {
		if err := store.close(); err != nil {
			level.Error(ds.logger).Log("msg", "failed to close partition", "partition", partitionID, "err", err)
			lastErr = err
		}

		totalObjects += store.totalUploadedObjects
	}

	if err := ds.buildIndex(); err != nil {
		level.Error(ds.logger).Log("msg", "failed to build index", "err", err)
		lastErr = err
	}

	level.Info(ds.logger).Log(
		"msg", "dataobj store summary",
		"partitions", ds.numPartitions,
		"total_objects", totalObjects,
	)

	return lastErr
}

// buildIndex builds the index for all partitions
func (ds *DataObjStore) buildIndex() error {
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

		err = ds.indexWriterBucket.Upload(context.Background(), key, reader)
		if err != nil {
			return fmt.Errorf("failed to upload index: %w", err)
		}

		err = ds.indexMetastoreToc.WriteEntry(context.Background(), key, timeRanges)
		if err != nil {
			return fmt.Errorf("failed to update metastore: %w", err)
		}
		calculator.Reset()
		return nil
	}

	builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
		TargetPageSize:          128 * 1024,        // 128KB
		TargetObjectSize:        128 * 1024 * 1024, // 128MB
		TargetSectionSize:       16 * 1024 * 1024,  // 16MB
		BufferSize:              16 * 1024 * 1024,  // 16MB
		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to create index builder: %w", err)
	}

	calculator := index.NewCalculator(builder)
	cnt := 0
	objectsPerIndex := 16
	err = ds.bucket.Iter(context.Background(), "", func(name string) error {
		if !strings.Contains(name, "objects") {
			return nil
		}

		reader, err := dataobj.FromBucket(context.Background(), ds.bucket, name)
		if err != nil {
			return fmt.Errorf("failed to read object: %w", err)
		}

		err = calculator.Calculate(context.Background(), ds.logger, reader, name)
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
