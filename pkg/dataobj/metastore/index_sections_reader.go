package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type bloomStatsProvider interface {
	totalReadRows() uint64
}

// indexSectionsReader combines pointer scanning and bloom filtering into a single reader.
// It reads section pointers matching stream matchers and time range, then applies
// bloom filter predicates to further filter the results.
type indexSectionsReader struct {
	logger log.Logger
	obj    *dataobj.Object

	// Stream matching configuration
	matchers []*labels.Matcher
	start    time.Time
	end      time.Time

	// Bloom filter predicates (will be filtered to remove stream labels after streams are resolved)
	predicates []*labels.Matcher

	// Reader state
	initialized        bool
	hasData            bool // Whether initialization pulled data to read
	readStreams        bool
	matchingStreamIDs  map[int64]struct{}
	labelNamesByStream map[int64][]string

	streamsReaders  []*streams.Reader
	pointersReaders []*pointers.Reader
	bloomReaders    []*pointers.Reader

	pointersReaderIdx int

	// Stats
	bloomRowsRead uint64

	// readSpan for recording observations, it is created once during init.
	readSpan *xcap.Span
}

var (
	_ ArrowRecordBatchReader = (*indexSectionsReader)(nil)
	_ bloomStatsProvider     = (*indexSectionsReader)(nil)
)

func newIndexSectionsReader(
	logger log.Logger,
	obj *dataobj.Object,
	start, end time.Time,
	matchers []*labels.Matcher,
	predicates []*labels.Matcher,
) *indexSectionsReader {
	// Only keep equal predicates for bloom filtering
	var equalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type == labels.MatchEqual {
			equalPredicates = append(equalPredicates, p)
		}
	}

	return &indexSectionsReader{
		logger:             logger,
		obj:                obj,
		matchers:           matchers,
		predicates:         equalPredicates,
		start:              start,
		end:                end,
		matchingStreamIDs:  make(map[int64]struct{}),
		labelNamesByStream: make(map[int64][]string),
	}
}

var errIndexSectionsReaderNotOpen = errors.New("index sections reader not opened")

func (r *indexSectionsReader) Open(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	if err := r.init(ctx); err != nil {
		return err
	}

	r.initialized = true
	return nil
}

func (r *indexSectionsReader) init(ctx context.Context) error {
	if len(r.matchers) == 0 {
		return nil
	}

	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	sStart, sEnd := r.scalarTimestamps()

	predicateKeys := make(map[string]struct{}, len(r.predicates))
	for _, predicate := range r.predicates {
		predicateKeys[predicate.Name] = struct{}{}
	}

	var (
		unopenedStreams  []*dataobj.Section
		unopenedPointers []*dataobj.Section
	)

	for _, section := range r.obj.Sections() {
		if section.Tenant != targetTenant {
			continue
		}

		switch {
		case streams.CheckSection(section):
			unopenedStreams = append(unopenedStreams, section)
		case pointers.CheckSection(section):
			unopenedPointers = append(unopenedPointers, section)
		}
	}

	r.streamsReaders = make([]*streams.Reader, len(unopenedStreams))
	r.pointersReaders = make([]*pointers.Reader, len(unopenedPointers))
	r.bloomReaders = make([]*pointers.Reader, len(unopenedPointers))

	g, ctx := errgroup.WithContext(ctx)

	for i, section := range unopenedStreams {
		g.Go(func() error {
			sec, err := streams.Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening streams section: %w", err)
			}

			reader, err := r.openStreamsReader(ctx, sec, sStart, sEnd, predicateKeys)
			if err != nil {
				return err
			}

			r.streamsReaders[i] = reader
			return nil
		})
	}

	for i, section := range unopenedPointers {
		g.Go(func() error {
			sec, err := pointers.Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening pointers section: %w", err)
			}

			sg, ctx := errgroup.WithContext(ctx)

			sg.Go(func() error {
				pointersReader, err := r.openStreamPointersReader(ctx, sec, sStart, sEnd)
				if err != nil {
					return err
				}

				r.pointersReaders[i] = pointersReader
				return nil
			})

			sg.Go(func() error {
				bloomReader, err := r.openBloomPointersReader(ctx, sec)
				if err != nil {
					return err
				}

				r.bloomReaders[i] = bloomReader
				return nil
			})

			return sg.Wait()
		})
	}

	if err := g.Wait(); err != nil {
		closeAll(r.streamsReaders)
		closeAll(r.pointersReaders)
		closeAll(r.bloomReaders)
		return err
	}

	return nil
}

type closable interface {
	comparable
	io.Closer
}

func closeAll[C closable](cs []C) {
	var zero C

	for _, c := range cs {
		if c == zero {
			continue
		}
		_ = c.Close()
	}
}

func (r *indexSectionsReader) scalarTimestamps() (*scalar.Timestamp, *scalar.Timestamp) {
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(r.start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(r.end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	return sStart, sEnd
}

func (r *indexSectionsReader) openStreamsReader(
	ctx context.Context,
	sec *streams.Section,
	sStart, sEnd *scalar.Timestamp,
	predicateKeys map[string]struct{},
) (*streams.Reader, error) {
	predicates, err := buildStreamReaderPredicate(sec, sStart, sEnd, r.matchers)
	if err != nil {
		return nil, err
	}

	targetColumns := make([]*streams.Column, 0, len(sec.Columns()))

	for _, column := range sec.Columns() {
		if column.Type == streams.ColumnTypeStreamID {
			targetColumns = append(targetColumns, column)
		} else if _, ok := predicateKeys[column.Name]; ok && column.Type == streams.ColumnTypeLabel {
			targetColumns = append(targetColumns, column)
		}
	}

	reader := streams.NewReader(streams.ReaderOptions{
		Columns:    targetColumns,
		Predicates: predicates,
		Allocator:  memory.DefaultAllocator,
	})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening streams reader: %w", err)
	}

	return reader, nil
}

func (r *indexSectionsReader) openStreamPointersReader(
	ctx context.Context,
	sec *pointers.Section,
	sStart, sEnd *scalar.Timestamp,
) (*pointers.Reader, error) {
	cols, err := findPointersColumnsByTypes(
		sec.Columns(),
		pointers.ColumnTypePath,
		pointers.ColumnTypeSection,
		pointers.ColumnTypePointerKind,
		pointers.ColumnTypeStreamID,
		pointers.ColumnTypeStreamIDRef,
		pointers.ColumnTypeMinTimestamp,
		pointers.ColumnTypeMaxTimestamp,
		pointers.ColumnTypeRowCount,
		pointers.ColumnTypeUncompressedSize,
	)
	if err != nil {
		return nil, fmt.Errorf("finding pointers columns: %w", err)
	}

	var (
		colStreamID     *pointers.Column
		colMinTimestamp *pointers.Column
		colMaxTimestamp *pointers.Column
		colPointerKind  *pointers.Column
	)
	for _, c := range cols {
		switch c.Type {
		case pointers.ColumnTypeStreamID:
			colStreamID = c
		case pointers.ColumnTypeMinTimestamp:
			colMinTimestamp = c
		case pointers.ColumnTypeMaxTimestamp:
			colMaxTimestamp = c
		case pointers.ColumnTypePointerKind:
			colPointerKind = c
		}
	}

	if colStreamID == nil || colMinTimestamp == nil || colMaxTimestamp == nil || colPointerKind == nil {
		// Section has no rows with stream-based indices, skip it
		return nil, nil
	}

	reader := pointers.NewReader(pointers.ReaderOptions{
		Columns: cols,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{
				Column: colPointerKind,
				Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindStreamIndex)),
			},
			pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
		},
		Allocator:            memory.DefaultAllocator,
		StreamIDToLabelNames: r.labelNamesByStream,
	})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening pointers reader: %w", err)
	}

	return reader, nil
}

func (r *indexSectionsReader) openBloomPointersReader(ctx context.Context, sec *pointers.Section) (*pointers.Reader, error) {
	if len(r.predicates) == 0 {
		return nil, nil
	}

	pointerCols, err := findPointersColumnsByTypes(
		sec.Columns(),
		pointers.ColumnTypePath,
		pointers.ColumnTypeSection,
		pointers.ColumnTypePointerKind,
		pointers.ColumnTypeColumnName,
		pointers.ColumnTypeValuesBloomFilter,
	)
	if err != nil {
		return nil, fmt.Errorf("finding bloom pointers columns: %w", err)
	}

	var (
		colPath        *pointers.Column
		colSection     *pointers.Column
		colColumnName  *pointers.Column
		colBloom       *pointers.Column
		colPointerKind *pointers.Column
	)

	for _, c := range pointerCols {
		switch c.Type {
		case pointers.ColumnTypePath:
			colPath = c
		case pointers.ColumnTypeSection:
			colSection = c
		case pointers.ColumnTypeColumnName:
			colColumnName = c
		case pointers.ColumnTypeValuesBloomFilter:
			colBloom = c
		case pointers.ColumnTypePointerKind:
			colPointerKind = c
		}
	}

	if colPath == nil || colSection == nil || colColumnName == nil || colBloom == nil || colPointerKind == nil {
		// The section has no rows for blooms and can be ignored completely
		return nil, nil
	}

	// Because a row only contains a bloom filter for one column, we must use OR
	// to try to find all the matching blooms and do an AND later based on the
	// record.
	//
	// If this ever changes to a wide table (one bloom column per indexed
	// column), the logic could be simplified here to AND the predicates and
	// drop the post-processing.
	bloomPredicates := make([]pointers.Predicate, 0, len(r.predicates))
	for _, predicate := range r.predicates {
		bloomPredicates = append(bloomPredicates, pointers.WhereBloomFilterMatches(
			colColumnName,
			colBloom,
			scalar.NewStringScalar(predicate.Name),
			predicate.Value,
		))
	}

	combinedBloomPredicate := bloomPredicates[0]
	for _, p := range bloomPredicates[1:] {
		combinedBloomPredicate = pointers.OrPredicate{Left: combinedBloomPredicate, Right: p}
	}

	reader := pointers.NewReader(pointers.ReaderOptions{
		Columns: pointerCols,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{
				Column: colPointerKind,
				Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindColumnIndex)),
			},
			combinedBloomPredicate,
		},
		Allocator: memory.DefaultAllocator,
	})
	if err := reader.Open(ctx); err != nil {
		return nil, fmt.Errorf("opening bloom pointers reader: %w", err)
	}

	return reader, nil
}

func (r *indexSectionsReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !r.initialized {
		return nil, errIndexSectionsReaderNotOpen
	}

	if r.readSpan == nil {
		ctx, r.readSpan = xcap.StartSpan(ctx, tracer, "metastore.indexSectionsReader.Read")
	} else {
		ctx = xcap.ContextWithSpan(ctx, r.readSpan)
	}

	if err := r.lazyReadStreams(ctx); err != nil {
		return nil, err
	} else if !r.hasData {
		return nil, io.EOF
	}

	if len(r.predicates) == 0 {
		return r.readPointers(ctx)
	}
	return r.readWithBloomFiltering(ctx)
}

func (r *indexSectionsReader) lazyReadStreams(ctx context.Context) error {
	if r.readStreams {
		return nil
	}

	region := xcap.RegionFromContext(ctx)
	startTime := time.Now()
	defer func() {
		region.Record(xcap.StatMetastoreStreamsReadTime.Observe(time.Since(startTime).Seconds()))
	}()

	for _, sr := range r.streamsReaders {
		if sr == nil {
			// Sections can be skipped during Open if they don't have relevant data.
			continue
		}

		streamIDColumnIndex := slices.IndexFunc(sr.Columns(), func(c *streams.Column) bool { return c.Type == streams.ColumnTypeStreamID })
		if streamIDColumnIndex < 0 {
			return fmt.Errorf("streams schema missing stream_id column")
		}

		var requestedLabelNames []string
		for _, col := range sr.Columns() {
			if col.Name != "" && col.Type == streams.ColumnTypeLabel {
				requestedLabelNames = append(requestedLabelNames, col.Name)
			}
		}

		for {
			rec, err := sr.Read(ctx, 8192)
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading streams record batch: %w", err)
			}

			if rec != nil && rec.NumRows() > 0 {
				streamIDCol, ok := rec.Column(streamIDColumnIndex).(*array.Int64)
				if !ok {
					return fmt.Errorf("stream_id column has unexpected type %T", rec.Column(streamIDColumnIndex))
				}
				for i := 0; i < int(rec.NumRows()); i++ {
					if streamIDCol.IsNull(i) {
						continue
					}
					streamID := streamIDCol.Value(i)
					r.matchingStreamIDs[streamID] = struct{}{}
					r.addLabelNamesForStream(streamID, requestedLabelNames)
				}
			}

			if errors.Is(err, io.EOF) {
				break
			}
		}

		// Eagerly close the streams reader to release resources.
		if err := sr.Close(); err != nil {
			level.Warn(utillog.WithContext(ctx, r.logger)).Log("msg", "error closing streams reader", "err", err)
		}
	}

	region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(r.matchingStreamIDs))))

	r.filterBloomPredicates()
	r.readStreams = true
	r.hasData = len(r.matchingStreamIDs) > 0
	return nil
}

// filterLabelPredicates removes predicates that reference known stream labels.
// This prevents false negatives on structured metadata columns with the same name.
func (r *indexSectionsReader) filterBloomPredicates() {
	allLabelNames := r.allLabelNames()
	filtered := make([]*labels.Matcher, 0, len(r.predicates))
	for _, predicate := range r.predicates {
		isStreamLabel := slices.Contains(allLabelNames, predicate.Name)
		if !isStreamLabel {
			filtered = append(filtered, predicate)
		}
	}
	r.predicates = filtered
}

func (r *indexSectionsReader) allLabelNames() []string {
	allLabelNamesMap := map[string]struct{}{}
	for _, names := range r.labelNamesByStream {
		for _, name := range names {
			allLabelNamesMap[name] = struct{}{}
		}
	}
	return slices.Collect(maps.Keys(allLabelNamesMap))
}

func (r *indexSectionsReader) Close() {
	closeAll(r.streamsReaders)
	closeAll(r.pointersReaders)
	closeAll(r.bloomReaders)

	if r.readSpan != nil {
		r.readSpan.End()
	}
}

func (r *indexSectionsReader) addLabelNamesForStream(streamID int64, names []string) {
	if len(names) == 0 {
		return
	}

	existing := r.labelNamesByStream[streamID]
	for _, name := range names {
		if !slices.Contains(existing, name) {
			existing = append(existing, name)
		}
	}
	r.labelNamesByStream[streamID] = existing
}

// readPointers returns the next batch of pointers from the current pointers
// section.
func (r *indexSectionsReader) readPointers(ctx context.Context) (arrow.RecordBatch, error) {
	defer func(start time.Time) {
		r.readSpan.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(time.Since(start).Seconds()))
	}(time.Now())

	for r.pointersReaderIdx < len(r.pointersReaders) {
		pr := r.pointersReaders[r.pointersReaderIdx]
		if pr == nil {
			// Sections can be skipped during Open if they don't have relevant data.
			r.pointersReaderIdx++
			continue
		}

		streamIDColumnIndex := slices.IndexFunc(pr.Columns(), func(c *pointers.Column) bool { return c.Type == pointers.ColumnTypeStreamID })
		if streamIDColumnIndex < 0 {
			return nil, fmt.Errorf("pointers schema missing stream_id column")
		}

		rec, err := pr.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		if rec != nil && rec.NumRows() > 0 {
			filteredRec, matchedRows, err := r.filterPointersByMatchingStreamID(ctx, rec, streamIDColumnIndex)
			if err != nil {
				return nil, err
			}
			if matchedRows > 0 {
				r.readSpan.Record(xcap.StatMetastoreSectionPointersRead.Observe(int64(matchedRows)))
				r.bloomRowsRead += uint64(matchedRows)
				return filteredRec, nil
			}
		}

		if errors.Is(err, io.EOF) {
			// Eager close the pointers reader to release resources.
			if err := pr.Close(); err != nil {
				level.Warn(utillog.WithContext(ctx, r.logger)).Log("msg", "error closing pointers reader", "err", err)
			}
			r.pointersReaderIdx++
		}
	}

	return nil, io.EOF
}

func (r *indexSectionsReader) filterPointersByMatchingStreamID(
	ctx context.Context,
	rec arrow.RecordBatch,
	streamIDColumnIndex int,
) (arrow.RecordBatch, int, error) {
	if streamIDColumnIndex < 0 || rec == nil || rec.NumRows() == 0 {
		return rec, 0, nil
	}

	streamIDCol, ok := rec.Column(streamIDColumnIndex).(*array.Int64)
	if !ok {
		return nil, 0, fmt.Errorf("stream_id column has unexpected type %T", rec.Column(streamIDColumnIndex))
	}

	maskBuilder := array.NewBooleanBuilder(memory.DefaultAllocator)
	maskBuilder.Reserve(int(rec.NumRows()))

	matchedRows := 0
	for i := 0; i < int(rec.NumRows()); i++ {
		if streamIDCol.IsNull(i) {
			maskBuilder.Append(false)
			continue
		}
		_, keep := r.matchingStreamIDs[streamIDCol.Value(i)]
		maskBuilder.Append(keep)
		if keep {
			matchedRows++
		}
	}

	if matchedRows == 0 {
		return nil, 0, nil
	}
	if matchedRows == int(rec.NumRows()) {
		return rec, matchedRows, nil
	}

	mask := maskBuilder.NewBooleanArray()
	rec, err := compute.FilterRecordBatch(ctx, rec, mask, compute.DefaultFilterOptions())
	return rec, matchedRows, err
}

func (r *indexSectionsReader) readWithBloomFiltering(ctx context.Context) (arrow.RecordBatch, error) {
	recs, err := r.readAllPointers(ctx)
	if err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, io.EOF
	}

	commonSchema, err := r.validateRecordsSchema(recs)
	if err != nil {
		return nil, fmt.Errorf("validate input schema: %w", err)
	}

	matchedSectionKeys, err := r.readMatchedSectionKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading matched section keys: %w", err)
	}
	if len(matchedSectionKeys) == 0 {
		level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
		return nil, io.EOF
	}

	chunks := make([][]arrow.Array, commonSchema.NumFields())
	filteredRows := 0
	for _, rec := range recs {
		mask, err := r.buildKeepBitmask(rec, matchedSectionKeys)
		if err != nil {
			return nil, fmt.Errorf("build keep bitmask: %w", err)
		}

		for colIdx, col := range rec.Columns() {
			out, err := compute.FilterArray(ctx, col, mask, compute.FilterOptions{})
			if err != nil {
				return nil, err
			}
			if colIdx == 0 {
				filteredRows += out.Len()
			}
			chunks[colIdx] = append(chunks[colIdx], out)
		}
	}

	if filteredRows == 0 {
		return nil, io.EOF
	}

	outCols := make([]arrow.Array, commonSchema.NumFields())
	for i := range chunks {
		concat, err := array.Concatenate(chunks[i], memory.DefaultAllocator)
		if err != nil {
			return nil, err
		}
		outCols[i] = concat
	}

	return array.NewRecordBatch(commonSchema, outCols, int64(outCols[0].Len())), nil
}

func (r *indexSectionsReader) readAllPointers(ctx context.Context) ([]arrow.RecordBatch, error) {
	var recs []arrow.RecordBatch

	for {
		rec, err := r.readPointers(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			return recs, err
		} else if err != nil && errors.Is(err, io.EOF) {
			break
		}
		recs = append(recs, rec)
	}

	return recs, nil
}

func (r *indexSectionsReader) validateRecordsSchema(recs []arrow.RecordBatch) (*arrow.Schema, error) {
	if len(recs) == 0 {
		return nil, fmt.Errorf("no records")
	}

	commonSchema := recs[0].Schema()
	for i := 1; i < len(recs); i++ {
		if !recs[i].Schema().Equal(commonSchema) {
			return nil, fmt.Errorf("input records schema mismatch")
		}
	}

	var (
		foundPath    bool
		foundSection bool
	)
	for _, field := range commonSchema.Fields() {
		if foundPath && foundSection {
			break
		}
		switch pointers.ColumnTypeFromField(field) {
		case pointers.ColumnTypePath:
			foundPath = true
			if field.Type.ID() != arrow.STRING {
				return nil, fmt.Errorf("invalid path column type: %s", field.Type)
			}
		case pointers.ColumnTypeSection:
			foundSection = true
			if field.Type.ID() != arrow.INT64 {
				return nil, fmt.Errorf("invalid section column type: %s", field.Type)
			}
		default:
			continue
		}
	}

	if !foundPath || !foundSection {
		return nil, fmt.Errorf("record is missing mandatory fields path/section")
	}

	return commonSchema, nil
}

func (r *indexSectionsReader) readMatchedSectionKeys(ctx context.Context) (map[SectionKey]struct{}, error) {
	predicateIndexesByName := make(map[string][]int, len(r.predicates))
	for i, predicate := range r.predicates {
		predicateIndexesByName[predicate.Name] = append(predicateIndexesByName[predicate.Name], i)
	}

	sectionMatches := make(map[SectionKey]map[int]struct{})

	for _, br := range r.bloomReaders {
		if br == nil {
			// Open can leave nil readers when a section was skipped due to not
			// having relevant data.
			continue
		}

		var (
			pathColumnIndex       = slices.IndexFunc(br.Columns(), func(c *pointers.Column) bool { return c.Type == pointers.ColumnTypePath })
			sectionColumnIndex    = slices.IndexFunc(br.Columns(), func(c *pointers.Column) bool { return c.Type == pointers.ColumnTypeSection })
			columnNameColumnIndex = slices.IndexFunc(br.Columns(), func(c *pointers.Column) bool { return c.Type == pointers.ColumnTypeColumnName })
			bloomColumnIndex      = slices.IndexFunc(br.Columns(), func(c *pointers.Column) bool { return c.Type == pointers.ColumnTypeValuesBloomFilter })
		)

		if pathColumnIndex < 0 || sectionColumnIndex < 0 || columnNameColumnIndex < 0 || bloomColumnIndex < 0 {
			return nil, fmt.Errorf("bloom schema missing required columns")
		}

		for {
			rec, err := br.Read(ctx, 128)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("reading bloom record batch: %w", err)
			}

			if rec != nil && rec.NumRows() > 0 {
				var (
					pathCol       = rec.Column(pathColumnIndex).(*array.String)
					sectionCol    = rec.Column(sectionColumnIndex).(*array.Int64)
					columnNameCol = rec.Column(columnNameColumnIndex).(*array.String)
					bloomCol      = rec.Column(bloomColumnIndex).(*array.Binary)
				)

				for i := 0; i < int(rec.NumRows()); i++ {
					if pathCol.IsNull(i) || sectionCol.IsNull(i) || columnNameCol.IsNull(i) || bloomCol.IsNull(i) {
						continue
					}

					predicateIndexes := predicateIndexesByName[columnNameCol.Value(i)]
					if len(predicateIndexes) == 0 {
						continue
					}

					sectionKey := SectionKey{
						ObjectPath: pathCol.Value(i),
						SectionIdx: sectionCol.Value(i),
					}
					for _, predicateIndex := range predicateIndexes {
						if !bloomFilterMayContain(bloomCol.Value(i), r.predicates[predicateIndex].Value) {
							continue
						}

						matchedPredicates := sectionMatches[sectionKey]
						if matchedPredicates == nil {
							matchedPredicates = make(map[int]struct{}, len(r.predicates))
							sectionMatches[sectionKey] = matchedPredicates
						}
						matchedPredicates[predicateIndex] = struct{}{}
					}
				}
			}

			if err != nil && errors.Is(err, io.EOF) {
				break
			}
		}
	}

	matchedSectionKeys := make(map[SectionKey]struct{})
	for sectionKey, matchedPredicates := range sectionMatches {
		if len(matchedPredicates) == len(r.predicates) {
			matchedSectionKeys[sectionKey] = struct{}{}
		}
	}
	return matchedSectionKeys, nil
}

func bloomFilterMayContain(bloomBytes []byte, value string) bool {
	var bf bloom.BloomFilter
	if _, err := bf.ReadFrom(bytes.NewReader(bloomBytes)); err != nil {
		return true
	}
	return bf.TestString(value)
}

func (r *indexSectionsReader) buildKeepBitmask(rec arrow.RecordBatch, matchedSectionKeys map[SectionKey]struct{}) (arrow.Array, error) {
	maskB := array.NewBooleanBuilder(memory.DefaultAllocator)
	maskB.Reserve(int(rec.NumRows()))

	buf := make([]pointers.SectionPointer, rec.NumRows())
	num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSectionKey)
	if err != nil {
		return nil, err
	}

	for i := range num {
		sk := SectionKey{
			ObjectPath: buf[i].Path,
			SectionIdx: buf[i].Section,
		}
		_, keep := matchedSectionKeys[sk]
		maskB.Append(keep)
	}

	return maskB.NewBooleanArray(), nil
}

func (r *indexSectionsReader) totalReadRows() uint64 {
	return r.bloomRowsRead
}
