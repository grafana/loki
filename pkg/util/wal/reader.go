package wal

import (
	"errors"
	"io"

	"github.com/prometheus/prometheus/tsdb/wlog"
)

// If startSegment is <0, it means all the segments.
func NewWalReader(dir string, startSegment int) (*wlog.Reader, io.Closer, error) {
	var (
		segmentReader io.ReadCloser
		err           error
	)
	if startSegment < 0 {
		segmentReader, err = wlog.NewSegmentsReader(dir)
		if err != nil {
			return nil, nil, err
		}
	} else {
		first, last, err := wlog.Segments(dir)
		if err != nil {
			return nil, nil, err
		}
		if startSegment > last {
			return nil, nil, errors.New("start segment is beyond the last WAL segment")
		}
		if first > startSegment {
			startSegment = first
		}
		segmentReader, err = wlog.NewSegmentsRangeReader(wlog.SegmentRange{
			Dir:   dir,
			First: startSegment,
			Last:  -1, // Till the end.
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return wlog.NewReader(segmentReader), segmentReader, nil
}
