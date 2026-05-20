package discover

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg/tsdb"
)

// RunContentProbes runs per-stream detected_fields (classification) and keyword
// probes using TSDB-derived selectors and entry counts.
//
// Classification runs first (faster per call), followed by keyword probing.
// Both phases run sequentially (not interleaved) to keep API load predictable.
// Errors from individual API calls are absorbed as warnings (never fatal).
//
// BroadSelector must be set — it is the label matcher clause used for keyword
// probing (one query per keyword instead of one query per stream×keyword).
func RunContentProbes(
	cfg ProbeConfig,
	tsdbResult *tsdb.StructuralResult,
	classifyClient ClassifyAPI,
	keywordClient KeywordAPI,
) (*ContentProbeResult, error) {
	from := cfg.From
	to := cfg.To
	parallelism := cfg.effectiveParallelism()

	// 1. Copy selectors to probe.
	selectors := make([]string, len(tsdbResult.AllSelectors))
	copy(selectors, tsdbResult.AllSelectors)

	// 2. Classification phase.
	classifyResult := runClassifyPhase(selectors, tsdbResult, classifyClient, from, to, parallelism)

	// 3. Keyword phase — one query per keyword using the broad selector.
	keywordResult := runKeywordPhaseBroad(cfg.BroadSelector, selectors, keywordClient, from, to, cfg.effectiveBroadKeywordLimit())

	return &ContentProbeResult{
		Classify: classifyResult,
		Keywords: keywordResult,
	}, nil
}

// runClassifyPhase fans out detected_fields API calls for each selector using
// the same errgroup+mutex+callWithRetry pattern as RunClassification.
func runClassifyPhase(
	selectors []string,
	tsdbResult *tsdb.StructuralResult,
	client ClassifyAPI,
	from, to time.Time,
	parallelism int,
) *ClassifyResult {
	var mu sync.Mutex
	var classifications []streamClassification
	var warnings []string
	completed := 0
	total := len(selectors)

	sets := newBoundedSets()

	ctx := context.Background()
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallelism)

	for _, selector := range selectors {
		sel := selector // capture loop variable
		g.Go(func() error {
			var resp *loghttp.DetectedFieldsResponse
			err := callWithRetry(ctx, func() error {
				var e error
				resp, e = client.GetDetectedFields(sel, 1000, from, to)
				return e
			})

			mu.Lock()
			completed++
			if completed%25 == 0 || completed == total {
				fmt.Fprintf(os.Stderr, "\r  classify: %d/%d streams", completed, total)
			}
			mu.Unlock()

			if err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("probe classify: skipping %s: %v", sel, err))
				mu.Unlock()
				return nil // absorbed — never return error from goroutine
			}

			// Build streamLabelSet from the stored LabelSet for this selector.
			streamLabelSet := make(map[string]struct{})
			if ls, ok := tsdbResult.LabelSets[sel]; ok {
				for key := range ls {
					streamLabelSet[key] = struct{}{}
				}
			}

			sc := classifyStream(sel, resp.Fields, streamLabelSet, sets)
			mu.Lock()
			classifications = append(classifications, sc)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	fmt.Fprintln(os.Stderr) // newline after progress

	// Build inverted indexes from per-stream classifications.
	out := &ClassifyResult{
		ByFormat:             make(map[bench.LogFormat][]string),
		ByUnwrappableField:   make(map[string][]string),
		ByDetectedField:      make(map[string][]string),
		ByStructuredMetadata: make(map[string][]string),
		ByLabelKey:           make(map[string][]string),
		FormatBySelector:     make(map[string]bench.LogFormat),
		TotalSkipped:         len(selectors) - len(classifications),
		TotalClassified:      len(classifications),
		Warnings:             warnings,
	}

	for _, sc := range classifications {
		out.ByFormat[sc.format] = append(out.ByFormat[sc.format], sc.selector)
		out.FormatBySelector[sc.selector] = sc.format
		for _, f := range sc.unwrappable {
			out.ByUnwrappableField[f] = append(out.ByUnwrappableField[f], sc.selector)
		}
		for _, f := range sc.detectedFields {
			out.ByDetectedField[f] = append(out.ByDetectedField[f], sc.selector)
		}
		for _, f := range sc.structuredMeta {
			out.ByStructuredMetadata[f] = append(out.ByStructuredMetadata[f], sc.selector)
		}
		for _, k := range sc.labelKeys {
			out.ByLabelKey[k] = append(out.ByLabelKey[k], sc.selector)
		}
	}

	// Sort all index slices for determinism.
	for k := range out.ByFormat {
		sort.Strings(out.ByFormat[k])
	}
	for k := range out.ByUnwrappableField {
		sort.Strings(out.ByUnwrappableField[k])
	}
	for k := range out.ByDetectedField {
		sort.Strings(out.ByDetectedField[k])
	}
	for k := range out.ByStructuredMetadata {
		sort.Strings(out.ByStructuredMetadata[k])
	}
	for k := range out.ByLabelKey {
		sort.Strings(out.ByLabelKey[k])
	}

	return out
}

// runKeywordPhaseBroad implements the broad keyword probing strategy.
// Instead of issuing one query per stream×keyword (N×K), it issues one query
// per keyword using the broad selector (K queries total), then matches returned
// stream labels client-side against the known selector set.
func runKeywordPhaseBroad(
	broadSelector string,
	knownSelectors []string,
	client KeywordAPI,
	from, to time.Time,
	broadLimit int,
) *KeywordResult {
	// Normalize BroadSelector: ensure it has outer braces for valid LogQL.
	// The --selector CLI flag is defined as bare matchers (no braces).
	sel := strings.TrimSpace(broadSelector)
	if !strings.HasPrefix(sel, "{") {
		sel = "{" + sel + "}"
	}
	broadSelector = sel

	// Build a job-based index for matching API streams to known selectors.
	//
	// The Loki query API and TSDB indexes return different label sets for the
	// same logical stream: the API may add structured metadata (org_id,
	// traceID, detected_level) and omit some indexed labels (agent_log,
	// insight), while TSDB has the full indexed set but no structured metadata.
	// This makes exact string matching or even subset matching unreliable.
	//
	// Instead, we match on the "job" label which uniquely identifies a
	// service deployment (namespace/service-zone) and is always present in
	// both sources. When a keyword appears in any stream of a job, we mark
	// ALL known selectors with that job value as having the keyword.
	jobIndex := make(map[string][]string) // job value → known selectors
	for _, s := range knownSelectors {
		lm := parseSelectorToMap(s)
		if job, ok := lm["job"]; ok {
			jobIndex[job] = append(jobIndex[job], s)
		}
	}

	out := &KeywordResult{
		ByKeyword: make(map[string][]string),
	}

	for i, keyword := range bench.FilterableKeywords {
		query := keywordQuery(broadSelector, keyword)
		fmt.Fprintf(os.Stderr, "\r  broad keyword probe [%d/%d]: %q", i+1, len(bench.FilterableKeywords), keyword)

		var resp *loghttp.QueryResponse
		err := callWithRetry(context.Background(), func() error {
			var e error
			resp, e = client.QueryRange(
				query,
				broadLimit,
				from,
				to,
				logproto.BACKWARD,
				0,
				0,
				true,
			)
			return e
		})
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("broad keyword %q: %v", keyword, err))
			out.TotalSkipped++
			continue
		}

		out.TotalProbed++

		streams, ok := resp.Data.Result.(loghttp.Streams)
		if !ok {
			continue
		}

		// Match returned streams against known selectors using job-based matching.
		// For each API stream, extract the "job" label and find all known
		// selectors that share the same job value.
		matched := make(map[string]struct{})
		for _, s := range streams {
			job, ok := s.Labels["job"]
			if !ok {
				continue
			}
			for _, sel := range jobIndex[job] {
				matched[sel] = struct{}{}
			}
		}

		if len(matched) > 0 {
			selectors := make([]string, 0, len(matched))
			for sel := range matched {
				selectors = append(selectors, sel)
			}
			sort.Strings(selectors)
			out.ByKeyword[keyword] = selectors
		}
	}
	fmt.Fprintln(os.Stderr) // newline after progress

	return out
}

// keywordQuery constructs the line filter query for a given selector and
// keyword. The keyword is quoted with %q to produce a valid LogQL string
// filter, e.g. {service_name="loki"} |= "error".
func keywordQuery(selector, keyword string) string {
	return fmt.Sprintf(`%s |= %q`, selector, keyword)
}

// parseSelectorToMap parses a canonical selector string like
// {foo="bar", baz="qux"} into a map[string]string.
func parseSelectorToMap(selector string) map[string]string {
	m := make(map[string]string)
	// Strip outer braces.
	s := strings.TrimSpace(selector)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	// Split on ", " — canonical selectors use this separator.
	// Each part is key="value".
	for part := range strings.SplitSeq(s, ", ") {
		part = strings.TrimSpace(part)
		eqIdx := strings.Index(part, "=")
		if eqIdx < 0 {
			continue
		}
		key := part[:eqIdx]
		val := part[eqIdx+1:]
		// Strip quotes from value.
		val = strings.Trim(val, "\"")
		m[key] = val
	}
	return m
}
