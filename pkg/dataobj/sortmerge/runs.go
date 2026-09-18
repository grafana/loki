package sortmerge

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// RemappedSection pairs a logs section with its source object's local-to-global
// stream IDs. Global IDs must be ranked in physical stream order.
type RemappedSection struct {
	Section *dataobj.Section
	Remap   map[int64]int64
}

// Run is a sequence of sections sorted by global stream ID ascending, then
// timestamp descending, including across section boundaries.
type Run []RemappedSection

func buildIteratorPerRun(ctx context.Context, runs []Run, expectedSchemaLabels []string) []*runSequence {
	sequences := make([]*runSequence, 0, len(runs))
	bufferSize := max(128, 8192/max(1, len(runs)))

	for _, run := range runs {
		sectionIterators := make([]sectionIterator, 0, len(run))
		for _, inputSection := range run {
			sectionIterators = append(sectionIterators, &lazySectionIterator{ctx: ctx, input: inputSection, schemaLabels: expectedSchemaLabels, bufferSize: bufferSize})
		}
		sequences = append(sequences, &runSequence{ctx: ctx, remaining: sectionIterators})
	}
	return sequences
}

// MixedRunIterator merges sorted runs, keeping at most one section reader open
// per run. Readers are opened only during iteration and closed on early stop.
// Inputs and their remaps must remain unchanged during iteration.
func MixedRunIterator(ctx context.Context, runs []Run, expectedSchemaLabels []string) result.Seq[logs.Record] {
	return result.Iter(func(yield func(logs.Record) bool) error {
		sequences := buildIteratorPerRun(ctx, runs, expectedSchemaLabels)

		maxValue := result.Value(dataset.Row{Index: math.MaxInt, Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), dataset.Int64Value(math.MinInt64),
		}})
		tree := loser.New(sequences, maxValue, (*runSequence).At, logs.CompareForSortOrder(logs.SortStreamASC), (*runSequence).Close)
		defer tree.Close()
		sym := symbolizer.New(1024, 100_000)
		for tree.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			seq := tree.Winner()
			row, err := seq.At().Value()
			if err != nil {
				return err
			}
			var record logs.Record
			if err := logs.DecodeRow(seq.current.Columns(), row, &record, sym); err != nil {
				return err
			}
			if !yield(record) {
				return nil
			}
		}
		return ctx.Err()
	})
}

// runSequence owns the active reader and releases it before opening its successor.
type runSequence struct {
	ctx       context.Context
	remaining []sectionIterator
	current   sectionIterator
	err       error

	// ordering verification
	lastID   int64
	lastTS   int64
	haveLast bool
}

func (s *runSequence) Next() bool {
	if s.err != nil {
		return false
	}
	for {
		if s.err = s.ctx.Err(); s.err != nil {
			return true
		}
		if s.current != nil && s.current.Next() {
			s.err = s.validateRow()
			return true // At delivers either the row or its error.
		}
		// Previous section was either nil or successfully emitted all rows without erroring, release it.
		if s.current != nil {
			s.current.Close()
			s.current = nil
		}
		if len(s.remaining) == 0 {
			return false
		}
		s.current = s.remaining[0]
		s.remaining[0] = nil
		s.remaining = s.remaining[1:]
	}
}

func (s *runSequence) validateRow() error {
	row, err := s.current.At().Value()
	if err != nil {
		return err
	}
	id, ts := row.Values[0].Int64(), row.Values[1].Int64()
	if s.haveLast && (id < s.lastID || (id == s.lastID && ts > s.lastTS)) {
		return fmt.Errorf("sort merge: run is not sorted by global stream ID and descending timestamp")
	}
	s.lastID, s.lastTS, s.haveLast = id, ts, true
	return nil
}

func (s *runSequence) At() result.Result[dataset.Row] {
	if s.err != nil {
		return result.Error[dataset.Row](s.err)
	}
	return s.current.At()
}

func (s *runSequence) Close() {
	s.remaining = nil
	if s.current != nil {
		s.current.Close()
		s.current = nil
	}
}

// sectionIterator starts reading on its first Next call; unopened children own no readers.
// Next returns true for a row or one terminal error exposed by At. After an
// error, Next returns false. Close releases the reader and is safe to repeat.
type sectionIterator interface {
	Next() bool
	At() result.Result[dataset.Row]
	Columns() []*logs.Column
	Close()
}

// lazySectionIterator defers opening and allocating row buffers until iteration starts.
type lazySectionIterator struct {
	ctx          context.Context
	input        RemappedSection
	schemaLabels []string
	bufferSize   int
	sequence     *sectionSequence
	err          error
}

func (s *lazySectionIterator) init() error {
	if s.sequence != nil {
		// already initialized
		return nil
	}

	seq, err := openRemappedSection(s.ctx, s.input, s.schemaLabels, s.bufferSize)
	if err != nil {
		return err
	}
	s.sequence = seq
	return nil
}

func (s *lazySectionIterator) Next() bool {
	if s.err != nil {
		return false
	}
	if err := s.init(); err != nil {
		s.err = err
		return true // At delivers the initialization error once.
	}

	if !s.sequence.Next() {
		return false
	}
	_, s.err = s.sequence.At().Value()
	return true
}

func (s *lazySectionIterator) At() result.Result[dataset.Row] {
	if s.err != nil {
		return result.Error[dataset.Row](s.err)
	}
	return s.sequence.At()
}

func (s *lazySectionIterator) Columns() []*logs.Column { return s.sequence.section.Columns() }

func (s *lazySectionIterator) Close() {
	if s.sequence != nil {
		s.sequence.Close()
		s.sequence = nil
	}
}

func openRemappedSection(ctx context.Context, input RemappedSection, schema []string, bufferSize int) (*sectionSequence, error) {
	if input.Section == nil || input.Remap == nil {
		return nil, fmt.Errorf("sort merge: section and stream remap are required")
	}
	sec, err := logs.Open(ctx, input.Section)
	if err != nil {
		return nil, fmt.Errorf("opening logs section: %w", err)
	}
	got, err := sec.SchemaLabels()
	if err != nil {
		return nil, fmt.Errorf("reading section schema labels: %w", err)
	}
	if !slices.Equal(got, schema) {
		return nil, fmt.Errorf("section schema %v does not match expected sort schema %v", got, schema)
	}
	seq, err := openSectionSequence(ctx, sec, bufferSize)
	if err != nil {
		return nil, err
	}
	seq.remap = input.Remap
	return seq, nil
}
