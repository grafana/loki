package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/parquet-go/parquet-go"
)

// factRow is one exported row: the join between a KindLabel posting and the
// logs section it references. All fields are sourced directly from
// [postings.Row] decoded in foldSection; no additional I/O is needed.
type factRow struct {
	Tenant           string `parquet:"tenant,dict"`
	IndexObject      string `parquet:"index_object,dict"`
	IndexSection     int64  `parquet:"index_section"`
	Compacted        bool   `parquet:"compacted"`
	ColumnName       string `parquet:"column_name,dict"`
	LabelValue       string `parquet:"label_value,dict"`
	LogsObject       string `parquet:"logs_object,dict"`
	LogsSection      int64  `parquet:"logs_section"`
	StreamRefs       int64  `parquet:"stream_refs"`
	UncompressedSize int64  `parquet:"uncompressed_size"`
}

// csvHeader lists the CSV column names in the same order as [factRow.record].
var csvHeader = []string{
	"tenant",
	"index_object",
	"index_section",
	"compacted",
	"column_name",
	"label_value",
	"logs_object",
	"logs_section",
	"stream_refs",
	"uncompressed_size",
}

// record converts a factRow to a CSV record. Column order matches csvHeader.
func (r factRow) record() []string {
	return []string{
		r.Tenant,
		r.IndexObject,
		strconv.FormatInt(r.IndexSection, 10),
		strconv.FormatBool(r.Compacted),
		r.ColumnName,
		r.LabelValue,
		r.LogsObject,
		strconv.FormatInt(r.LogsSection, 10),
		strconv.FormatInt(r.StreamRefs, 10),
		strconv.FormatInt(r.UncompressedSize, 10),
	}
}

// factSink is the write interface for raw fact export. Implementations are
// safe for concurrent use.
type factSink interface {
	write(rows []factRow) error
	close() error
}

// parquetFactSink writes facts to a Parquet file.
type parquetFactSink struct {
	file   *os.File
	writer *parquet.GenericWriter[factRow]
}

func newParquetFactSink(path string) (*parquetFactSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating parquet file %s: %w", path, err)
	}
	schema := parquet.SchemaOf(new(factRow))
	w := parquet.NewGenericWriter[factRow](f, schema)
	return &parquetFactSink{file: f, writer: w}, nil
}

func (s *parquetFactSink) write(rows []factRow) error {
	_, err := s.writer.Write(rows)
	return err
}

func (s *parquetFactSink) close() error {
	if err := s.writer.Close(); err != nil {
		_ = s.file.Close()
		return fmt.Errorf("closing parquet writer: %w", err)
	}
	return s.file.Close()
}

// csvFactSink writes facts to a CSV file.
type csvFactSink struct {
	file   *os.File
	writer *csv.Writer
}

func newCSVFactSink(path string) (*csvFactSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating csv file %s: %w", path, err)
	}
	w := csv.NewWriter(f)
	if err := w.Write(csvHeader); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing csv header: %w", err)
	}
	return &csvFactSink{file: f, writer: w}, nil
}

func (s *csvFactSink) write(rows []factRow) error {
	for _, r := range rows {
		if err := s.writer.Write(r.record()); err != nil {
			return err
		}
	}
	return nil
}

func (s *csvFactSink) close() error {
	s.writer.Flush()
	if err := s.writer.Error(); err != nil {
		_ = s.file.Close()
		return fmt.Errorf("flushing csv writer: %w", err)
	}
	return s.file.Close()
}

// multiSink fans out writes to multiple sinks and serializes concurrent calls
// with a mutex. parquet-go's GenericWriter is not safe for concurrent use.
type multiSink struct {
	mu    sync.Mutex
	sinks []factSink
}

func (m *multiSink) write(rows []factRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sinks {
		if err := s.write(rows); err != nil {
			return err
		}
	}
	return nil
}

func (m *multiSink) close() error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// newFactSink creates a factSink that writes to one or both of a Parquet and
// CSV file, determined by format ("parquet", "csv", or "both"). The files are
// named <pathPrefix>.parquet and/or <pathPrefix>.csv.
//
// The returned sink is safe for concurrent use.
func newFactSink(pathPrefix, format string) (factSink, error) {
	var sinks []factSink

	if format == "parquet" || format == "both" {
		ps, err := newParquetFactSink(pathPrefix + ".parquet")
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, ps)
	}
	if format == "csv" || format == "both" {
		cs, err := newCSVFactSink(pathPrefix + ".csv")
		if err != nil {
			// Close already-opened sinks before returning.
			for _, s := range sinks {
				_ = s.close()
			}
			return nil, err
		}
		sinks = append(sinks, cs)
	}

	return &multiSink{sinks: sinks}, nil
}
