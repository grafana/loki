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

// MixedRunIterator merges sorted runs, keeping at most one section reader open
// per run. Readers are opened only during iteration and closed on early stop.
// Inputs and their remaps must remain unchanged during iteration.
func MixedRunIterator(ctx context.Context, runs []Run, expectedSchema []string) result.Seq[logs.Record] {
	return result.Iter(func(yield func(logs.Record) bool) error {
		sequences := make([]*runSequence, 0, len(runs))
		bufferSize := max(128, 8192/max(1, len(runs)))
		for _, run := range runs {
			sequences = append(sequences, &runSequence{ctx: ctx, remaining: run, schema: expectedSchema, bufferSize: bufferSize})
		}
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
			if err := logs.DecodeRow(seq.current.section.Columns(), row, &record, sym); err != nil {
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
	ctx        context.Context
	remaining  Run
	schema     []string
	bufferSize int
	current    *sectionSequence
	err        error

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
		var opened bool
		opened, s.err = s.openNextSection()
		if s.err != nil {
			return true
		}
		if !opened {
			return false
		}
	}
}

// openNextSection releases the exhausted reader before opening its successor.
// It returns false, nil when there are no sections left.
func (s *runSequence) openNextSection() (bool, error) {
	s.Close()
	if len(s.remaining) == 0 {
		return false, nil
	}
	input := s.remaining[0]
	s.remaining = s.remaining[1:]
	seq, err := openRemappedSection(s.ctx, input, s.schema, s.bufferSize)
	if err != nil {
		return false, err
	}
	s.current = seq
	return true, nil
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
	if s.current != nil {
		s.current.Close()
		s.current = nil
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
