package dataobj

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
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

	// Labels of the stream.
	Labels labels.Labels
}

// StreamsReader reads the set of streams from an [Object].
type StreamsReader struct {
	obj *Object
	idx int

	predicate Predicate

	next func() (result.Result[streams.Stream], bool)
	stop func()
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
	if r.next != nil {
		return fmt.Errorf("cannot change predicate after reading has started")
	}

	r.predicate = p
	return nil
}

// Read reads up to the next len(s) streams from the reader and stores them
// into s. It returns the number of streams read and any error encountered. At
// the end of the stream section, Read returns 0, io.EOF.
func (r *StreamsReader) Read(ctx context.Context, s []Stream) (int, error) {
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
	//   of StreamsReader.
	//
	// These details can change internally without changing the API exposed by
	// StreamsReader, which is designed to permit efficient use in the future.

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

		stream, err := res.Value()
		if err != nil {
			return i, fmt.Errorf("reading stream: %w", err)
		}

		s[i] = Stream{
			ID:      stream.ID,
			MinTime: stream.MinTimestamp,
			MaxTime: stream.MaxTimestamp,
			Labels:  stream.Labels,
		}
	}

	return len(s), nil
}

func (r *StreamsReader) initIter(ctx context.Context) error {
	sec, err := r.findSection(ctx)
	if err != nil {
		return fmt.Errorf("finding section: %w", err)
	}

	if r.stop != nil {
		r.stop()
	}

	seq := streams.IterSection(ctx, r.obj.dec.StreamsDecoder(), sec)
	r.next, r.stop = result.Pull(seq)
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

func (r *StreamsReader) nextMatching() (result.Result[streams.Stream], bool) {
	if r.next == nil {
		return result.Result[streams.Stream]{}, false
	}

NextRow:
	res, ok := r.next()
	if !ok {
		return res, ok
	}

	stream, err := res.Value()
	if err != nil {
		return res, true
	}

	if !matchStreamsPredicate(r.predicate, stream) {
		goto NextRow
	}

	return res, true
}

func matchStreamsPredicate(p Predicate, stream streams.Stream) bool {
	if p == nil {
		return true
	}

	switch p := p.(type) {
	case AndPredicate[StreamsPredicate]:
		return matchStreamsPredicate(p.Left, stream) && matchStreamsPredicate(p.Right, stream)
	case OrPredicate[StreamsPredicate]:
		return matchStreamsPredicate(p.Left, stream) || matchStreamsPredicate(p.Right, stream)
	case NotPredicate[StreamsPredicate]:
		return !matchStreamsPredicate(p.Inner, stream)
	case TimeRangePredicate[StreamsPredicate]:
		// A stream matches if its time range overlaps with the query range
		return overlapsTimeRange(p, stream.MinTimestamp, stream.MaxTimestamp)
	case LabelMatcherPredicate:
		return stream.Labels.Get(p.Name) == p.Value
	case LabelFilterPredicate:
		return p.Keep(p.Name, stream.Labels.Get(p.Name))
	default:
		// Unsupported predicates should already be caught by
		// [StreamsReader.SetPredicate].
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func overlapsTimeRange[P Predicate](p TimeRangePredicate[P], start, end time.Time) bool {
	switch {
	case p.IncludeStart && p.IncludeEnd:
		return !end.Before(p.StartTime) && !start.After(p.EndTime)
	case p.IncludeStart && !p.IncludeEnd:
		return !end.Before(p.StartTime) && start.Before(p.EndTime)
	case !p.IncludeStart && p.IncludeEnd:
		return end.After(p.StartTime) && !start.After(p.EndTime)
	case !p.IncludeStart && !p.IncludeEnd:
		return end.After(p.StartTime) && start.Before(p.EndTime)
	default:
		panic("unreachable")
	}
}

func matchTimestamp[P Predicate](p TimeRangePredicate[P], ts time.Time) bool {
	switch {
	case p.IncludeStart && p.IncludeEnd:
		return !ts.Before(p.StartTime) && !ts.After(p.EndTime) // ts >= start && ts <= end
	case p.IncludeStart && !p.IncludeEnd:
		return !ts.Before(p.StartTime) && ts.Before(p.EndTime) // ts >= start && ts < end
	case !p.IncludeStart && p.IncludeEnd:
		return ts.After(p.StartTime) && !ts.After(p.EndTime) // ts > start && ts <= end
	case !p.IncludeStart && !p.IncludeEnd:
		return ts.After(p.StartTime) && ts.Before(p.EndTime) // ts > start && ts < end
	default:
		panic("unreachable")
	}
}

// Reset resets the StreamsReader with a new object and section index to read
// from. Reset allows reusing a StreamsReader without allocating a new one.
//
// Any set predicate is cleared when Reset is called.
//
// Reset may be called with a nil object and a negative section index to clear
// the StreamsReader without needing a new object.
func (r *StreamsReader) Reset(obj *Object, sectionIndex int) {
	if r.stop != nil {
		r.stop()
	}

	r.obj = obj
	r.idx = sectionIndex
	r.next = nil
	r.stop = nil
	r.predicate = nil
}
