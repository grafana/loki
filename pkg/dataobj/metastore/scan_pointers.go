package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/xcap"
)

type scanPointers struct {
	region   *xcap.Region
	obj      *dataobj.Object
	matchers []*labels.Matcher

	sStart *scalar.Timestamp
	sEnd   *scalar.Timestamp

	initialized        bool
	matchingStreamIDs  []scalar.Scalar
	pointersSections   []*dataobj.Section
	pointersSectionIdx int
	pointersReader     *pointers.Reader
	cols               []*pointers.Column

	// stats
	rowsRead int64
}

func newScanPointers(idxObj *dataobj.Object, sStart, sEnd *scalar.Timestamp, matchers []*labels.Matcher, region *xcap.Region) *scanPointers {
	return &scanPointers{
		obj:      idxObj,
		matchers: matchers,

		sStart: sStart,
		sEnd:   sEnd,

		region: region,
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
	s.matchingStreamIDs, err = s.findMatchingStreamIDs(ctx)
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

func (s *scanPointers) findMatchingStreamIDs(ctx context.Context) ([]scalar.Scalar, error) {
	var matchingStreamIDs []scalar.Scalar
	err := forEachStreamID(ctx, s.obj, s.sStart, s.sEnd, s.matchers, func(streamID int64) {
		matchingStreamIDs = append(matchingStreamIDs, scalar.NewInt64Scalar(streamID))
	})
	if err != nil {
		return nil, fmt.Errorf("error iterating streams: %v", err)
	}

	return matchingStreamIDs, nil
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
}

var _ ArrowRecordBatchReader = (*scanPointers)(nil)
