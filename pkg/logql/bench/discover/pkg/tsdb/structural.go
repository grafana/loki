package tsdb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// parseSelectorMatchers parses a LogQL-style label matcher clause (without
// outer braces) into Prometheus label matchers suitable for ForSeries. If the
// selector string is empty, a single match-everything matcher is returned.
func parseSelectorMatchers(selector string) ([]*labels.Matcher, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "", "")}, nil
	}

	// Wrap in braces so the LogQL parser can handle it as a stream selector.
	matchers, err := syntax.ParseMatchers("{"+selector+"}", false)
	if err != nil {
		return nil, fmt.Errorf("parse --selector %q: %w", selector, err)
	}
	if len(matchers) == 0 {
		return []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "", "")}, nil
	}
	return matchers, nil
}

// RunStructuralDiscovery enumerates streams from local TSDB indexes,
// deduplicates by canonical selector, and builds structural inverted indexes.
// When cfg.Selector is set, only streams matching those label matchers are
// enumerated. When cfg.MaxStreams is set, the output is capped using
// service-diversity selection.
func RunStructuralDiscovery(cfg StructuralConfig, indexes []tsdb.Index) (*StructuralResult, error) {
	from, through, err := resolveStructuralBounds(cfg, indexes)
	if err != nil {
		return nil, err
	}

	matchers, err := parseSelectorMatchers(cfg.Selector)
	if err != nil {
		return nil, err
	}

	// Progress reporting setup.
	progressInterval := cfg.ProgressInterval
	if progressInterval == 0 {
		progressInterval = 10 * time.Second
	}
	lastProgress := time.Now()

	merged := make(map[string]*MergedStream)
	totalRaw := 0

	for i, idx := range indexes {
		if idx == nil {
			continue
		}

		err := idx.ForSeries(
			context.Background(),
			cfg.UserID,
			nil,
			from,
			through,
			func(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) (stop bool) {
				payload := cloneStructuralSeriesPayload(lbls, fp, chks)
				// Strip internal TSDB labels (__stream_shard__, __time_shard__, etc.)
				// from the canonical selector so it can be used with the Loki query API.
				payload.Labels = stripInternalLabels(payload.Labels)
				selector := payload.Labels.String()

				entry, ok := merged[selector]
				if !ok {
					entry = &MergedStream{
						Selector: selector,
						Labels:   payload.Labels,
					}
					merged[selector] = entry
				}

				entry.SourceCount++
				entry.ChunkMetas = append(entry.ChunkMetas, payload.ChunkMetas...)
				totalRaw++

				// Periodic progress callback.
				if cfg.ProgressWriter != nil && time.Since(lastProgress) >= progressInterval {
					cfg.ProgressWriter(totalRaw, len(merged))
					lastProgress = time.Now()
				}

				return false
			},
			matchers...,
		)
		if err != nil {
			return nil, fmt.Errorf("enumerate TSDB series for index %d: %w", i, err)
		}
	}

	// Final progress update.
	if cfg.ProgressWriter != nil {
		cfg.ProgressWriter(totalRaw, len(merged))
	}

	// Apply service-diversity selection if MaxStreams is set.
	// Build a seen map (selector → LabelSet) for selectWithDiversity.
	seen := make(map[string]loghttp.LabelSet, len(merged))
	for selector, stream := range merged {
		seen[selector] = stream.Labels
	}

	var selectedSelectors []string
	if cfg.MaxStreams > 0 {
		selectedSelectors = selectWithDiversity(seen, cfg.MaxStreams)
	} else {
		selectedSelectors = make([]string, 0, len(merged))
		for selector := range merged {
			selectedSelectors = append(selectedSelectors, selector)
		}
		sort.Strings(selectedSelectors)
	}

	// Build inverted indexes only for selected selectors.
	selectedSet := make(map[string]struct{}, len(selectedSelectors))
	for _, sel := range selectedSelectors {
		selectedSet[sel] = struct{}{}
	}

	labelSets := make(map[string]loghttp.LabelSet, len(selectedSelectors))
	byServiceName := make(map[string][]string)
	byLabelKey := make(map[string][]string)
	boundedLabelKeys := boundedLabelKeySet()

	for _, selector := range selectedSelectors {
		stream := merged[selector]
		labelSets[selector] = cloneStructuralLabelSet(stream.Labels)

		if svc := stream.Labels["service_name"]; svc != "" {
			byServiceName[svc] = append(byServiceName[svc], selector)
		}

		for key := range stream.Labels {
			if _, ok := boundedLabelKeys[key]; ok {
				byLabelKey[key] = append(byLabelKey[key], selector)
			}
		}
	}

	for svc := range byServiceName {
		sort.Strings(byServiceName[svc])
	}
	for key := range byLabelKey {
		sort.Strings(byLabelKey[key])
	}

	// Build merged streams only for selected selectors.
	mergedStreams := make(map[string]MergedStream, len(selectedSelectors))
	for _, selector := range selectedSelectors {
		stream := merged[selector]
		copiedChunks := make([]tsdbindex.ChunkMeta, len(stream.ChunkMetas))
		copy(copiedChunks, stream.ChunkMetas)
		sort.Sort(tsdbindex.ChunkMetas(copiedChunks))
		mergedStreams[selector] = MergedStream{
			Selector:    selector,
			Labels:      cloneStructuralLabelSet(stream.Labels),
			ChunkMetas:  copiedChunks,
			SourceCount: stream.SourceCount,
		}
	}

	return &StructuralResult{
		AllSelectors:  selectedSelectors,
		LabelSets:     labelSets,
		ByServiceName: byServiceName,
		ByLabelKey:    byLabelKey,
		TotalRaw:      totalRaw,
		TotalUnique:   len(merged),
		TotalSelected: len(selectedSelectors),
		MergedStreams: mergedStreams,
	}, nil
}

func resolveStructuralBounds(cfg StructuralConfig, indexes []tsdb.Index) (model.Time, model.Time, error) {
	if len(indexes) == 0 {
		return 0, 0, fmt.Errorf("at least one TSDB index is required")
	}

	from, through := cfg.From, cfg.To
	if from.IsZero() || through.IsZero() {
		var minBound model.Time
		var maxBound model.Time
		hasBounds := false

		for _, idx := range indexes {
			if idx == nil {
				continue
			}
			idxFrom, idxThrough := idx.Bounds()
			if !hasBounds {
				minBound = idxFrom
				maxBound = idxThrough
				hasBounds = true
				continue
			}
			if idxFrom < minBound {
				minBound = idxFrom
			}
			if idxThrough > maxBound {
				maxBound = idxThrough
			}
		}

		if !hasBounds {
			return 0, 0, fmt.Errorf("at least one non-nil TSDB index is required")
		}

		if from.IsZero() {
			from = minBound.Time()
		}
		if through.IsZero() {
			through = maxBound.Time()
		}
	}

	if from.After(through) {
		return 0, 0, fmt.Errorf("invalid TSDB structural time range: from (%s) is after to (%s)", from.Format(time.RFC3339), through.Format(time.RFC3339))
	}

	return model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()), nil
}

func cloneStructuralSeriesPayload(lbls labels.Labels, fp model.Fingerprint, chks []tsdbindex.ChunkMeta) StructuralSeriesPayload {
	clonedChunks := make([]tsdbindex.ChunkMeta, len(chks))
	copy(clonedChunks, chks)

	return StructuralSeriesPayload{
		Labels:      cloneStructuralLabelSetFromPromLabels(lbls),
		Fingerprint: fp,
		ChunkMetas:  clonedChunks,
	}
}

func cloneStructuralLabelSetFromPromLabels(lbls labels.Labels) loghttp.LabelSet {
	cloned := make(loghttp.LabelSet)
	lbls.Range(func(lbl labels.Label) {
		cloned[strings.Clone(lbl.Name)] = strings.Clone(lbl.Value)
	})
	return cloned
}

func cloneStructuralLabelSet(src loghttp.LabelSet) loghttp.LabelSet {
	cloned := make(loghttp.LabelSet, len(src))
	for key, value := range src {
		cloned[strings.Clone(key)] = strings.Clone(value)
	}
	return cloned
}

// stripInternalLabels returns a copy of the LabelSet with internal TSDB labels
// removed. Internal labels start and end with "__" (e.g. __stream_shard__,
// __time_shard__). These labels are stored in TSDB indexes but are not visible
// through the Loki query API, so selectors containing them cause API queries to
// hang or return empty results.
func stripInternalLabels(src loghttp.LabelSet) loghttp.LabelSet {
	out := make(loghttp.LabelSet, len(src))
	for key, value := range src {
		if strings.HasPrefix(key, "__") && strings.HasSuffix(key, "__") {
			continue
		}
		out[key] = value
	}
	return out
}

func boundedLabelKeySet() map[string]struct{} {
	keys := make(map[string]struct{}, len(bench.LabelKeys))
	for _, key := range bench.LabelKeys {
		keys[key] = struct{}{}
	}
	return keys
}
