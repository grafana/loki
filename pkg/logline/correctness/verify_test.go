package correctness

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

func TestVerifyHints_AllResultsCovered(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []queryEntry{
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 0, 51, 0, time.UTC)}},
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 1, 9, 0, time.UTC)}},
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 1, report.HintRanges)
	require.Equal(t, 2, report.TotalResults)
	require.Equal(t, 2, report.CoveredResults)
	require.Equal(t, 0, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.True(t, report.Correct)
}

func TestVerifyHints_PartialCoverage(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []queryEntry{
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 0, 51, 0, time.UTC)}},
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)}}, // outside range
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 2, report.TotalResults)
	require.Equal(t, 1, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_FalsePositiveRanges(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}
	entries := []queryEntry{
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)}}, // outside all ranges
	}

	report := verifyHints(ranges, entries)
	require.Equal(t, 1, report.TotalResults)
	require.Equal(t, 0, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 1, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_EmptyResults(t *testing.T) {
	ranges := []hintprovider.HintTimeRange{
		{Start: time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC), End: time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)},
	}

	report := verifyHints(ranges, nil)
	require.Equal(t, 1, report.HintRanges)
	require.Equal(t, 0, report.TotalResults)
	require.Equal(t, 0, report.FalseNegatives)
	require.Equal(t, 1, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_EmptyHints(t *testing.T) {
	entries := []queryEntry{
		{Entry: logproto.Entry{Timestamp: time.Date(2026, 2, 26, 10, 5, 0, 0, time.UTC)}},
	}

	report := verifyHints(nil, entries)
	require.Equal(t, 0, report.HintRanges)
	require.Equal(t, 1, report.TotalResults)
	require.Equal(t, 0, report.CoveredResults)
	require.Equal(t, 1, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.False(t, report.Correct)
}

func TestVerifyHints_DocumentShards(t *testing.T) {
	stream := loghttp.LabelSet{"job": "api"}
	fp, _ := logline.StreamFingerprint(labels.FromMap(stream), nil)
	shard := logline.DocumentShard(fp, 5)
	ts := time.Date(2026, 2, 26, 10, 1, 0, 0, time.UTC)
	ranges := []hintprovider.HintTimeRange{
		{Start: ts.Add(-time.Second), End: ts.Add(time.Second), DocumentShard: shard, DocumentShardBits: 5},
		{Start: ts.Add(-time.Second), End: ts.Add(time.Second), DocumentShard: shard ^ 1, DocumentShardBits: 5},
	}
	entries := []queryEntry{{Entry: logproto.Entry{Timestamp: ts}, StreamLabels: stream}}

	report := verifyHints(ranges, entries)
	require.Equal(t, 1, report.CoveredResults)
	require.Equal(t, 1, report.FalsePositives, "the other shard's range covers no result's stream")
	require.True(t, report.Correct)

	report = verifyHints(ranges[1:], entries)
	require.Equal(t, 1, report.FalseNegatives, "a range of another document shard misses the stream")
	require.False(t, report.Correct)
}
