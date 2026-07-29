package distributor

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/grafana/loki/pkg/push"
)

// discardedBytesFor sums loki_discarded_bytes_total over every label combination carrying the
// given reason, so a test does not have to guess the retention/policy/format labels.
func discardedBytesFor(t *testing.T, reason string) float64 {
	t.Helper()

	ch := make(chan prometheus.Metric, 128)
	go func() {
		validation.DiscardedBytes.Collect(ch)
		close(ch)
	}()

	total := 0.0
	for m := range ch {
		var pb dto.Metric
		require.NoError(t, m.Write(&pb))
		for _, l := range pb.GetLabel() {
			if l.GetName() == validation.ReasonLabel && l.GetValue() == reason {
				total += pb.GetCounter().GetValue()
			}
		}
	}
	return total
}

// discardTestStream is a stream whose entries carry their own structured metadata and reference
// both sets of a shared pool, i.e. what the OTLP push path produces for a tenant with
// otlp_defer_structured_metadata_expansion on. withPool false is the same logical payload as the
// flag would be off: no pool, nothing referenced.
func discardTestStream(labelsStr string, entries int, withPool bool) logproto.Stream {
	stream := logproto.Stream{Labels: labelsStr}
	for i := 0; i < entries; i++ {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp:          time.Unix(int64(1600000000+i), 0),
			Line:               fmt.Sprintf("log line %d", i),
			StructuredMetadata: push.LabelsAdapter{{Name: "own", Value: "value"}},
		})
	}
	if withPool {
		stream.SharedStructuredMetadataSets = sharedTestPool()
		refAllEntries(&stream)
	}
	return stream
}

// TestDistributor_RateLimitedDiscardCountsSharedPoolOnce asserts that the bytes reported as
// discarded when a push is rate limited are the tenant-facing unexpanded size of the streams: the
// entries plus each set of the shared structured metadata pool exactly once per stream.
//
// The pool used to be left out entirely, so a flag-on tenant saw fewer discarded bytes than it was
// refused for: the byte count in the 429 message comes from the ingestion rate limit bucket, which
// does count the pool once per stream (streamEntriesSize in PushWithResolver). This asserts the two
// against each other, so the discard metric cannot drift from the accounting the rejection was
// decided on.
func TestDistributor_RateLimitedDiscardCountsSharedPoolOnce(t *testing.T) {
	const entries = 8

	for _, tc := range []struct {
		name     string
		withPool bool
	}{
		{name: "with shared pool", withPool: true},
		{name: "without shared pool", withPool: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation.DiscardedBytes.Reset()
			validation.DiscardedSamples.Reset()

			stream := discardTestStream(`{foo="bar"}`, entries, tc.withPool)

			// Unexpanded accounting: every entry's line and own structured metadata, plus the
			// pool once for the whole stream, however many entries reference it.
			expected := util.EntriesTotalSize(stream.Entries) + util.SharedSetsSize(stream.SharedStructuredMetadataSets)
			if tc.withPool {
				require.Greater(t, expected, util.EntriesTotalSize(stream.Entries), "the pool must contribute something")
			} else {
				require.Equal(t, expected, util.EntriesTotalSize(stream.Entries), "there is no pool to contribute")
			}

			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			// A burst of one byte rejects any push, so the whole request is discarded as
			// rate_limited.
			limits.IngestionRateMB = 1.0 / (1024 * 1024)
			limits.IngestionBurstSizeMB = 1.0 / (1024 * 1024)

			distributors, _ := prepare(t, 1, 3, limits, nil)
			distributors[0].tee = &mockTee{}

			err := pushDeferred(t, distributors[0], &logproto.PushRequest{Streams: []logproto.Stream{stream}})
			require.Error(t, err)

			// The 429 reports the bytes of the rate limit bucket, which is the same unexpanded
			// accounting. Asserting the message pins the discard metric to it.
			require.Contains(t, err.Error(), fmt.Sprintf("totaling '%d' bytes", expected))

			require.Equal(t, float64(expected), discardedBytesFor(t, validation.RateLimited))
		})
	}
}

// TestDistributor_ValidateLabelsDiscardCountsSharedPoolOnce covers the whole-stream discards
// reported by Validator.ValidateLabels, which reports for the entire stream and so has to count the
// shared pool once for it too.
func TestDistributor_ValidateLabelsDiscardCountsSharedPoolOnce(t *testing.T) {
	const entries = 4

	for _, tc := range []struct {
		name     string
		withPool bool
	}{
		{name: "with shared pool", withPool: true},
		{name: "without shared pool", withPool: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation.DiscardedBytes.Reset()
			validation.DiscardedSamples.Reset()

			// Two label names against a limit of one, so ValidateLabels rejects the stream with
			// max_label_names_per_series.
			stream := discardTestStream(`{foo="bar", baz="qux"}`, entries, tc.withPool)
			expected := util.EntriesTotalSize(stream.Entries) + util.SharedSetsSize(stream.SharedStructuredMetadataSets)

			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			limits.MaxLabelNamesPerSeries = 1

			distributors, _ := prepare(t, 1, 3, limits, nil)
			distributors[0].tee = &mockTee{}

			err := pushDeferred(t, distributors[0], &logproto.PushRequest{Streams: []logproto.Stream{stream}})
			require.Error(t, err)
			require.Contains(t, err.Error(), "has 2 label names; limit 1")

			require.Equal(t, float64(expected), discardedBytesFor(t, validation.MaxLabelNamesPerSeries))
			// The label failure fails parseStreamLabels, so the stream is also reported as
			// invalid_labels; that site is a whole-stream discard too and gets the same number.
			require.Equal(t, float64(expected), discardedBytesFor(t, validation.InvalidLabels))
		})
	}
}

// TestDistributor_MissingEnforcedLabelsDiscardCountsSharedPoolOnce covers the enforced-labels
// whole-stream discard, the third of the in-loop sites that used to omit the pool.
func TestDistributor_MissingEnforcedLabelsDiscardCountsSharedPoolOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withPool bool
	}{
		{name: "with shared pool", withPool: true},
		{name: "without shared pool", withPool: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation.DiscardedBytes.Reset()
			validation.DiscardedSamples.Reset()

			stream := discardTestStream(`{foo="bar"}`, 4, tc.withPool)
			expected := util.EntriesTotalSize(stream.Entries) + util.SharedSetsSize(stream.SharedStructuredMetadataSets)

			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			limits.EnforcedLabels = []string{"app"}

			distributors, _ := prepare(t, 1, 3, limits, nil)
			distributors[0].tee = &mockTee{}

			err := pushDeferred(t, distributors[0], &logproto.PushRequest{Streams: []logproto.Stream{stream}})
			require.Error(t, err)

			require.Equal(t, float64(expected), discardedBytesFor(t, validation.MissingEnforcedLabels))
		})
	}
}

// TestDistributor_BlockedIngestionDiscardCountsSharedPoolOnce covers the blocked-ingestion
// whole-stream discard.
func TestDistributor_BlockedIngestionDiscardCountsSharedPoolOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withPool bool
	}{
		{name: "with shared pool", withPool: true},
		{name: "without shared pool", withPool: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validation.DiscardedBytes.Reset()
			validation.DiscardedSamples.Reset()

			stream := discardTestStream(`{foo="bar"}`, 4, tc.withPool)
			expected := util.EntriesTotalSize(stream.Entries) + util.SharedSetsSize(stream.SharedStructuredMetadataSets)

			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.AllowStructuredMetadata = true
			limits.RejectOldSamples = false
			limits.DiscoverLogLevels = false
			limits.BlockIngestionUntil = flagext.Time(time.Now().Add(time.Hour))

			distributors, _ := prepare(t, 1, 3, limits, nil)
			distributors[0].tee = &mockTee{}

			err := pushDeferred(t, distributors[0], &logproto.PushRequest{Streams: []logproto.Stream{stream}})
			require.Error(t, err)

			require.Equal(t, float64(expected), discardedBytesFor(t, validation.BlockedIngestion))
		})
	}
}
