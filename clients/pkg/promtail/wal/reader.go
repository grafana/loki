package wal

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/wlog"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/util"
)

// ReadWAL will read all entries in the WAL located under dir. Mainly used for testing
func ReadWAL(dir string) ([]api.Entry, error) {
	reader, close, err := NewWalReader(dir, -1)
	if err != nil {
		return nil, err
	}
	defer func() { close.Close() }()

	seenSeries := make(map[uint64]model.LabelSet)
	seenEntries := []api.Entry{}

	for reader.Next() {
		var walRec = WALRecord{}
		bytes := reader.Record()
		err = DecodeWALRecord(bytes, &walRec)
		if err != nil {
			return nil, fmt.Errorf("error decoding wal record: %w", err)
		}

		// first read series
		for _, series := range walRec.Series {
			if _, ok := seenSeries[uint64(series.Ref)]; !ok {

				seenSeries[uint64(series.Ref)] = util.MapToModelLabelSet(series.Labels.Map())
			}
		}

		for _, entries := range walRec.RefEntries {
			for _, entry := range entries.Entries {
				labels, ok := seenSeries[uint64(entries.Ref)]
				if !ok {
					return nil, fmt.Errorf("found entry without matching series")
				}
				seenEntries = append(seenEntries, api.Entry{
					Labels: labels,
					Entry:  entry,
				})
			}
		}

		// reset entry
		walRec.Series = walRec.Series[:]
		walRec.RefEntries = walRec.RefEntries[:]
	}

	return seenEntries, nil
}

// NewWalReader creates a reader that's able to read all segments in a WAL from startSegment onwards. If startSegment is
// less than 1, all segments are read.
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
