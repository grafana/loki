package rules

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// testStorage is an in-memory storage backend for unit tests.
// It stores log streams and allows querying them via the Querier interface.
type testStorage struct {
	streams []logproto.Stream
	// Current time for time-based queries
	currentTime time.Time
}

// newTestStorage creates a new test storage instance.
func newTestStorage() *testStorage {
	return &testStorage{
		streams:     []logproto.Stream{},
		currentTime: time.Unix(0, 0).UTC(),
	}
}

// parseAndLoadStreams parses the test stream definitions and loads them into storage.
// Stream format examples:
//   - '{job="app"}' with lines ['{"level":"info"}', '{"level":"error"}']
//   - '{job="api"}' with entries containing line and structured_metadata
func (ts *testStorage) parseAndLoadStreams(inputStreams []stream, interval model.Duration) error {
	for _, is := range inputStreams {
		// Validate stream format
		if err := is.Validate(); err != nil {
			return err
		}

		logStream, err := ts.parseStream(is, interval)
		if err != nil {
			return fmt.Errorf("failed to parse stream %s: %w", is.Labels, err)
		}
		ts.streams = append(ts.streams, logStream)
	}
	return nil
}

// parseStream converts a stream definition into a logproto.Stream.
func (ts *testStorage) parseStream(s stream, interval model.Duration) (logproto.Stream, error) {
	// Parse labels
	lbls, err := parseStreamLabels(s.Labels)
	if err != nil {
		return logproto.Stream{}, fmt.Errorf("failed to parse labels: %w", err)
	}

	var entries []logproto.Entry
	baseTime := time.Unix(0, 0).UTC()

	// Create log entries from explicit log lines
	entries = make([]logproto.Entry, len(s.Lines))
	for i, line := range s.Lines {
		timestamp := baseTime.Add(time.Duration(interval) * time.Duration(i))
		entries[i] = logproto.Entry{
			Timestamp: timestamp,
			Line:      line,
		}
	}

	stream := logproto.Stream{
		Labels:  lbls.String(),
		Entries: entries,
		Hash:    labels.StableHash(lbls),
	}

	return stream, nil
}

// parseStreamLabels parses a LogQL label selector string into labels.Labels.
// Supports format: {job="test", instance="localhost"}
// Only equality matchers (=) are supported, not regex matchers (=~, !=, !~).
func parseStreamLabels(labelStr string) (labels.Labels, error) {
	labelStr = strings.TrimSpace(labelStr)

	// Use LogQL parser to parse label selector
	matchers, err := syntax.ParseMatchers(labelStr, false)
	if err != nil {
		return labels.EmptyLabels(), fmt.Errorf("failed to parse labels: %w", err)
	}

	// Convert matchers to labels
	lblBuilder := labels.NewBuilder(labels.EmptyLabels())
	for _, m := range matchers {
		// Only support equality matchers for stream selectors
		if m.Type != labels.MatchEqual {
			return labels.EmptyLabels(), fmt.Errorf("only equality matchers (=) are supported in stream selectors, got: %s", m)
		}
		lblBuilder.Set(m.Name, m.Value)
	}

	return lblBuilder.Labels(), nil
}

// Querier returns a logql.Querier that can query the test storage.
func (ts *testStorage) Querier() logql.Querier {
	return logql.NewMockQuerier(0, ts.streams)
}

// GetStreams returns all stored streams (for testing purposes).
func (ts *testStorage) GetStreams() []logproto.Stream {
	return ts.streams
}

// GetStreamsByLabels returns streams matching the given label matchers.
func (ts *testStorage) GetStreamsByLabels(matchers []*labels.Matcher) []logproto.Stream {
	var matched []logproto.Stream

outer:
	for _, stream := range ts.streams {
		lbls, err := parseStreamLabels(stream.Labels)
		if err != nil {
			continue
		}

		for _, matcher := range matchers {
			if !matcher.Matches(lbls.Get(matcher.Name)) {
				continue outer
			}
		}
		matched = append(matched, stream)
	}

	return matched
}

// SetCurrentTime sets the current time for the storage (used for time-based queries).
func (ts *testStorage) SetCurrentTime(t time.Time) {
	ts.currentTime = t
}

// GetEntriesInRange returns all log entries within the specified time range.
func (ts *testStorage) GetEntriesInRange(start, end time.Time) []logproto.Entry {
	var entries []logproto.Entry

	for _, stream := range ts.streams {
		for _, entry := range stream.Entries {
			if (entry.Timestamp.Equal(start) || entry.Timestamp.After(start)) &&
				(entry.Timestamp.Before(end) || entry.Timestamp.Equal(end)) {
				entries = append(entries, entry)
			}
		}
	}

	// Sort by timestamp
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	return entries
}

// SelectLogs implements a simple SelectLogs for testing without full pipeline support.
func (ts *testStorage) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	// Use the built-in MockQuerier for proper log selection
	querier := ts.Querier()
	return querier.SelectLogs(ctx, params)
}

// SelectSamples implements SelectSamples for metric queries.
func (ts *testStorage) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	// Use the built-in MockQuerier for proper sample selection
	querier := ts.Querier()
	return querier.SelectSamples(ctx, params)
}

// Clear clears all stored streams from the storage.
func (ts *testStorage) Clear() {
	ts.streams = []logproto.Stream{}
}
