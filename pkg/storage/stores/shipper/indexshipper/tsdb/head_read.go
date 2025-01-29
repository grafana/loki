// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tsdb

import (
	"math"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Index returns an IndexReader against the block.
func (h *Head) Index() IndexReader {
	return h.indexRange(math.MinInt64, math.MaxInt64)
}

func (h *Head) indexRange(mint, maxt int64) *headIndexReader {
	if hmin := h.MinTime(); hmin > mint {
		mint = hmin
	}
	return &headIndexReader{head: h, mint: mint, maxt: maxt}
}

type headIndexReader struct {
	head       *Head
	mint, maxt int64
}

func (h *headIndexReader) Bounds() (int64, int64) {
	return h.head.MinTime(), h.head.MaxTime()
}

func (h *headIndexReader) Checksum() uint32 { return 0 }

func (h *headIndexReader) Close() error {
	return nil
}

func (h *headIndexReader) Symbols() index.StringIter {
	return h.head.postings.Symbols()
}

// SortedLabelValues returns label values present in the head for the
// specific label name that are within the time range mint to maxt.
// If matchers are specified the returned result set is reduced
// to label values of metrics matching the matchers.
func (h *headIndexReader) SortedLabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	values, err := h.LabelValues(name, matchers...)
	if err == nil {
		sort.Strings(values)
	}
	return values, err
}

// LabelValues returns label values present in the head for the
// specific label name that are within the time range mint to maxt.
// If matchers are specified the returned result set is reduced
// to label values of metrics matching the matchers.
func (h *headIndexReader) LabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	if h.maxt < h.head.MinTime() || h.mint > h.head.MaxTime() {
		return []string{}, nil
	}

	if len(matchers) == 0 {
		return h.head.postings.LabelValues(name), nil
	}

	return labelValuesWithMatchers(h, name, matchers...)
}

// LabelNames returns all the unique label names present in the head
// that are within the time range mint to maxt.
func (h *headIndexReader) LabelNames(matchers ...*labels.Matcher) ([]string, []string, error) {
	if h.maxt < h.head.MinTime() || h.mint > h.head.MaxTime() {
		return []string{}, nil, nil
	}

	if len(matchers) == 0 {
		labelNames := h.head.postings.LabelNames()
		sort.Strings(labelNames)

		// On the file index we don't return the SM fields unless we pass some matchers.
		// For consistency, we won't return them here.
		// Here's the code that would return them:
		//SMFieldNames := make(map[string]struct{})
		//for _, s := range h.head.series.series {
		//	s.RLock()
		//	for _, ms := range s.m {
		//		ms.RLock()
		//		for ln := range ms.stats.StructuredMetadataFieldNames {
		//			SMFieldNames[ln] = struct{}{}
		//		}
		//		ms.RUnlock()
		//	}
		//	s.RUnlock()
		//}
		//
		//structuredMetadataFieldNames := make([]string, 0, len(SMFieldNames))
		//for name := range SMFieldNames {
		//	structuredMetadataFieldNames = append(structuredMetadataFieldNames, name)
		//}
		//sort.Strings(structuredMetadataFieldNames)

		return labelNames, nil, nil
	}

	return labelNamesWithMatchers(h, matchers...)
}

// Postings returns the postings list iterator for the label pairs.
func (h *headIndexReader) Postings(name string, fpFilter index.FingerprintFilter, values ...string) (index.Postings, error) {
	var p index.Postings
	switch len(values) {
	case 0:
		p = index.EmptyPostings()
	case 1:
		p = h.head.postings.Get(name, values[0])
	default:
		res := make([]index.Postings, 0, len(values))
		for _, value := range values {
			res = append(res, h.head.postings.Get(name, value))
		}
		p = index.Merge(res...)
	}

	if fpFilter != nil {
		return index.NewShardedPostings(p, fpFilter, nil), nil
	}
	return p, nil
}

// Series returns the series for the given reference.
func (h *headIndexReader) Series(ref storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]index.ChunkMeta, stats **index.StreamStats) (uint64, error) {
	s := h.head.series.getByID(uint64(ref))

	if s == nil {
		h.head.metrics.seriesNotFound.Inc()
		return 0, storage.ErrNotFound
	}
	*lbls = append((*lbls)[:0], s.ls...)

	if stats != nil {
		*stats = s.stats
	}

	queryBounds := newBounds(model.Time(from), model.Time(through))

	*chks = (*chks)[:0]
	s.Lock()
	for _, chk := range s.chks {
		if !Overlap(chk, queryBounds) {
			continue
		}
		*chks = append(*chks, chk)
	}
	s.Unlock()

	return s.fp, nil
}

func (h *headIndexReader) ChunkStats(ref storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, index.ChunkStats, error) {
	s := h.head.series.getByID(uint64(ref))

	if s == nil {
		h.head.metrics.seriesNotFound.Inc()
		return 0, index.ChunkStats{}, storage.ErrNotFound
	}
	if len(by) == 0 {
		*lbls = append((*lbls)[:0], s.ls...)
	} else {
		*lbls = (*lbls)[:0]
		for _, l := range s.ls {
			if _, ok := by[l.Name]; ok {
				*lbls = append(*lbls, l)
			}
		}
	}

	queryBounds := newBounds(model.Time(from), model.Time(through))

	var res index.ChunkStats
	s.Lock()
	for _, chk := range s.chks {
		if !Overlap(chk, queryBounds) {
			continue
		}
		res.AddChunk(&chk, from, through)
	}
	s.Unlock()

	return s.fp, res, nil
}

// LabelValueFor returns label value for the given label name in the series referred to by ID.
func (h *headIndexReader) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	memSeries := h.head.series.getByID(uint64(id))
	if memSeries == nil {
		return "", storage.ErrNotFound
	}

	value := memSeries.ls.Get(label)
	if value == "" {
		return "", storage.ErrNotFound
	}

	return value, nil
}

// LabelNamesFor returns all the label names for the series referred to by IDs.
// The names returned are sorted.
func (h *headIndexReader) LabelNamesFor(ids ...storage.SeriesRef) ([]string, []string, error) {
	SMNames := make(map[string]struct{})
	streamLabelNamesMap := make(map[string]struct{})
	for _, id := range ids {
		memSeries := h.head.series.getByID(uint64(id))
		if memSeries == nil {
			return nil, nil, storage.ErrNotFound
		}

		memSeries.RLock()
		for _, lbl := range memSeries.ls {
			streamLabelNamesMap[lbl.Name] = struct{}{}
		}
		for ln := range memSeries.stats.StructuredMetadataFieldNames {
			SMNames[ln] = struct{}{}
		}
		memSeries.RUnlock()
	}

	streamLabelNames := make([]string, 0, len(streamLabelNamesMap))
	for name := range streamLabelNamesMap {
		streamLabelNames = append(streamLabelNames, name)
	}
	sort.Strings(streamLabelNames)

	structuredMetadataFieldNames := make([]string, 0, len(SMNames))
	for name := range SMNames {
		structuredMetadataFieldNames = append(structuredMetadataFieldNames, name)
	}
	sort.Strings(structuredMetadataFieldNames)

	return streamLabelNames, structuredMetadataFieldNames, nil
}
