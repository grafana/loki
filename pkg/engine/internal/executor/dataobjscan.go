package executor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type dataobjScanOptions struct {
	StreamsSection *streams.Section
	LogsSection    *logs.Section
	StreamIDs      []int64                     // Stream IDs to match from logs sections.
	Predicates     []logs.Predicate            // Predicate to apply to the logs.
	Projections    []physical.ColumnExpression // Columns to include. An empty slice means all columns.

	BatchSize int64 // The buffer size for reading rows, derived from the engine batch size.
}

type dataobjScan struct {
	opts   dataobjScanOptions
	logger log.Logger
	region *xcap.Region

	initialized     bool
	initializedAt   time.Time
	streams         *streamsView
	streamsInjector *streamInjector
	reader          *logs.Reader
	desiredSchema   *arrow.Schema
}

var _ Pipeline = (*dataobjScan)(nil)

// newDataobjScanPipeline creates a new Pipeline which emits a single
// [arrow.RecordBatch] composed of the requested log section in a data object. Rows
// in the returned record are ordered by timestamp in the direction specified
// by opts.Direction.
func newDataobjScanPipeline(opts dataobjScanOptions, logger log.Logger, region *xcap.Region) *dataobjScan {
	return &dataobjScan{
		opts:   opts,
		logger: logger,
		region: region,
	}
}

func (s *dataobjScan) Open(ctx context.Context) error {
	return s.init(ctx)
}

func (s *dataobjScan) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !s.initialized {
		return nil, errPipelineNotOpen
	}
	return s.read(xcap.ContextWithRegion(ctx, s.region))
}

func (s *dataobjScan) init(ctx context.Context) error {
	if s.initialized {
		return nil
	}

	// [dataobjScan.initLogs] depends on the result of [dataobjScan.initStreams]
	// (to know whether label columns are needed), so we must initialize streams
	// first.
	if err := s.initStreams(ctx); err != nil {
		return fmt.Errorf("initializing streams: %w", err)
	} else if err := s.initLogs(ctx); err != nil {
		return fmt.Errorf("initializing logs: %w", err)
	}

	s.initialized = true
	s.initializedAt = time.Now().UTC()
	return nil
}

func (s *dataobjScan) initStreams(ctx context.Context) error {
	if s.opts.StreamsSection == nil {
		return fmt.Errorf("no streams section provided")
	}

	columnsToRead := projectedLabelColumns(s.opts.StreamsSection, s.opts.Projections)
	if len(columnsToRead) == 0 {
		s.streams = nil
		s.streamsInjector = nil
		return nil
	}

	s.streams = newStreamsView(s.opts.StreamsSection, &streamsViewOptions{
		StreamIDs:    s.opts.StreamIDs,
		LabelColumns: columnsToRead,
		BatchSize:    int(s.opts.BatchSize),
	})
	if err := s.streams.Open(ctx); err != nil {
		return fmt.Errorf("opening streams view: %w", err)
	}

	s.streamsInjector = newStreamInjector(s.streams)
	return nil
}

// projectedLabelColumns returns the label columns to read from the given
// streams section for the provided list of projections. If projections is
// empty, all stream label columns are returned.
//
// References to columns in projections that do not exist in the section are
// ignored.
//
// If projections is non-empty but contains no references to labels or
// ambiguous columns, projectedLabelColumns returns nil to indicate that no
// label columns are needed.
func projectedLabelColumns(sec *streams.Section, projections []physical.ColumnExpression) []*streams.Column {
	var found []*streams.Column

	// Special case: if projections is empty, we return all label columns. While
	// [streamsView] accepts a nil list to mean "all columns," we reserve nil
	// here to mean "we're not projecting stream labels at all."
	if len(projections) == 0 {
		for _, col := range sec.Columns() {
			if col.Type != streams.ColumnTypeLabel {
				continue
			}
			found = append(found, col)
		}
		return found
	}

	// Inefficient search. Will we have enough columns + projections such that
	// this needs to be optimized?
	for _, projection := range projections {
		expr, ok := projection.(*physical.ColumnExpr)
		if !ok {
			panic("invalid projection type, expected *physical.ColumnExpr")
		}

		// We're loading the streams section for joining stream labels into
		// records, so we only need to consider label and ambiguous columns here.
		if expr.Ref.Type != types.ColumnTypeLabel && expr.Ref.Type != types.ColumnTypeAmbiguous {
			continue
		}

		for _, col := range sec.Columns() {
			if col.Type != streams.ColumnTypeLabel {
				continue
			}

			if col.Name == expr.Ref.Column {
				found = append(found, col)
				break
			}
		}
	}

	return found
}

func (s *dataobjScan) initLogs(ctx context.Context) error {
	if s.opts.LogsSection == nil {
		return fmt.Errorf("no logs section provided")
	}

	predicates := s.opts.Predicates

	var columnsToRead []*logs.Column

	if s.streams != nil || len(s.opts.StreamIDs) > 0 {
		// We're reading sreams, so we need to include the stream ID column.
		var streamIDColumn *logs.Column

		for _, col := range s.opts.LogsSection.Columns() {
			if col.Type == logs.ColumnTypeStreamID {
				streamIDColumn = col
				columnsToRead = append(columnsToRead, streamIDColumn)
				break
			}
		}

		if streamIDColumn == nil {
			return fmt.Errorf("logs section does not contain stream ID column")
		}

		if len(s.opts.StreamIDs) > 0 {
			predicates = append([]logs.Predicate{
				logs.InPredicate{
					Column: streamIDColumn,
					Values: makeScalars(s.opts.StreamIDs),
				},
			}, predicates...)
		}
	}

	columnsToRead = append(columnsToRead, projectedLogsColumns(s.opts.LogsSection, s.opts.Projections)...)

	s.reader = logs.NewReader(logs.ReaderOptions{
		// TODO(rfratto): is it possible to hit an edge case where len(columnsToRead)
		// == 0, indicating that we don't need to read any logs at all? How should we
		// handle that?
		Columns: columnsToRead,

		Predicates: predicates,
		Allocator:  memory.DefaultAllocator,
	})
	if err := s.reader.Open(ctx); err != nil {
		return fmt.Errorf("opening logs reader: %w", err)
	}

	// Create the engine-compatible expected schema for the logs section.
	origSchema := s.reader.Schema()
	if got, want := origSchema.NumFields(), len(columnsToRead); got != want {
		return fmt.Errorf("logs.Reader returned schema with %d fields, expected %d", got, want)
	}

	// Convert the logs columns to engine-compatible fields.
	var desiredFields []arrow.Field
	for _, col := range columnsToRead {
		field, err := logsColumnToEngineField(col)
		if err != nil {
			return err
		}
		desiredFields = append(desiredFields, field)
	}

	s.desiredSchema = arrow.NewSchema(desiredFields, nil)
	return nil
}

func makeScalars[S ~[]E, E any](s S) []scalar.Scalar {
	scalars := make([]scalar.Scalar, len(s))
	for i, v := range s {
		scalars[i] = scalar.MakeScalar(v)
	}
	return scalars
}

// logsColumnToEngineField gets the engine-compatible [arrow.Field] for the
// given [logs.Column]. Returns an error if the column is not expected by the
// engine.
func logsColumnToEngineField(col *logs.Column) (arrow.Field, error) {
	switch col.Type {
	case logs.ColumnTypeStreamID:
		return semconv.FieldFromIdent(streamInjectorColumnIdent, true), nil

	case logs.ColumnTypeTimestamp:
		return semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, true), nil

	case logs.ColumnTypeMessage:
		return semconv.FieldFromIdent(semconv.ColumnIdentMessage, true), nil

	case logs.ColumnTypeMetadata:
		return semconv.FieldFromIdent(semconv.NewIdentifier(col.Name, types.ColumnTypeMetadata, types.Loki.String), true), nil
	}

	return arrow.Field{}, fmt.Errorf("unsupported logs column type %s", col.Type)
}

var logsColumnPrecedence = map[logs.ColumnType]int{
	logs.ColumnTypeInvalid:   0,
	logs.ColumnTypeStreamID:  1,
	logs.ColumnTypeMetadata:  2,
	logs.ColumnTypeTimestamp: 3,
	logs.ColumnTypeMessage:   4,
}

// projectedLogsColumns returns the section columns to read from the given logs
// section for the provided list of projections. If projections is empty, all
// section columns are returned (except for stream ID).
//
// References to columns in projections that do not exist in the section are
// ignored. If no projections reference any known column in the logs section,
// projectedLogsColumns returns nil.
//
// projectedLogsColumns never includes the stream ID column in its results, as
// projections can never reference a stream ID column.
func projectedLogsColumns(sec *logs.Section, projections []physical.ColumnExpression) []*logs.Column {
	var found []*logs.Column

	defer func() {
		// Use consistent sort order for columns. While at some point we may wish
		// to have a consistent order based on projections, that would need to be
		// handled at read time after we inject stream labels.
		slices.SortFunc(found, func(a, b *logs.Column) int {
			if a.Type != b.Type {
				// Sort by type precedence.
				return cmp.Compare(logsColumnPrecedence[a.Type], logsColumnPrecedence[b.Type])
			}
			return cmp.Compare(a.Name, b.Name)
		})
	}()

	// Special case: if projections is empty, we return all columns. While
	// [logs.Reader] accepts a nil list to mean "all columns," we reserve nil
	// here to mean "we're not projecting any columns at all."
	if len(projections) == 0 {
		for _, col := range sec.Columns() {
			if col.Type == logs.ColumnTypeStreamID {
				continue
			}
			found = append(found, col)
		}

		return found
	}

	// Inefficient search. Will we have enough columns + projections such that
	// this needs to be optimized?
NextProjection:
	for _, projection := range projections {
		expr, ok := projection.(*physical.ColumnExpr)
		if !ok {
			panic("invalid projection type, expected *physical.ColumnExpr")
		}

		// Ignore columns that cannot exist in the logs section.
		switch expr.Ref.Type {
		case types.ColumnTypeLabel, types.ColumnTypeParsed, types.ColumnTypeGenerated:
			continue NextProjection
		}

		for _, col := range sec.Columns() {
			switch {
			case expr.Ref.Type == types.ColumnTypeBuiltin && expr.Ref.Column == types.ColumnNameBuiltinTimestamp && col.Type == logs.ColumnTypeTimestamp:
				found = append(found, col)
				continue NextProjection

			case expr.Ref.Type == types.ColumnTypeBuiltin && expr.Ref.Column == types.ColumnNameBuiltinMessage && col.Type == logs.ColumnTypeMessage:
				found = append(found, col)
				continue NextProjection

			case expr.Ref.Type == types.ColumnTypeMetadata && col.Type == logs.ColumnTypeMetadata && col.Name == expr.Ref.Column:
				found = append(found, col)
				continue NextProjection

			case expr.Ref.Type == types.ColumnTypeAmbiguous && col.Type == logs.ColumnTypeMetadata && col.Name == expr.Ref.Column:
				found = append(found, col)
				continue NextProjection
			}
		}
	}

	return found
}

// read reads the entire section into memory and generates an [arrow.RecordBatch]
// from the data. It returns an error if reading a section resulted in an
// error.
func (s *dataobjScan) read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := s.reader.Read(ctx, int(s.opts.BatchSize))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	} else if (rec == nil || rec.NumRows() == 0) && errors.Is(err, io.EOF) {
		return nil, EOF
	}

	if rec.NumRows() == 0 {
		return rec, nil
	}

	// Update the schema of the record to match the schema the engine expects.
	rec, err = changeSchema(rec, s.desiredSchema)
	if err != nil {
		return nil, fmt.Errorf("changing schema: %w", err)
	}

	if s.streamsInjector == nil {
		// No streams injector needed, so we return the record as-is.
		return rec, nil
	}

	return s.streamsInjector.Inject(rec)
}

// Close closes s and releases all resources.
func (s *dataobjScan) Close() {
	if s.region != nil {
		s.region.End()
	}
	if s.streams != nil {
		s.streams.Close()
	}
	if s.reader != nil {
		_ = s.reader.Close()
	}

	s.initialized = false
	s.streams = nil
	s.streamsInjector = nil
	s.reader = nil
	s.region = nil
}

// Region implements RegionProvider.
func (s *dataobjScan) Region() *xcap.Region {
	return s.region
}
