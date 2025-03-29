package arrowtest

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/csv"
)

// ReadCSV reads CSV data from in and returns an Arrow record. Empty fields in
// the CSV data are represented as NULL values in the Arrow record, alongside
// instances of "null" or "NULL."
//
// All rows in the CSV are returned as a single Arrow record. Release must be
// called on the returned record to free it.
//
// Additional options can be passed to control or override the behavior of the
// CSV reader.
//
// The input CSV data is sanitized with [SanitizeCSV] before being parsed.
func ReadCSV(in string, schema *arrow.Schema, option ...csv.Option) (arrow.Record, error) {
	in = SanitizeCSV(in)

	options := []csv.Option{csv.WithChunk(-1), csv.WithNullReader(true)}
	options = append(options, option...)

	r := csv.NewReader(strings.NewReader(in), schema, options...)
	if !r.Next() {
		return nil, r.Err()
	}
	defer r.Release()

	rec := r.Record()
	rec.Retain()
	return rec, nil
}

// WriteCSV writes the Arrow records as a CSV string. Nulls in the Arrow
// record are written as "NULL."
func WriteCSV(schema *arrow.Schema, recs ...arrow.Record) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b, schema)

	for _, rec := range recs {
		if err := w.Write(rec); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

// SanitizeCSV sanitizes CSV strings by:
//
//  1. Removing all empty lines.
//  2. Removing all leading and trailing whitespace from each line.
func SanitizeCSV(in string) string {
	var sb strings.Builder

	s := bufio.NewScanner(strings.NewReader(in))
	for s.Scan() {
		line := bytes.TrimSpace(s.Bytes())
		if len(line) == 0 {
			continue
		}
		fmt.Fprintln(&sb, string(line))
	}

	return sb.String()
}
