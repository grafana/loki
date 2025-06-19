package bench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/parquet-go/parquet-go"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

// DataObjStore implements Store using the dataobj format
type ParquetStore struct {
	dir      string
	tenantID string
	buf      *bytes.Buffer

	logLineBuf []*LogLine

	bucket objstore.Bucket

	flushCount int

	logger log.Logger
}

// NewParquetStore creates a new ParquetStore
func NewParquetStore(dir, tenantID string) (*ParquetStore, error) {
	// Create store-specific directory
	storeDir := filepath.Join(dir, "parquet")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create required directories for metastore and tenant
	tenantDir := filepath.Join(storeDir, "tenant-"+tenantID)
	if err := os.MkdirAll(tenantDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	bucket, err := filesystem.NewBucket(storeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	buf := bytes.NewBuffer(make([]byte, 0, 128*1024*1024))

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())

	return &ParquetStore{
		dir:        storeDir,
		tenantID:   tenantID,
		buf:        buf,
		bucket:     bucket,
		logger:     logger,
		logLineBuf: make([]*LogLine, 0, 1_000_100),
	}, nil
}

type LogLine struct {
	Labels             map[string]string `parquet:"labels"`
	StructuredMetadata map[string]string `parquet:"structured_metadata"`
	TimestampNanos     int64             `parquet:"timestamp"`
	Line               string            `parquet:"line"`
}

// Write implements Store
func (s *ParquetStore) Write(_ context.Context, streams []logproto.Stream) error {
	for _, stream := range streams {
		lbs, err := syntax.ParseLabels(stream.Labels)
		if err != nil {
			return fmt.Errorf("failed to parse labels: %w", err)
		}
		sort.Sort(lbs)
		for _, entry := range stream.Entries {
			var strmd labels.Labels
			for _, label := range entry.StructuredMetadata {
				strmd = append(strmd, labels.Label{Name: label.Name, Value: label.Value})
			}
			sort.Sort(strmd)
			s.logLineBuf = append(s.logLineBuf, &LogLine{
				Labels:             lbs.Map(),
				StructuredMetadata: strmd.Map(),
				TimestampNanos:     entry.Timestamp.UnixNano(),
				Line:               entry.Line,
			})
		}
	}

	if len(s.logLineBuf) > 400_000 {
		if err := s.flush(); err != nil {
			return fmt.Errorf("failed to flush: %w", err)
		}
	}

	return nil
}

func (s *ParquetStore) Querier() (logql.Querier, error) {
	return NewParquetQuerier(s.bucket, s.logger), nil
}

func (s *ParquetStore) flush() error {
	if len(s.logLineBuf) == 0 {
		return nil
	}
	keys := map[string]struct{}{}
	md := map[string]struct{}{}
	for _, logLine := range s.logLineBuf {
		for k := range logLine.Labels {
			keys[k] = struct{}{}
		}
		for k := range logLine.StructuredMetadata {
			md[k] = struct{}{}
		}
	}

	// Define the schema
	group := parquet.Group{}
	for k := range keys {
		group[fmt.Sprintf("label$%s", k)] = parquet.Compressed(parquet.String(), &parquet.Zstd)
	}
	for m := range md {
		group[fmt.Sprintf("md$%s", m)] = parquet.Compressed(parquet.String(), &parquet.Zstd)
	}
	group["timestamp"] = parquet.Encoded(parquet.Int(64), &parquet.DeltaBinaryPacked)
	group["line"] = parquet.Compressed(parquet.String(), &parquet.Zstd)

	schema := parquet.NewSchema("root", group)
	config, err := parquet.NewWriterConfig()
	if err != nil {
		return fmt.Errorf("failed to create writer config: %w", err)
	}
	config.Schema = schema

	columnWriter := parquet.NewWriter(s.buf, config)

	rows := make([]parquet.Row, 0, len(s.logLineBuf))
	for _, logLine := range s.logLineBuf {
		row := make(parquet.Row, 0, len(keys)+len(md)+2)

		for _, c := range schema.Columns() {
			name := c[0]
			if strings.Contains(name, "label$") {
				val, ok := logLine.Labels[strings.TrimPrefix(name, "label$")]
				if !ok {
					row = append(row, parquet.NullValue())
				} else {
					row = append(row, parquet.ValueOf(val))
				}
			} else if strings.Contains(name, "md$") {
				val, ok := logLine.StructuredMetadata[strings.TrimPrefix(name, "md$")]
				if !ok {
					row = append(row, parquet.NullValue())
				} else {
					row = append(row, parquet.ValueOf(val))
				}
			} else if name == "timestamp" {
				row = append(row, parquet.ValueOf(logLine.TimestampNanos))
			} else if name == "line" {
				row = append(row, parquet.ValueOf(logLine.Line))
			}
		}
		rows = append(rows, row)
	}

	writers := columnWriter.ColumnWriters()
	for i, writer := range writers {
		vals := []parquet.Value{}
		for _, row := range rows {
			vals = append(vals, row[i])
		}

		for i := 0; i < len(vals); i += 10000 {
			_, err := writer.WriteRowValues(vals[i:min(i+10000, len(vals))])
			if err != nil {
				name := schema.Columns()[0]
				return fmt.Errorf("failed to write row values: %w, column: %s", err, name)
			}
		}
	}
	columnWriter.Flush()
	columnWriter.Close()

	s.bucket.Upload(context.Background(), fmt.Sprintf("tenant-%s/logs-%d.parquet", s.tenantID, s.flushCount), s.buf)
	s.buf.Reset()
	s.logLineBuf = s.logLineBuf[:0]
	s.flushCount++
	return nil
}

// Name implements Store
func (s *ParquetStore) Name() string {
	return "parquet"
}

// Close flushes any remaining data and closes resources
func (s *ParquetStore) Close() error {
	// Flush any remaining data
	if err := s.flush(); err != nil {
		return fmt.Errorf("failed to flush remaining data: %w", err)
	}
	return nil
}
