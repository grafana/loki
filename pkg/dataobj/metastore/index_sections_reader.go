package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/compute"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// indexSectionsReader combines pointer scanning and bloom filtering into a single reader.
// It reads section pointers matching stream matchers and time range, then applies
// bloom filter predicates to further filter the results.
type indexSectionsReader struct {
	logger log.Logger
	region *xcap.Region
	obj    *dataobj.Object

	// Stream matching configuration
	matchers []*labels.Matcher
	start    time.Time
	end      time.Time

	// Bloom filter predicates (will be filtered to remove stream labels after init)
	predicates []*labels.Matcher

	// Pointer scanning state
	initialized        bool
	matchingStreamIDs  []scalar.Scalar
	pointersSections   []*dataobj.Section
	pointersSectionIdx int
	pointersReader     *pointers.Reader
	cols               []*pointers.Column
	labelNamesByStream map[int64][]string

	// Stats
	sectionPointersRead         int64
	sectionPointersReadDuration time.Duration
	streamsReadDuration         time.Duration
	bloomRowsRead               uint64
}

func newIndexSectionsReader(
	obj *dataobj.Object,
	start, end time.Time,
	matchers []*labels.Matcher,
	predicates []*labels.Matcher,
	region *xcap.Region,
) *indexSectionsReader {
	// Only keep equal predicates for bloom filtering
	var equalPredicates []*labels.Matcher
	for _, p := range predicates {
		if p.Type == labels.MatchEqual {
			equalPredicates = append(equalPredicates, p)
		}
	}

	return &indexSectionsReader{
		obj:                obj,
		matchers:           matchers,
		predicates:         equalPredicates,
		start:              start,
		end:                end,
		region:             region,
		labelNamesByStream: make(map[int64][]string),
	}
}

func (r *indexSectionsReader) Read(ctx context.Context) (arrow.RecordBatch, error) {
	ctx = xcap.ContextWithRegion(ctx, r.region)

	if err := r.init(ctx); err != nil {
		return nil, err
	}

	if len(r.predicates) == 0 {
		return r.readPointers(ctx)
	}

	return r.readWithBloomFiltering(ctx)
}

func (r *indexSectionsReader) Close() {
	if r.pointersReader != nil {
		_ = r.pointersReader.Close()
	}
	if r.region != nil {
		r.region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(r.matchingStreamIDs))))
		r.region.Record(xcap.StatMetastoreStreamsReadTime.Observe(r.streamsReadDuration.Seconds()))
		r.region.Record(xcap.StatMetastoreSectionPointersRead.Observe(r.sectionPointersRead))
		r.region.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(r.sectionPointersReadDuration.Seconds()))
	}
}

func (r *indexSectionsReader) totalReadRows() uint64 {
	return r.bloomRowsRead
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

	allLabelNames := make([]string, 0, len(allLabelNamesMap))
	for labelName := range allLabelNamesMap {
		allLabelNames = append(allLabelNames, labelName)
	}

	return allLabelNames
}

func (r *indexSectionsReader) init(ctx context.Context) error {
	if r.initialized {
		return nil
	}

	if len(r.matchers) == 0 {
		return io.EOF
	}

	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	for _, section := range r.obj.Sections().Filter(pointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}
		r.pointersSections = append(r.pointersSections, section)
	}

	if len(r.pointersSections) == 0 {
		return io.EOF
	}

	// Find stream ids that satisfy the predicate and start/end, and populate labels
	if err := r.populateMatchingStreamsAndLabels(ctx); err != nil {
		return fmt.Errorf("creating matching stream ids: %w", err)
	}
	// After populating matching streams from all the predicates, filter them down to the bloom supported predicates only
	r.filterBloomPredicates()

	if r.matchingStreamIDs == nil {
		return io.EOF
	}

	r.pointersSectionIdx = -1
	if err := r.prepareForNextSection(ctx); err != nil {
		return err
	}

	r.initialized = true
	return nil
}

func (r *indexSectionsReader) populateMatchingStreamsAndLabels(ctx context.Context) error {
	defer func(start time.Time) {
		r.streamsReadDuration = time.Since(start)
	}(time.Now())

	sStart, sEnd := r.scalarTimestamps()

	predicateKeys := map[string]struct{}{}
	for _, predicate := range r.predicates {
		predicateKeys[predicate.Name] = struct{}{}
	}

	requestedColumnsFn := func(column *streams.Column) bool {
		_, presentInPredicates := predicateKeys[column.Name]
		return column.Type == streams.ColumnTypeLabel && presentInPredicates
	}

	err := forEachStreamWithColumns(ctx, r.obj, r.matchers, sStart, sEnd, requestedColumnsFn, func(streamID int64, columnValues map[string]string) {
		r.matchingStreamIDs = append(r.matchingStreamIDs, scalar.NewInt64Scalar(streamID))
		if len(columnValues) == 0 {
			return
		}

		columns := make([]string, 0, len(columnValues))
		for columnName := range columnValues {
			columns = append(columns, columnName)
		}
		r.labelNamesByStream[streamID] = columns
	})
	if err != nil {
		return fmt.Errorf("error iterating streams: %v", err)
	}

	return nil
}

func (r *indexSectionsReader) readPointers(ctx context.Context) (arrow.RecordBatch, error) {
	defer func(start time.Time) {
		r.sectionPointersReadDuration += time.Since(start)
	}(time.Now())

	for {
		rec, err := r.pointersReader.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		} else if (rec == nil || rec.NumRows() == 0) && errors.Is(err, io.EOF) {
			// Section fully read, proceed to next one
			if err := r.prepareForNextSection(ctx); err != nil {
				return nil, err
			}
			continue
		}

		r.sectionPointersRead += rec.NumRows()
		r.bloomRowsRead += uint64(rec.NumRows())
		return rec, nil
	}
}

func (r *indexSectionsReader) readWithBloomFiltering(ctx context.Context) (arrow.RecordBatch, error) {
	// Read all pointers first
	recs, matchedRows, err := r.readAllPointers(ctx)
	if err != nil {
		return nil, err
	}
	r.bloomRowsRead = uint64(matchedRows)
	if len(recs) == 0 {
		return nil, io.EOF
	}

	// Validate schema
	commonSchema, err := r.validateRecordsSchema(recs)
	if err != nil {
		return nil, fmt.Errorf("validate input schema: %w", err)
	}

	chunks := make([][]arrow.Array, commonSchema.NumFields())

	// Apply each predicate's bloom filter
	for _, predicate := range r.predicates {
		if matchedRows == 0 {
			level.Debug(utillog.WithContext(ctx, r.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
			return nil, io.EOF
		}

		// Find matched section keys for the predicate
		sColumnName := scalar.NewStringScalar(predicate.Name)
		matchedSectionKeys := make(map[SectionKey]struct{})
		err := forEachMatchedPointerSectionKey(ctx, r.obj, r.labelNamesByStream, sColumnName, predicate.Value, func(sk SectionKey) {
			matchedSectionKeys[sk] = struct{}{}
		})
		if err != nil {
			return nil, fmt.Errorf("reading section keys: %w", err)
		}

		if len(matchedSectionKeys) == 0 {
			return nil, io.EOF
		}

		matchedRows = 0
		for recIdx, rec := range recs {
			mask, err := r.buildKeepBitmask(rec, matchedSectionKeys)
			if err != nil {
				return nil, fmt.Errorf("build keep bitmask: %w", err)
			}

			for colIdx, col := range rec.Columns() {
				out, err := compute.FilterArray(ctx, col, mask, compute.FilterOptions{})
				if err != nil {
					return nil, err
				}
				chunks[colIdx] = append(chunks[colIdx], out)
			}
			matchedRows += chunks[0][recIdx].Len()
		}
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

func (r *indexSectionsReader) readAllPointers(ctx context.Context) ([]arrow.RecordBatch, int, error) {
	var recs []arrow.RecordBatch
	totalRows := 0

	defer func(start time.Time) {
		r.sectionPointersReadDuration += time.Since(start)
	}(time.Now())

	for {
		rec, err := r.pointersReader.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, fmt.Errorf("failed to read record: %w", err)
		}

		if rec != nil && rec.NumRows() > 0 {
			totalRows += int(rec.NumRows())
			r.sectionPointersRead += rec.NumRows()
			recs = append(recs, rec)
		}

		if errors.Is(err, io.EOF) {
			// Try to move to next section
			err := r.prepareForNextSection(ctx)
			if errors.Is(err, io.EOF) {
				// No more sections
				break
			}
			if err != nil {
				return nil, 0, err
			}
			continue
		}
	}

	return recs, totalRows, nil
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

func (r *indexSectionsReader) prepareForNextSection(ctx context.Context) error {
	for {
		skip, err := r.prepareForNextSectionOrSkip(ctx)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		return nil
	}
}

func (r *indexSectionsReader) prepareForNextSectionOrSkip(ctx context.Context) (bool, error) {
	r.pointersSectionIdx++
	if r.pointersSectionIdx >= len(r.pointersSections) {
		return false, io.EOF
	}

	if r.pointersReader != nil {
		_ = r.pointersReader.Close()
	}

	sec, err := pointers.Open(ctx, r.pointersSections[r.pointersSectionIdx])
	if err != nil {
		return false, fmt.Errorf("opening section: %w", err)
	}

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
		return false, fmt.Errorf("finding pointers columns: %w", err)
	}

	var (
		colStreamID     *pointers.Column
		colMinTimestamp *pointers.Column
		colMaxTimestamp *pointers.Column
		colPointerKind  *pointers.Column
	)

	for _, c := range cols {
		if c.Type == pointers.ColumnTypeStreamID {
			colStreamID = c
		}
		if c.Type == pointers.ColumnTypeMinTimestamp {
			colMinTimestamp = c
		}
		if c.Type == pointers.ColumnTypeMaxTimestamp {
			colMaxTimestamp = c
		}
		if c.Type == pointers.ColumnTypePointerKind {
			colPointerKind = c
		}
		if colStreamID != nil && colMinTimestamp != nil && colMaxTimestamp != nil && colPointerKind != nil {
			break
		}
	}

	if colStreamID == nil || colMinTimestamp == nil || colMaxTimestamp == nil || colPointerKind == nil {
		// Section has no rows with stream-based indices, skip it
		return true, nil
	}

	r.cols = cols

	sStart, sEnd := r.scalarTimestamps()
	r.pointersReader = pointers.NewReader(pointers.ReaderOptions{
		Columns: r.cols,
		Predicates: []pointers.Predicate{
			pointers.EqualPredicate{
				Column: colPointerKind,
				Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindStreamIndex)),
			},
			pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
			pointers.InPredicate{
				Column: colStreamID,
				Values: r.matchingStreamIDs,
			},
		},
		Allocator:            memory.DefaultAllocator,
		StreamIDToLabelNames: r.labelNamesByStream,
	})

	return false, nil
}

func (r *indexSectionsReader) scalarTimestamps() (*scalar.Timestamp, *scalar.Timestamp) {
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(r.start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(r.end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	return sStart, sEnd
}

var (
	_ ArrowRecordBatchReader = (*indexSectionsReader)(nil)
	_ bloomStatsProvider     = (*indexSectionsReader)(nil)
)

type bloomStatsProvider interface {
	totalReadRows() uint64
}

func forEachMatchedPointerSectionKey(
	ctx context.Context,
	object *dataobj.Object,
	labelNamesByStream map[int64][]string,
	columnName scalar.Scalar,
	matchColumnValue string,
	f func(key SectionKey),
) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	var reader pointers.Reader
	defer reader.Close()

	const batchSize = 128
	buf := make([]pointers.SectionPointer, batchSize)

	for _, section := range object.Sections().Filter(pointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := pointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
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
			return fmt.Errorf("finding pointers columns: %w", err)
		}

		var (
			colColumnName  *pointers.Column
			colBloom       *pointers.Column
			colPointerKind *pointers.Column
		)

		for _, c := range pointerCols {
			if c.Type == pointers.ColumnTypeColumnName {
				colColumnName = c
			}
			if c.Type == pointers.ColumnTypeValuesBloomFilter {
				colBloom = c
			}
			if c.Type == pointers.ColumnTypePointerKind {
				colPointerKind = c
			}
			if colColumnName != nil && colBloom != nil && colPointerKind != nil {
				break
			}
		}

		if colColumnName == nil || colBloom == nil || colPointerKind == nil {
			// the section has no rows for blooms and can be ignored completely
			continue
		}

		reader.Reset(
			pointers.ReaderOptions{
				Columns:              pointerCols,
				StreamIDToLabelNames: labelNamesByStream,
				Predicates: []pointers.Predicate{
					pointers.EqualPredicate{
						Column: colPointerKind,
						Value:  scalar.NewInt64Scalar(int64(pointers.PointerKindColumnIndex)),
					},
					pointers.WhereBloomFilterMatches(colColumnName, colBloom, columnName, matchColumnValue),
				},
				Allocator: memory.DefaultAllocator,
			},
		)

		for {
			rec, readErr := reader.Read(ctx, batchSize)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("reading record batch: %w", readErr)
			}

			if rec != nil && rec.NumRows() > 0 {
				num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSectionKey)
				if err != nil {
					return err
				}
				for i := range num {
					sk := SectionKey{
						ObjectPath: buf[i].Path,
						SectionIdx: buf[i].Section,
					}
					f(sk)
				}
			}

			if errors.Is(readErr, io.EOF) {
				break
			}
		}
	}

	return nil
}
