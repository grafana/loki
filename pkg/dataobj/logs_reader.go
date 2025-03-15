package dataobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
)

// A Record is an individual log record in a data object.
type Record struct {
	StreamID  int64         // StreamID associated with the log record.
	Timestamp time.Time     // Timestamp of the log record.
	Metadata  labels.Labels // Set of metadata associated with the log record.
	Line      []byte        // Line of the log record.
}

// LogsReader reads the set of logs from an [Object].
type LogsReader struct {
	obj   *Object
	idx   int
	ready bool

	matchIDs  map[int64]struct{}
	predicate LogsPredicate

	buf []dataset.Row

	reader     *dataset.Reader
	columns    []dataset.Column
	columnDesc []*logsmd.ColumnDesc
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
	if r.ready {
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

// SetPredicate sets the predicate to use for filtering logs. [LogsReader.Read]
// will only return logs for which the predicate passes.
//
// A predicate may only be set before reading begins or after a call to
// [LogsReader.Reset].
func (r *LogsReader) SetPredicate(p LogsPredicate) error {
	if r.ready {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicate = p
	return nil
}

// Read reads up to the next len(s) records from the reader and stores them
// into s. It returns the number of records read and any error encountered. At
// the end of the logs section, Read returns 0, io.EOF.
func (r *LogsReader) Read(ctx context.Context, s []Record) (int, error) {
	if r.obj == nil {
		return 0, io.EOF
	} else if r.idx < 0 {
		return 0, fmt.Errorf("invalid section index %d", r.idx)
	}

	if !r.ready {
		err := r.initReader(ctx)
		if err != nil {
			return 0, err
		}
	}

	r.buf = slices.Grow(r.buf, len(s))
	r.buf = r.buf[:len(s)]

	n, err := r.reader.Read(ctx, r.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("reading rows: %w", err)
	} else if n == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}

	for i := range r.buf[:n] {
		readRecord, err := logs.Decode(r.columnDesc, r.buf[i])
		if err != nil {
			return i, fmt.Errorf("decoding record: %w", err)
		}

		s[i] = Record{
			StreamID:  readRecord.StreamID,
			Timestamp: readRecord.Timestamp,
			Metadata:  readRecord.Metadata,
			Line:      readRecord.Line,
		}
	}

	return n, nil
}

func (r *LogsReader) initReader(ctx context.Context) error {
	dec := r.obj.dec.LogsDecoder()
	sec, err := r.findSection(ctx)
	if err != nil {
		return fmt.Errorf("finding section: %w", err)
	}

	columnDescs, err := dec.Columns(ctx, sec)
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	dset := encoding.LogsDataset(dec, sec)
	columns, err := result.Collect(dset.ListColumns(ctx))
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	// r.predicate doesn't contain mappings of stream IDs; we need to build
	// that as a separate predicate and AND them together.
	predicate := streamIDPredicate(maps.Keys(r.matchIDs), columns, columnDescs)
	if r.predicate != nil {
		predicate = dataset.AndPredicate{
			Left:  predicate,
			Right: translateLogsPredicate(r.predicate, columns, columnDescs),
		}
	}

	readerOpts := dataset.ReaderOptions{
		Dataset:   dset,
		Columns:   columns,
		Predicate: predicate,

		TargetCacheSize: 16_000_000, // Permit up to 16MB of cache pages.
	}

	if r.reader == nil {
		r.reader = dataset.NewReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}

	r.columnDesc = columnDescs
	r.columns = columns
	r.ready = true
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
// Any set predicate is cleared when Reset is called.
//
// Reset may be called with a nil object and a negative section index to clear
// the LogsReader without needing a new object.
func (r *LogsReader) Reset(obj *Object, sectionIndex int) {
	r.obj = obj
	r.idx = sectionIndex
	r.ready = false

	clear(r.matchIDs)
	r.predicate = nil

	r.columns = nil
	r.columnDesc = nil

	// We leave r.reader as-is to avoid reallocating; it'll be reset on the first
	// call to Read.
}

func streamIDPredicate(ids iter.Seq[int64], columns []dataset.Column, columnDesc []*logsmd.ColumnDesc) dataset.Predicate {
	var res dataset.Predicate

	streamIDColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
		return desc.Type == logsmd.COLUMN_TYPE_STREAM_ID
	})
	if streamIDColumn == nil {
		return dataset.FalsePredicate{}
	}

	for id := range ids {
		p := dataset.EqualPredicate{
			Column: streamIDColumn,
			Value:  dataset.Int64Value(id),
		}

		if res == nil {
			res = p
		} else {
			res = dataset.OrPredicate{
				Left:  res,
				Right: p,
			}
		}
	}

	return res
}

func translateLogsPredicate(p LogsPredicate, columns []dataset.Column, columnDesc []*logsmd.ColumnDesc) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndPredicate[LogsPredicate]:
		return dataset.AndPredicate{
			Left:  translateLogsPredicate(p.Left, columns, columnDesc),
			Right: translateLogsPredicate(p.Right, columns, columnDesc),
		}

	case OrPredicate[LogsPredicate]:
		return dataset.OrPredicate{
			Left:  translateLogsPredicate(p.Left, columns, columnDesc),
			Right: translateLogsPredicate(p.Right, columns, columnDesc),
		}

	case NotPredicate[LogsPredicate]:
		return dataset.NotPredicate{
			Inner: translateLogsPredicate(p.Inner, columns, columnDesc),
		}

	case TimeRangePredicate[LogsPredicate]:
		timeColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_TIMESTAMP
		})
		if timeColumn == nil {
			return dataset.FalsePredicate{}
		}
		return convertLogsTimePredicate(p, timeColumn)

	case MetadataMatcherPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_METADATA && desc.Info.Name == p.Key
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.EqualPredicate{
			Column: metadataColumn,
			Value:  dataset.StringValue(p.Value),
		}

	case MetadataFilterPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *logsmd.ColumnDesc) bool {
			return desc.Type == logsmd.COLUMN_TYPE_METADATA && desc.Info.Name == p.Key
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.FuncPredicate{
			Column: metadataColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				return p.Keep(p.Key, valueToString(value))
			},
		}

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertLogsTimePredicate(p TimeRangePredicate[LogsPredicate], column dataset.Column) dataset.Predicate {
	var start dataset.Predicate = dataset.GreaterThanPredicate{
		Column: column,
		Value:  dataset.Int64Value(p.StartTime.UnixNano()),
	}
	if p.IncludeStart {
		start = dataset.OrPredicate{
			Left: start,
			Right: dataset.EqualPredicate{
				Column: column,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
		}
	}

	var end dataset.Predicate = dataset.LessThanPredicate{
		Column: column,
		Value:  dataset.Int64Value(p.EndTime.UnixNano()),
	}
	if p.IncludeEnd {
		end = dataset.OrPredicate{
			Left: end,
			Right: dataset.EqualPredicate{
				Column: column,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}
	}

	return dataset.AndPredicate{
		Left:  start,
		Right: end,
	}
}

func findColumnFromDesc[Desc any](columns []dataset.Column, descs []Desc, check func(Desc) bool) dataset.Column {
	for i, desc := range descs {
		if check(desc) {
			return columns[i]
		}
	}
	return nil
}

func valueToString(value dataset.Value) string {
	switch value.Type() {
	case datasetmd.VALUE_TYPE_UNSPECIFIED:
		return ""
	case datasetmd.VALUE_TYPE_INT64:
		return strconv.FormatInt(value.Int64(), 10)
	case datasetmd.VALUE_TYPE_UINT64:
		return strconv.FormatUint(value.Uint64(), 10)
	case datasetmd.VALUE_TYPE_STRING:
		return value.String()
	default:
		panic(fmt.Sprintf("unsupported value type %s", value.Type()))
	}
}
