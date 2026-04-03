package discover

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestRunContentProbes_BasicFlow(t *testing.T) {
	sel1 := `{app="a", job="ns/svc-a"}`
	sel2 := `{app="b", job="ns/svc-b"}`

	tsdbResult := makeTSDBResult(map[string]uint32{
		sel1: 100,
		sel2: 100,
	})
	tsdbResult.LabelSets = map[string]loghttp.LabelSet{
		sel1: {"app": "a", "job": "ns/svc-a"},
		sel2: {"app": "b", "job": "ns/svc-b"},
	}

	// Mock classify: return JSON fields with one unwrappable field for both selectors.
	classifyClient := newFakeClassifyClient()
	classifyClient.response[sel1] = &loghttp.DetectedFieldsResponse{
		Fields: []loghttp.DetectedField{
			df("field_a", "json", logproto.DetectedFieldString),
			df(bench.UnwrappableFields[0], "json", logproto.DetectedFieldInt), // unwrappable
		},
	}
	classifyClient.response[sel2] = &loghttp.DetectedFieldsResponse{
		Fields: []loghttp.DetectedField{
			df("field_b", "json", logproto.DetectedFieldString),
		},
	}

	// Mock keyword: "error" present on both sel1 and sel2 via broad strategy.
	kwClient := &fakeBroadKeywordClient{
		streamsByKeyword: map[string]loghttp.Streams{
			"error": {
				{Labels: loghttp.LabelSet{"app": "a", "job": "ns/svc-a"}, Entries: []loghttp.Entry{{Line: "error happened"}}},
			},
		},
	}

	cfg := baseProbeCfg()
	result, err := RunContentProbes(cfg, tsdbResult, classifyClient, kwClient)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Classify assertions.
	require.Equal(t, 2, result.Classify.TotalClassified)
	require.Contains(t, result.Classify.ByFormat[bench.LogFormatJSON], sel1)
	require.Contains(t, result.Classify.ByFormat[bench.LogFormatJSON], sel2)
	require.Contains(t, result.Classify.ByUnwrappableField[bench.UnwrappableFields[0]], sel1)

	// Keyword assertions (broad strategy: TotalProbed = len(FilterableKeywords)).
	require.Contains(t, result.Keywords.ByKeyword["error"], sel1)
	require.Equal(t, len(bench.FilterableKeywords), result.Keywords.TotalProbed)
}

func TestRunContentProbes_EmptyStreams(t *testing.T) {
	tsdbResult := &tsdb.StructuralResult{
		AllSelectors:  []string{},
		LabelSets:     map[string]loghttp.LabelSet{},
		MergedStreams: map[string]tsdb.MergedStream{},
	}
	classifyClient := newFakeClassifyClient()
	kwClient := emptyBroadKeywordClient()

	cfg := baseProbeCfg()
	result, err := RunContentProbes(cfg, tsdbResult, classifyClient, kwClient)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, 0, result.Classify.TotalClassified)
	require.Equal(t, 0, result.Classify.TotalSkipped)
	require.Equal(t, len(bench.FilterableKeywords), result.Keywords.TotalProbed)
	require.Equal(t, 0, result.Keywords.TotalSkipped)
}

func TestRunContentProbes_ClassifyError(t *testing.T) {
	sel1 := `{app="a"}`
	sel2 := `{app="b"}`

	tsdbResult := makeTSDBResult(map[string]uint32{
		sel1: 100,
		sel2: 100,
	})
	classifyClient := newFakeClassifyClient()
	// sel1 returns error, sel2 succeeds.
	classifyClient.errOn[sel1] = errors.New("simulated classify failure")
	classifyClient.response[sel2] = &loghttp.DetectedFieldsResponse{
		Fields: []loghttp.DetectedField{
			df("field_a", "json", logproto.DetectedFieldString),
		},
	}

	kwClient := emptyBroadKeywordClient()

	cfg := baseProbeCfg()
	result, err := RunContentProbes(cfg, tsdbResult, classifyClient, kwClient)
	require.NoError(t, err)

	// sel1 is skipped (counted in TotalSkipped), sel2 classified.
	require.Equal(t, 1, result.Classify.TotalClassified)
	require.Equal(t, 1, result.Classify.TotalSkipped)
	require.NotEmpty(t, result.Classify.Warnings)

	// sel2 still classified successfully.
	require.Contains(t, result.Classify.ByFormat[bench.LogFormatJSON], sel2)
}

func TestRunContentProbes_BroadKeyword(t *testing.T) {
	sel1 := `{app="a", job="ns/svc-a"}`
	sel2 := `{app="b", job="ns/svc-b"}`
	broadSel := `{namespace=~"test"}`

	tsdbResult := makeTSDBResult(map[string]uint32{
		sel1: 100,
		sel2: 100,
	})
	tsdbResult.LabelSets = map[string]loghttp.LabelSet{
		sel1: {"app": "a", "job": "ns/svc-a"},
		sel2: {"app": "b", "job": "ns/svc-b"},
	}

	classifyClient := newFakeClassifyClient()
	classifyClient.response[sel1] = &loghttp.DetectedFieldsResponse{
		Fields: []loghttp.DetectedField{
			df("field_a", "json", logproto.DetectedFieldString),
		},
	}
	classifyClient.response[sel2] = &loghttp.DetectedFieldsResponse{
		Fields: []loghttp.DetectedField{
			df("field_b", "json", logproto.DetectedFieldString),
		},
	}

	// Mock keyword client that returns streams with job labels for matching.
	// "error" query returns streams matching sel1 and sel2, "level" returns sel1 only.
	// The API may return additional labels (e.g. traceID) not in TSDB —
	// job-based matching handles this correctly.
	kwClient := &fakeBroadKeywordClient{
		streamsByKeyword: map[string]loghttp.Streams{
			"error": {
				{Labels: loghttp.LabelSet{"app": "a", "job": "ns/svc-a", "traceID": "abc"}, Entries: []loghttp.Entry{{Line: "error happened"}}},
				{Labels: loghttp.LabelSet{"app": "b", "job": "ns/svc-b"}, Entries: []loghttp.Entry{{Line: "error too"}}},
				{Labels: loghttp.LabelSet{"app": "unknown", "job": "ns/unknown"}, Entries: []loghttp.Entry{{Line: "not in known set"}}},
			},
			"level": {
				{Labels: loghttp.LabelSet{"app": "a", "job": "ns/svc-a"}, Entries: []loghttp.Entry{{Line: "level=info"}}},
			},
		},
	}

	cfg := baseProbeCfg()
	cfg.BroadSelector = broadSel

	result, err := RunContentProbes(cfg, tsdbResult, classifyClient, kwClient)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Classify assertions (unchanged — broad only affects keywords).
	require.Equal(t, 2, result.Classify.TotalClassified)

	// Keyword assertions: broad strategy was used.
	// TotalProbed = number of keywords actually queried (not stream×keyword).
	require.Equal(t, len(bench.FilterableKeywords), result.Keywords.TotalProbed)

	// "error" matched both sel1 and sel2.
	require.Contains(t, result.Keywords.ByKeyword["error"], sel1)
	require.Contains(t, result.Keywords.ByKeyword["error"], sel2)

	// "level" matched only sel1.
	require.Contains(t, result.Keywords.ByKeyword["level"], sel1)
	require.NotContains(t, result.Keywords.ByKeyword["level"], sel2)

	// "unknown" stream not in known set — should not appear.
	for _, sels := range result.Keywords.ByKeyword {
		for _, sel := range sels {
			require.NotContains(t, sel, "unknown")
		}
	}

	// All queries used the broad selector, not per-stream selectors.
	for _, call := range kwClient.calls {
		require.Contains(t, call, broadSel, "all queries should use the broad selector")
	}
}

func TestRunKeywordPhaseBroad_ErrorRecovery(t *testing.T) {
	broadSel := `{namespace=~"test"}`
	sel1 := `{app="a", job="ns/svc-a"}`

	// Client that fails on "error" keyword but succeeds on others.
	kwClient := &fakeBroadKeywordClient{
		errKeywords: map[string]bool{"error": true},
		streamsByKeyword: map[string]loghttp.Streams{
			"level": {
				{Labels: loghttp.LabelSet{"app": "a", "job": "ns/svc-a"}, Entries: []loghttp.Entry{{Line: "level=info"}}},
			},
		},
	}

	result := runKeywordPhaseBroad(broadSel, []string{sel1}, kwClient,
		time.Unix(0, 0), time.Unix(3600, 0), 1000)

	// "error" was skipped (warning), others probed.
	require.Greater(t, result.TotalSkipped, 0)
	require.NotEmpty(t, result.Warnings)

	// "level" still works.
	require.Contains(t, result.ByKeyword["level"], sel1)
}

func TestRunKeywordPhaseBroad_NoMatches(t *testing.T) {
	broadSel := `{namespace=~"test"}`
	knownSelectors := []string{`{app="a", job="ns/svc-a"}`, `{app="b", job="ns/svc-b"}`}

	// Client returns empty streams for all keywords.
	kwClient := &fakeBroadKeywordClient{
		streamsByKeyword: map[string]loghttp.Streams{},
	}

	result := runKeywordPhaseBroad(broadSel, knownSelectors, kwClient,
		time.Unix(0, 0), time.Unix(3600, 0), 1000)

	require.Equal(t, len(bench.FilterableKeywords), result.TotalProbed)
	require.Empty(t, result.ByKeyword)
	require.Empty(t, result.Warnings)
}

// fakeBroadKeywordClient returns streams with full label sets for broad keyword
// probing tests. It maps keywords to pre-built Streams responses.
type fakeBroadKeywordClient struct {
	// streamsByKeyword maps keyword → Streams response. The keyword is
	// extracted from the query string (everything after |= ).
	streamsByKeyword map[string]loghttp.Streams
	// errKeywords, when a keyword is true, returns an error for that query.
	errKeywords map[string]bool
	calls       []string
}

func (f *fakeBroadKeywordClient) QueryRange(
	queryStr string,
	_ int,
	_, _ time.Time,
	_ logproto.Direction,
	_, _ time.Duration,
	_ bool,
) (*loghttp.QueryResponse, error) {
	f.calls = append(f.calls, queryStr)

	// Extract keyword from query: `{selector} |= "keyword"`.
	// Find the keyword between the last pair of quotes.
	keyword := ""
	for _, kw := range bench.FilterableKeywords {
		if keywordQuery("{unused}", kw) != "" {
			expected := `|= "` + kw + `"`
			if len(queryStr) >= len(expected) {
				idx := len(queryStr) - len(expected)
				if queryStr[idx:] == expected {
					keyword = kw
					break
				}
			}
		}
	}

	if f.errKeywords != nil && f.errKeywords[keyword] {
		return nil, errors.New("simulated broad keyword failure for " + keyword)
	}

	streams := f.streamsByKeyword[keyword]
	if streams == nil {
		streams = loghttp.Streams{}
	}
	return &loghttp.QueryResponse{
		Data: loghttp.QueryResponseData{
			Result: streams,
		},
	}, nil
}

// makeTSDBResult builds a tsdb.StructuralResult with the given selectors and
// entry counts per selector. Each selector gets a MergedStream with a single
// ChunkMeta whose Entries field equals the provided count.
func makeTSDBResult(selectorEntries map[string]uint32) *tsdb.StructuralResult {
	var selectors []string
	merged := make(map[string]tsdb.MergedStream, len(selectorEntries))
	labelSets := make(map[string]loghttp.LabelSet, len(selectorEntries))

	for sel, entries := range selectorEntries {
		selectors = append(selectors, sel)
		merged[sel] = tsdb.MergedStream{
			Selector: sel,
			Labels:   loghttp.LabelSet{},
			ChunkMetas: []tsdbindex.ChunkMeta{
				{Entries: entries},
			},
			SourceCount: 1,
		}
		labelSets[sel] = loghttp.LabelSet{}
	}

	// Sort for determinism.
	sort.Strings(selectors)

	return &tsdb.StructuralResult{
		AllSelectors:  selectors,
		LabelSets:     labelSets,
		MergedStreams: merged,
	}
}

const testBroadSelector = `{namespace=~"test"}`

// baseProbeCfg returns a ProbeConfig with Parallelism=1 and the test broad
// selector for deterministic tests.
func baseProbeCfg() ProbeConfig {
	return ProbeConfig{
		Parallelism:   1,
		From:          time.Unix(0, 0),
		To:            time.Unix(3600, 0),
		BroadSelector: testBroadSelector,
	}
}

// emptyBroadKeywordClient returns a fakeBroadKeywordClient that returns
// no matches for any keyword — useful when a test only cares about the
// classify phase.
func emptyBroadKeywordClient() *fakeBroadKeywordClient {
	return &fakeBroadKeywordClient{
		streamsByKeyword: map[string]loghttp.Streams{},
	}
}
