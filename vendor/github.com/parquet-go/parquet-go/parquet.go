// Copyright 2022 Twilio Inc.

// Package parquet is a library for working with parquet files. For an overview
// of Parquet's qualities as a storage format, see this blog post:
// https://blog.twitter.com/engineering/en_us/a/2013/dremel-made-simple-with-parquet
//
// Or see the Parquet documentation: https://parquet.apache.org/docs/
package parquet

import (
	"io"
	"os"
	"reflect"
)

// Read reads and returns rows from the parquet file in the given reader.
//
// The type T defines the type of rows read from r. T must be compatible with
// the file's schema or an error will be returned. The row type might represent
// a subset of the full schema, in which case only a subset of the columns will
// be loaded from r.
//
// This function is provided for convenience to facilitate reading of parquet
// files from arbitrary locations in cases where the data set fit in memory.
func Read[T any](r io.ReaderAt, size int64, options ...ReaderOption) (rows []T, err error) {
	config, err := NewReaderConfig(options...)
	if err != nil {
		return nil, err
	}
	file, err := OpenFile(r, size)
	if err != nil {
		return nil, err
	}
	rows = make([]T, file.NumRows())
	reader := NewGenericReader[T](file, config)
	n, err := reader.Read(rows)
	if err == io.EOF {
		err = nil
	}
	reader.Close()
	return rows[:n], err
}

// ReadFile reads rows of the parquet file at the given path.
//
// The type T defines the type of rows read from r. T must be compatible with
// the file's schema or an error will be returned. The row type might represent
// a subset of the full schema, in which case only a subset of the columns will
// be loaded from the file.
//
// This function is provided for convenience to facilitate reading of parquet
// files from the file system in cases where the data set fit in memory.
func ReadFile[T any](path string, options ...ReaderOption) (rows []T, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Read[T](f, s.Size())
}

// Write writes the given list of rows to a parquet file written to w.
//
// This function is provided for convenience to facilitate the creation of
// parquet files.
func Write[T any](w io.Writer, rows []T, options ...WriterOption) error {
	config, err := NewWriterConfig(options...)
	if err != nil {
		return err
	}
	writer := NewGenericWriter[T](w, config)
	if _, err := writer.Write(rows); err != nil {
		return err
	}
	return writer.Close()
}

// Write writes the given list of rows to a parquet file written to w.
//
// This function is provided for convenience to facilitate writing parquet
// files to the file system.
func WriteFile[T any](path string, rows []T, options ...WriterOption) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Write(f, rows, options...)
}

func atLeastOne(size int) int {
	return atLeast(size, 1)
}

func atLeast(size, least int) int {
	if size < least {
		return least
	}
	return size
}

func typeNameOf(t reflect.Type) string {
	s1 := t.String()
	s2 := t.Kind().String()
	if s1 == s2 {
		return s1
	}
	return s1 + " (" + s2 + ")"
}

func isZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
