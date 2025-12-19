package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type scanPointers struct {
	region   *xcap.Region
	obj      *dataobj.Object
	matchers []*labels.Matcher

	start time.Time
	end   time.Time

	initialized        bool
	matchingStreamIDs  []scalar.Scalar
	pointersSections   []*dataobj.Section
	pointersSectionIdx int
	pointersReader     *pointers.Reader
	cols               []*pointers.Column

	// stats
	rowsRead       int64
	labelsByStream map[int64]labels.Labels
}

func newScanPointers(idxObj *dataobj.Object, start, end time.Time, matchers []*labels.Matcher, region *xcap.Region) *scanPointers {
	return &scanPointers{
		obj:      idxObj,
		matchers: matchers,

		start: start,
		end:   end,

		region:         region,
		labelsByStream: make(map[int64]labels.Labels),
	}
}

func (s *scanPointers) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if err := s.init(ctx); err != nil {
		return nil, err
	}

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

		s.rowsRead += rec.NumRows()
		return rec, nil
	}
}

func (s *scanPointers) removeLabelPredicates(predicates []*labels.Matcher) []*labels.Matcher {
	allLabels := s.allLabels()
	newPredicates := make([]*labels.Matcher, 0, len(predicates))
	for _, predicate := range predicates {
		if !slices.Contains(allLabels, predicate.Name) {
			newPredicates = append(newPredicates, predicate)
		}
	}
	return newPredicates
}

func (s *scanPointers) allLabels() []string {
	allLabelsMap := map[string]struct{}{}
	for _, lbls := range s.labelsByStream {
		lbls.Range(func(l labels.Label) {
			allLabelsMap[l.Name] = struct{}{}
		})
	}

	allLabels := make([]string, 0, len(allLabelsMap))
	for label := range allLabelsMap {
		allLabels = append(allLabels, label)
	}

	return allLabels
}

func (s *scanPointers) LabelsByStreamID() map[int64][]string {
	allLabelsMap := map[int64][]string{}
	for id, lbls := range s.labelsByStream {
		_, ok := allLabelsMap[id]
		if !ok {
			allLabelsMap[id] = []string{}
		}
		lbls.Range(func(l labels.Label) {
			allLabelsMap[id] = append(allLabelsMap[id], l.Name)
		})
	}
	return allLabelsMap
}

func (s *scanPointers) init(ctx context.Context) error {
	if s.initialized {
		return nil
	}

	if len(s.matchers) == 0 {
		return io.EOF
	}

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

	// find stream ids that satisfy the predicate and start/end
	err = s.populateMatchingStreamsAndLabels(ctx)
	if err != nil {
		return fmt.Errorf("creating matching stream ids: %w", err)
	}

	if s.matchingStreamIDs == nil {
		return io.EOF
	}

	s.pointersSectionIdx = -1
	err = s.prepareForNextSection(ctx)
	if err != nil {
		return err
	}

	s.initialized = true

	return nil
}

func (s *scanPointers) populateMatchingStreamsAndLabels(ctx context.Context) error {
	predicate := streamPredicateFromMatchers(s.start, s.end, s.matchers...)
	err := forEachStream(ctx, s.obj, predicate, func(stream streams.Stream) {
		s.labelsByStream[stream.ID] = stream.Labels
	})

	for streamID := range s.labelsByStream {
		s.matchingStreamIDs = append(s.matchingStreamIDs, scalar.NewInt64Scalar(streamID))
	}

	if err != nil {
		return fmt.Errorf("error iterating streams: %v", err)
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

	sStart, sEnd := s.scalarTimestamps()
	s.pointersReader = pointers.NewReader(pointers.ReaderOptions{
		Columns: s.cols,
		Predicates: []pointers.Predicate{
			pointers.WhereTimeRangeOverlapsWith(colMinTimestamp, colMaxTimestamp, sStart, sEnd),
			pointers.InPredicate{
				Column: colStreamID,
				Values: s.matchingStreamIDs,
			},
		},
		Allocator: memory.DefaultAllocator,
	})

	return false, nil
}

func (s *scanPointers) scalarTimestamps() (*scalar.Timestamp, *scalar.Timestamp) {
	sStart := scalar.NewTimestampScalar(arrow.Timestamp(s.start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(s.end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	return sStart, sEnd
}

func (s *scanPointers) Close() {
	if s.pointersReader != nil {
		_ = s.pointersReader.Close()
	}
}

var _ ArrowRecordBatchReader = (*scanPointers)(nil)
