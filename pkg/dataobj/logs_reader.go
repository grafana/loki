package dataobj

import (
	"context"
	"fmt"
	"io"
	"iter"
	"sort"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
)

// Predicates for reading logs.
type (
	// MetadataMatcher is a predicate for matching metadata in a logs section.
	// MetadataMatcher predicates assert that a metadata entry named Name exists
	// and its value is set to Value.
	//
	// For equality matches, MetadataMatcher should always be used;
	// MetadataMatchers can translate into more efficient filter operations than
	// a [MetadataFilter] can.
	MetadataMatcher struct{ Name, Value string }

	// MetadataFilter is a predicate for matching metadata in a logs section.
	// MetadataFilter predicates return a true value when the combination of the
	// provided metadata entry name and value should be included in the result.
	//
	// MetadataFilter predicates should be only used for more complex filtering;
	// for equality matches, [MetadataMatcher]s are more efficient.
	MetadataFilter func(name, value string) bool
)

// A Record is an individual log record in a data object.
type Record struct {
	StreamID  int64         // StreamID associated with the log record.
	Timestamp time.Time     // Timestamp of the log record.
	Metadata  labels.Labels // Set of metadata associated with the log record.
	Line      string        // Line of the log record.
}

// LogsReader reads the set of logs from an [Object].
type LogsReader struct {
	obj *Object
	idx int

	matchers map[string]string
	filters  map[string]MetadataFilter
	matchIDs map[int64]struct{}

	next func() (result.Result[logs.Record], bool)
	stop func()
}

// NewLogsReader creates a new LogsReader that reads from the logs section of
// the given object.
func NewLogsReader(obj *Object, sectionIndex int) *LogsReader {
	var lr LogsReader
	lr.Reset(obj, sectionIndex)
	return &lr
}

// MatchStreams provides a sequence of stream IDs for the logs reader to match.
// [LogsReader.Read] will only return logs for the provided stream IDs.
//
// MatchStreams may be called multiple times to match multiple sets of streams.
//
// MatchStreams may only be called before reading begins or after a call to
// [LogsReader.Reset].
func (r *LogsReader) MatchStreams(ids iter.Seq[int64]) error {
	if r.next != nil {
		return fmt.Errorf("cannot change matched streams after reading has started")
	}

	if r.matchIDs == nil {
		r.matchIDs = make(map[int64]struct{})
	}
	for id := range ids {
		r.matchIDs[id] = struct{}{}
	}
	return nil
}

// AddMetadataMatcher adds a metadata matcher to the LogsReader.
// [LogsReader.Read] will only return logs for which the metadata matcher
// predicate passes.
//
// AddMetadataMatcher may only be called before reading begins or after a call
// to [LogsReader.Reset].
func (r *LogsReader) AddMetadataMatcher(m MetadataMatcher) error {
	if r.next != nil {
		return fmt.Errorf("cannot add metadata matcher after reading has started")
	}

	if r.matchers == nil {
		r.matchers = make(map[string]string)
	}
	r.matchers[m.Name] = m.Value
	return nil
}

// AddMetadataFilter adds a metadata filter to the LogsReader.
// [LogsReader.Read] will only return records for which the metadata filter
// predicate passes. The filter f will be called with the provided key to allow
// the same function to be reused for multiple keys.
//
// AddMetadataFilter may only be called before reading begins or after a call
// to [LogsReader.Reset].
func (r *LogsReader) AddMetadataFilter(key string, f MetadataFilter) error {
	if r.next != nil {
		return fmt.Errorf("cannot add metadata filter after reading has started")
	}

	if r.filters == nil {
		r.filters = make(map[string]MetadataFilter)
	}
	r.filters[key] = f
	return nil
}

// Read reads up to the next len(s) records from the reader and stores them
// into s. It returns the number of records read and any error encountered. At
// the end of the logs section, Read returns 0, io.EOF.
func (r *LogsReader) Read(ctx context.Context, s []Record) (int, error) {
	// TODO(rfratto): The implementation below is the initial, naive approach. It
	// lacks a few features that will be needed at scale:
	//
	// * Read columns/pages in batches of len(s), rather than one row at a time,
	//
	// * Add page-level filtering based on min/max page values to quickly filter
	//   out batches of rows without needing to download or decode them.
	//
	// * Download pages in batches, rather than one at a time.
	//
	// * Only download/decode non-predicate columns following finding rows that
	//   match all predicate columns.
	//
	// * Reuse as much memory as possible from a combination of s and the state
	//   of LogsReader.
	//
	// These details can change internally without changing the API exposed by
	// LogsReader, which is designed to permit efficient use in the future.

	if r.obj == nil {
		return 0, io.EOF
	} else if r.idx < 0 {
		return 0, fmt.Errorf("invalid section index %d", r.idx)
	}

	if r.next == nil {
		err := r.initIter(ctx)
		if err != nil {
			return 0, err
		}
	}

	for i := range s {
		res, ok := r.nextMatching()
		if !ok {
			return i, io.EOF
		}

		record, err := res.Value()
		if err != nil {
			return i, fmt.Errorf("reading record: %w", err)
		}

		s[i] = Record{
			StreamID:  record.StreamID,
			Timestamp: record.Timestamp,
			Metadata:  convertMetadata(record.Metadata),
			Line:      record.Line,
		}
	}

	return len(s), nil
}

func (r *LogsReader) initIter(ctx context.Context) error {
	sec, err := r.findSection(ctx)
	if err != nil {
		return fmt.Errorf("finding section: %w", err)
	}

	if r.stop != nil {
		r.stop()
	}

	seq := logs.IterSection(ctx, r.obj.dec.LogsDecoder(), sec)
	r.next, r.stop = result.Pull(seq)
	return nil
}

func (r *LogsReader) findSection(ctx context.Context) (*filemd.SectionInfo, error) {
	si, err := r.obj.dec.Sections(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading sections: %w", err)
	}

	var n int

	for _, s := range si {
		if s.Type == filemd.SECTION_TYPE_LOGS {
			if n == r.idx {
				return s, nil
			}
			n++
		}
	}

	return nil, fmt.Errorf("section index %d not found", r.idx)
}

func (r *LogsReader) nextMatching() (result.Result[logs.Record], bool) {
	if r.next == nil {
		return result.Result[logs.Record]{}, false
	}

NextRow:
	res, ok := r.next()
	if !ok {
		return res, ok
	}

	record, err := res.Value()
	if err != nil {
		return res, true
	}

	if r.matchIDs != nil {
		if _, ok := r.matchIDs[record.StreamID]; !ok {
			goto NextRow
		}
	}

	for key, value := range r.matchers {
		if getMetadata(record.Metadata, key) != value {
			goto NextRow
		}
	}

	for key, filter := range r.filters {
		if !filter(key, getMetadata(record.Metadata, key)) {
			goto NextRow
		}
	}

	return res, true
}

func getMetadata(md push.LabelsAdapter, key string) string {
	for _, l := range md {
		if l.Name == key {
			return l.Value
		}
	}

	return ""
}

func convertMetadata(md push.LabelsAdapter) labels.Labels {
	l := make(labels.Labels, 0, len(md))
	for _, label := range md {
		l = append(l, labels.Label{Name: label.Name, Value: label.Value})
	}
	sort.Sort(l)
	return l
}

// Reset resets the LogsReader with a new object and section index to read
// from. Reset allows reusing a LogsReader without allocating a new one.
//
// Reset may be called with a nil object and a negative section index to clear
// the LogsReader without needing a new object.
func (r *LogsReader) Reset(obj *Object, sectionIndex int) {
	if r.stop != nil {
		r.stop()
	}

	r.obj = obj
	r.idx = sectionIndex
	r.next = nil
	r.stop = nil

	clear(r.matchers)
	clear(r.filters)
	clear(r.matchIDs)
}
