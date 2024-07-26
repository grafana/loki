// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package wal

import (
	"path/filepath"
	"sync"

	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"
)

type walReplayer struct {
	w wlog.WriteTo
}

func (r walReplayer) Replay(dir string) error {
	w, err := wlog.Open(nil, dir)
	if err != nil {
		return err
	}

	dir, startFrom, err := wlog.LastCheckpoint(w.Dir())
	if err != nil && err != record.ErrNotFound {
		return err
	}

	if err == nil {
		sr, err := wlog.NewSegmentsReader(dir)
		if err != nil {
			return err
		}

		err = r.replayWAL(wlog.NewReader(sr))
		if closeErr := sr.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}

		startFrom++
	}

	_, last, err := wlog.Segments(w.Dir())
	if err != nil {
		return err
	}

	for i := startFrom; i <= last; i++ {
		s, err := wlog.OpenReadSegment(wlog.SegmentName(w.Dir(), i))
		if err != nil {
			return err
		}

		sr := wlog.NewSegmentBufReader(s)
		err = r.replayWAL(wlog.NewReader(sr))
		if closeErr := sr.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (r walReplayer) replayWAL(reader *wlog.Reader) error {
	var dec record.Decoder

	for reader.Next() {
		rec := reader.Record()
		switch dec.Type(rec) {
		case record.Series:
			series, err := dec.Series(rec, nil)
			if err != nil {
				return err
			}
			r.w.StoreSeries(series, 0)
		case record.Samples:
			samples, err := dec.Samples(rec, nil)
			if err != nil {
				return err
			}
			r.w.Append(samples)
		case record.Exemplars:
			exemplars, err := dec.Exemplars(rec, nil)
			if err != nil {
				return err
			}
			r.w.AppendExemplars(exemplars)
		}
	}

	return nil
}

type walDataCollector struct {
	mut       sync.Mutex
	samples   []record.RefSample
	series    []record.RefSeries
	exemplars []record.RefExemplar
}

func (c *walDataCollector) AppendExemplars(exemplars []record.RefExemplar) bool {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.exemplars = append(c.exemplars, exemplars...)
	return true
}

func (c *walDataCollector) Append(samples []record.RefSample) bool {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.samples = append(c.samples, samples...)
	return true
}

func (c *walDataCollector) StoreSeries(series []record.RefSeries, _ int) {
	c.mut.Lock()
	defer c.mut.Unlock()

	c.series = append(c.series, series...)
}

func (c *walDataCollector) AppendHistograms(_ []record.RefHistogramSample) bool {
	// TODO: support native histograms
	return true
}

func (c *walDataCollector) AppendFloatHistograms(_ []record.RefFloatHistogramSample) bool {
	// TODO: support native histograms
	return true
}

func (c *walDataCollector) UpdateSeriesSegment(_ []record.RefSeries, _ int) {}

func (c *walDataCollector) SeriesReset(_ int) {}

func (c *walDataCollector) StoreMetadata(_ []record.RefMetadata) {}

// SubDirectory returns the subdirectory within a Storage directory used for
// the Prometheus WAL.
func SubDirectory(base string) string {
	return filepath.Join(base, "wal")
}
