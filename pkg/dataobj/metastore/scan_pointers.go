package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type scanPointers struct {
	region   *xcap.Region
	obj      *dataobj.Object
	matchers []*labels.Matcher

	sStart *scalar.Timestamp
	sEnd   *scalar.Timestamp

	// internal state
	initialized        bool
	sym                *symbolizer.Symbolizer
	streams            map[int64]streams.Stream
	matchingStreamIDs  []scalar.Scalar
	pointersSections   []*dataobj.Section
	pointersSectionIdx int
	pointersReader     *pointers.Reader
	cols               []*pointers.Column

	// stats
	sectionPointersRead         int64
	sectionPointersReadDuration time.Duration
	streamsReadDuration         time.Duration
}

func newScanPointers(idxObj *dataobj.Object, sStart, sEnd *scalar.Timestamp, matchers []*labels.Matcher, region *xcap.Region) *scanPointers {
	return &scanPointers{
		obj:      idxObj,
		matchers: matchers,

		sStart: sStart,
		sEnd:   sEnd,

		region: region,

		sym: symbolizer.New(128, 1024),
	}
}

func (s *scanPointers) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}

	defer func(start time.Time) {
		s.sectionPointersReadDuration += time.Since(start)
	}(time.Now())

	ctx = xcap.ContextWithRegion(ctx, s.region)
	for {
		rec, err := s.pointersReader.Read(ctx, 128)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		} else if (rec == nil || rec.NumRows() == 0) && errors.Is(err, io.EOF) {
			// the section is fully read, proceed to the next one, continue the iteration so we read from the next section
			err := s.prepareForNextSection(ctx)
			if err != nil {
				return nil, err
			}
			continue
		}

		s.sectionPointersRead += rec.NumRows()
		return rec, nil
	}
}

func (s *scanPointers) GetStreamLabels(ctx context.Context, id int64) (labels.Labels, error) {
	if err := s.init(ctx); err != nil {
		return labels.EmptyLabels(), err
	}

	stream, _ := s.streams[id]
	return stream.Labels, nil
}

func (s *scanPointers) init(ctx context.Context) error {
	if s.initialized {
		return nil
	}

	if len(s.matchers) == 0 {
		return io.EOF
	}

	err := s.preparePointersSections(ctx)
	if err != nil {
		return err
	}

	err = s.prepareStreams(ctx)
	if err != nil {
		return err
	}

	s.pointersSectionIdx = -1
	err = s.prepareForNextSection(ctx)
	if err != nil {
		return err
	}

	s.initialized = true

	return nil
}

func (s *scanPointers) preparePointersSections(ctx context.Context) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	for _, section := range s.obj.Sections().Filter(pointers.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		s.pointersSections = append(s.pointersSections, section)
	}

	if len(s.pointersSections) == 0 {
		return io.EOF
	}

	return nil
}

func (s *scanPointers) prepareStreams(ctx context.Context) error {
	var reader streams.Reader
	defer func(start time.Time) {
		_ = reader.Close()
		s.streamsReadDuration = time.Since(start)
	}(time.Now())

	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}

	s.streams = make(map[int64]streams.Stream)
	const batchSize = 1024

	for _, section := range s.obj.Sections().Filter(streams.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := streams.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		predicates, err := buildStreamReaderPredicate(sec, s.sStart, s.sEnd, s.matchers)
		if err != nil {
			return err
		}

		var columns []*streams.Column
		for _, col := range sec.Columns() {
			if col.Type == streams.ColumnTypeStreamID || col.Type == streams.ColumnTypeLabel {
				columns = append(columns, col)
			}
		}

		if len(columns) == 0 {
			return fmt.Errorf("no columns to retrieve")
		}

		reader.Reset(streams.ReaderOptions{
			Columns:    columns,
			Predicates: predicates,
			Allocator:  memory.DefaultAllocator,
		})

		for {
			rec, err := reader.Read(ctx, batchSize)
			if rec != nil && rec.NumRows() > 0 {
				err := s.addStreams(rec, columns)
				if err != nil {
					return err
				}
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return fmt.Errorf("reading streams: %w", err)
			}
		}
	}

	if len(s.streams) == 0 {
		return io.EOF
	}

	return nil
}

func (s *scanPointers) addStreams(rec arrow.RecordBatch, columns []*streams.Column) error {
	numRows := int(rec.NumRows())
	// TODO(ivkalita): reuse buffers
	lbBuilders := make([]*labels.Builder, numRows)
	streamIDs := make([]int64, numRows)
	for cIdx := range int(rec.NumCols()) {
		col := rec.Column(cIdx)
		ct := columns[cIdx].Type
		switch ct {
		case streams.ColumnTypeLabel:
			values := col.(*array.String)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				if lbBuilders[rIdx] == nil {
					lbBuilders[rIdx] = labels.NewBuilder(labels.EmptyLabels())
				}

				lbBuilders[rIdx].Set(columns[cIdx].Name, s.sym.Get(values.Value(rIdx)))
			}
		case streams.ColumnTypeStreamID:
			values := col.(*array.Int64)
			for rIdx := range numRows {
				if col.IsNull(rIdx) {
					continue
				}
				streamIDs[rIdx] = values.Value(rIdx)
			}
		default:
			continue
		}
	}

	for rIdx := range numRows {
		streamID := streamIDs[rIdx]
		if streamID == 0 {
			// TODO(ivkalita): can this actually happen?
			continue
		}
		lbs := labels.EmptyLabels()
		if lbBuilders[rIdx] != nil {
			lbs = lbBuilders[rIdx].Labels()
		}
		s.streams[streamIDs[rIdx]] = streams.Stream{ID: streamID, Labels: lbs}
		s.matchingStreamIDs = append(s.matchingStreamIDs, scalar.NewInt64Scalar(streamID))
	}

	return nil
}

func (s *scanPointers) prepareForNextSection(ctx context.Context) error {
	for {
		skip, err := s.prepareForNextSectionOrSkip(ctx)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		return nil
	}
}

func (s *scanPointers) prepareForNextSectionOrSkip(ctx context.Context) (bool, error) {
	s.pointersSectionIdx++
	if s.pointersSectionIdx >= len(s.pointersSections) {
		// no more pointers sections to read
		return false, io.EOF
	}

	if s.pointersReader != nil {
		_ = s.pointersReader.Close()
	}

	sec, err := pointers.Open(ctx, s.pointersSections[s.pointersSectionIdx])
	if err != nil {
		return false, fmt.Errorf("opening section: %w", err)
	}

	cols, err := findPointersColumnsByTypes(
		sec.Columns(),
		pointers.ColumnTypePath,
		pointers.ColumnTypeSection,
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
		if colStreamID != nil && colMinTimestamp != nil && colMaxTimestamp != nil {
			break
		}
	}

	if colStreamID == nil || colMinTimestamp == nil || colMaxTimestamp == nil {
		// the section has no rows with stream-based indices and can be ignored completely
		return true, nil
	}

	s.cols = cols

	s.pointersReader = pointers.NewReader(pointers.ReaderOptions{
		Columns: s.cols,
		Predicates: []pointers.Predicate{
			pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, s.sStart, s.sEnd),
			pointers.InPredicate{
				Column: colStreamID,
				Values: s.matchingStreamIDs,
			},
		},
		Allocator: memory.DefaultAllocator,
	})

	return false, nil
}

func (s *scanPointers) Close() {
	if s.pointersReader != nil {
		_ = s.pointersReader.Close()
	}
	if s.region != nil {
		s.region.Record(xcap.StatMetastoreStreamsRead.Observe(int64(len(s.streams))))
		s.region.Record(xcap.StatMetastoreStreamsReadTime.Observe(s.streamsReadDuration.Seconds()))
		s.region.Record(xcap.StatMetastoreSectionPointersRead.Observe(s.sectionPointersRead))
		s.region.Record(xcap.StatMetastoreSectionPointersReadTime.Observe(s.sectionPointersReadDuration.Seconds()))
	}
}

var _ ArrowRecordBatchReader = (*scanPointers)(nil)
