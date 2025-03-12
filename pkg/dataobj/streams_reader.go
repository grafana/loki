package dataobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
)

// A Stream is an individual stream in a data object.
type Stream struct {
	// ID of the stream. Stream IDs are unique across all sections in an object,
	// but not across multiple objects.
	ID int64

	// MinTime and MaxTime denote the range of timestamps across all entries in
	// the stream.
	MinTime, MaxTime time.Time

	// UncompressedSize is the total size of all the log lines and structured metadata values in the stream
	UncompressedSize int64

	// Labels of the stream.
	Labels labels.Labels
}

// StreamsReader reads the set of streams from an [Object].
type StreamsReader struct {
	obj   *Object
	idx   int
	ready bool

	predicate StreamsPredicate

	buf []dataset.Row

	reader     *dataset.Reader
	columns    []dataset.Column
	columnDesc []*streamsmd.ColumnDesc
}

// NewStreamsReader creates a new StreamsReader that reads from the streams
// section of the given object.
func NewStreamsReader(obj *Object, sectionIndex int) *StreamsReader {
	var sr StreamsReader
	sr.Reset(obj, sectionIndex)
	return &sr
}

// SetPredicate sets the predicate to use for filtering logs. [LogsReader.Read]
// will only return logs for which the predicate passes.
//
// SetPredicate returns an error if the predicate is not supported by
// LogsReader.
//
// A predicate may only be set before reading begins or after a call to
// [StreamsReader.Reset].
func (r *StreamsReader) SetPredicate(p StreamsPredicate) error {
	if r.ready {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicate = p
	return nil
}

// Read reads up to the next len(s) streams from the reader and stores them
// into s. It returns the number of streams read and any error encountered. At
// the end of the stream section, Read returns 0, io.EOF.
func (r *StreamsReader) Read(ctx context.Context, s []Stream) (int, error) {
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
		readStream, err := streams.Decode(r.columnDesc, r.buf[i])
		if err != nil {
			return i, fmt.Errorf("decoding stream: %w", err)
		}

		s[i] = Stream{
			ID:               readStream.ID,
			MinTime:          readStream.MinTimestamp,
			MaxTime:          readStream.MaxTimestamp,
			UncompressedSize: readStream.UncompressedSize,
			Labels:           readStream.Labels,
		}
	}

	return n, nil
}

func (r *StreamsReader) initReader(ctx context.Context) error {
	dec := r.obj.dec.StreamsDecoder()
	sec, err := r.findSection(ctx)
	if err != nil {
		return fmt.Errorf("finding section: %w", err)
	}

	columnDescs, err := dec.Columns(ctx, sec)
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	dset := encoding.StreamsDataset(dec, sec)
	columns, err := result.Collect(dset.ListColumns(ctx))
	if err != nil {
		return fmt.Errorf("reading columns: %w", err)
	}

	readerOpts := dataset.ReaderOptions{
		Dataset:   dset,
		Columns:   columns,
		Predicate: translateStreamsPredicate(r.predicate, columns, columnDescs),

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

func (r *StreamsReader) findSection(ctx context.Context) (*filemd.SectionInfo, error) {
	si, err := r.obj.dec.Sections(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading sections: %w", err)
	}

	var n int

	for _, s := range si {
		if s.Type == filemd.SECTION_TYPE_STREAMS {
			if n == r.idx {
				return s, nil
			}
			n++
		}
	}

	return nil, fmt.Errorf("section index %d not found", r.idx)
}

// Reset resets the StreamsReader with a new object and section index to read
// from. Reset allows reusing a StreamsReader without allocating a new one.
//
// Any set predicate is cleared when Reset is called.
//
// Reset may be called with a nil object and a negative section index to clear
// the StreamsReader without needing a new object.
func (r *StreamsReader) Reset(obj *Object, sectionIndex int) {
	r.obj = obj
	r.idx = sectionIndex
	r.predicate = nil
	r.ready = false
	r.columns = nil
	r.columnDesc = nil

	// We leave r.reader as-is to avoid reallocating; it'll be reset on the first
	// call to Read.
}

func translateStreamsPredicate(p StreamsPredicate, columns []dataset.Column, columnDesc []*streamsmd.ColumnDesc) dataset.Predicate {
	if p == nil {
		return nil
	}

	switch p := p.(type) {
	case AndPredicate[StreamsPredicate]:
		return dataset.AndPredicate{
			Left:  translateStreamsPredicate(p.Left, columns, columnDesc),
			Right: translateStreamsPredicate(p.Right, columns, columnDesc),
		}

	case OrPredicate[StreamsPredicate]:
		return dataset.OrPredicate{
			Left:  translateStreamsPredicate(p.Left, columns, columnDesc),
			Right: translateStreamsPredicate(p.Right, columns, columnDesc),
		}

	case NotPredicate[StreamsPredicate]:
		return dataset.NotPredicate{
			Inner: translateStreamsPredicate(p.Inner, columns, columnDesc),
		}

	case TimeRangePredicate[StreamsPredicate]:
		minTimestamp := findColumnFromDesc(columns, columnDesc, func(desc *streamsmd.ColumnDesc) bool {
			return desc.Type == streamsmd.COLUMN_TYPE_MIN_TIMESTAMP
		})
		maxTimestamp := findColumnFromDesc(columns, columnDesc, func(desc *streamsmd.ColumnDesc) bool {
			return desc.Type == streamsmd.COLUMN_TYPE_MAX_TIMESTAMP
		})
		if minTimestamp == nil || maxTimestamp == nil {
			return dataset.FalsePredicate{}
		}
		return convertStreamsTimePredicate(p, minTimestamp, maxTimestamp)

	case LabelMatcherPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *streamsmd.ColumnDesc) bool {
			return desc.Type == streamsmd.COLUMN_TYPE_LABEL && desc.Info.Name == p.Name
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.EqualPredicate{
			Column: metadataColumn,
			Value:  dataset.StringValue(p.Value),
		}

	case LabelFilterPredicate:
		metadataColumn := findColumnFromDesc(columns, columnDesc, func(desc *streamsmd.ColumnDesc) bool {
			return desc.Type == streamsmd.COLUMN_TYPE_LABEL && desc.Info.Name == p.Name
		})
		if metadataColumn == nil {
			return dataset.FalsePredicate{}
		}
		return dataset.FuncPredicate{
			Column: metadataColumn,
			Keep: func(_ dataset.Column, value dataset.Value) bool {
				return p.Keep(p.Name, valueToString(value))
			},
		}

	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func convertStreamsTimePredicate(p TimeRangePredicate[StreamsPredicate], minColumn, maxColumn dataset.Column) dataset.Predicate {
	switch {
	case p.IncludeStart && p.IncludeEnd: // !max.Before(p.StartTime) && !min.After(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.NotPredicate{
				Inner: dataset.LessThanPredicate{
					Column: maxColumn,
					Value:  dataset.Int64Value(p.StartTime.UnixNano()),
				},
			},
			Right: dataset.NotPredicate{
				Inner: dataset.GreaterThanPredicate{
					Column: minColumn,
					Value:  dataset.Int64Value(p.EndTime.UnixNano()),
				},
			},
		}

	case p.IncludeStart && !p.IncludeEnd: // !max.Before(p.StartTime) && min.Before(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.NotPredicate{
				Inner: dataset.LessThanPredicate{
					Column: maxColumn,
					Value:  dataset.Int64Value(p.StartTime.UnixNano()),
				},
			},
			Right: dataset.LessThanPredicate{
				Column: minColumn,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}

	case !p.IncludeStart && p.IncludeEnd: // max.After(p.StartTime) && !min.After(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.GreaterThanPredicate{
				Column: maxColumn,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
			Right: dataset.NotPredicate{
				Inner: dataset.GreaterThanPredicate{
					Column: minColumn,
					Value:  dataset.Int64Value(p.EndTime.UnixNano()),
				},
			},
		}

	case !p.IncludeStart && !p.IncludeEnd: // max.After(p.StartTime) && min.Before(p.EndTime)
		return dataset.AndPredicate{
			Left: dataset.GreaterThanPredicate{
				Column: maxColumn,
				Value:  dataset.Int64Value(p.StartTime.UnixNano()),
			},
			Right: dataset.LessThanPredicate{
				Column: minColumn,
				Value:  dataset.Int64Value(p.EndTime.UnixNano()),
			},
		}

	default:
		panic("unreachable")
	}
}
