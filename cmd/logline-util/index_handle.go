package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

// indexHandle wraps a logline.Reader for use by CLI tools.
type indexHandle struct {
	path   string
	reader logline.Reader
	ver    string
}

// openIndex opens an index file and auto-detects its version.
func openIndex(path string) (*indexHandle, error) {
	r, ver, err := logline.OpenFile(path)
	if err != nil {
		return nil, err
	}
	return &indexHandle{path: path, reader: r, ver: ver}, nil
}

func (h *indexHandle) Close() error {
	return h.reader.Close()
}

func (h *indexHandle) version() string {
	return h.ver
}

func (h *indexHandle) documentCount() uint32 {
	return h.reader.ReadHeader().DocumentCount
}

func (h *indexHandle) newTermIterator() (format.TermIterator, error) {
	return h.reader.NewTermIterator()
}

// collectTerms reads all terms from the index (for benchmark sampling).
func (h *indexHandle) collectTerms() ([]string, error) {
	it, err := h.newTermIterator()
	if err != nil {
		return nil, err
	}
	var all []string
	for it.Next() {
		t := it.Term()
		all = append(all, strings.TrimRight(string(t[:]), "\x00"))
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

// openQueryFunc opens a fresh reader on the given file via a countingReaderAt
// and returns a query function plus a closer.
func (h *indexHandle) openQueryFunc(counter *countingReaderAt) (queryFn func([]string) error, closer func(), err error) {
	f, ok := counter.r.(*os.File)
	if !ok {
		return nil, nil, fmt.Errorf("counter must wrap an *os.File")
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}

	reader, _, _, err := logline.OpenReaderAt(counter, 0, fi.Size())
	if err != nil {
		return nil, nil, err
	}

	queryFn = func(terms []string) error {
		var result format.Bitmap
		result.MatchesAll = true
		for _, term := range terms {
			idx, err := reader.FindTerm(term)
			if err != nil {
				return err
			}
			if idx < 0 {
				return nil
			}
			res, err := reader.GetBitmap(idx)
			if err != nil {
				return err
			}
			result = result.And(res)
			if result.IsEmpty() {
				return nil
			}
		}
		return nil
	}

	return queryFn, func() { reader.Close() }, nil
}

// openForBenchmark creates a countingReaderAt for the index file
// and returns it along with a query function and closer.
func openForBenchmark(h *indexHandle) (counter *countingReaderAt, queryFn func([]string) error, closer func(), err error) {
	f, err := os.Open(h.path)
	if err != nil {
		return nil, nil, nil, err
	}

	counter = &countingReaderAt{r: f}
	queryFn, readerCloser, err := h.openQueryFunc(counter)
	if err != nil {
		f.Close()
		return nil, nil, nil, err
	}

	closer = func() {
		readerCloser()
		f.Close()
	}
	return counter, queryFn, closer, nil
}
