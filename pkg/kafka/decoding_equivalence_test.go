package kafka

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestNoDifferenceBetweenEncodingsForConsumers having sent a stream over the wire, a consumer cannot tell which of the two
// encodings the producer chose.
//
// The two streams are compared after decoding rather than against the input, so that the
// timestamp normalisation a protobuf round trip performs applies equally to both.
func TestNoDifferenceBetweenEncodingsForConsumers(t *testing.T) {
	const nestedEntriesPerRecord = 7

	tests := []struct {
		name       string
		stream     logproto.Stream
		maxSize    int
		flatSplits bool
	}{
		{
			name:    "no entries",
			stream:  logproto.Stream{Labels: `{app="test"}`},
			maxSize: 10 << 20,
		},
		{
			name:    "no entries with a hash",
			stream:  logproto.Stream{Labels: `{app="test"}`, Hash: 1234},
			maxSize: 10 << 20,
		},
		{
			name: "one entry",
			stream: logproto.Stream{Labels: `{app="test"}`, Hash: 1234, Entries: []push.Entry{
				{Timestamp: time.Unix(0, 1), Line: "only"},
			}},
			maxSize: 10 << 20,
		},
		{
			name: "entries with structured metadata",
			stream: logproto.Stream{Labels: `{app="test"}`, Hash: 1234, Entries: []push.Entry{
				{Timestamp: time.Unix(0, 1), Line: "a", StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: "1"}}},
				{Timestamp: time.Unix(0, 2), Line: "b", StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: "2"}}},
			}},
			maxSize: 10 << 20,
		},
		{
			name:    "many entries with the flat side in one record",
			stream:  generateStream(200, 100),
			maxSize: 10 << 20,
		},
		{
			// The flat side splits here too, at boundaries unrelated to the nested side's.
			name:       "many entries with both sides split",
			stream:     generateStream(200, 100),
			maxSize:    4096,
			flatSplits: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder, err := NewDecoder()
			require.NoError(t, err)
			nestedRecs := nestedRecords(t, tt.stream, nestedEntriesPerRecord)
			flatRecs, err := Encode(0, "test-tenant", tt.stream, tt.maxSize)
			require.NoError(t, err)

			// Asserted so that a case meant to exercise concatenation cannot quietly
			// collapse into a single record on either side.
			if tt.flatSplits {
				require.Greater(t, len(flatRecs), 1)
			} else {
				require.Len(t, flatRecs, 1)
			}
			if len(tt.stream.Entries) > nestedEntriesPerRecord {
				require.Greater(t, len(nestedRecs), 1)
			} else {
				require.Len(t, nestedRecs, 1)
			}

			flat := readStream(t, decoder, flatRecs)
			nested := readStream(t, decoder, nestedRecs)

			require.Equal(t, flat, nested)
			require.Len(t, flat.Entries, len(tt.stream.Entries))
		})
	}
}

func TestDecodeNestedRecordYieldsFlatStream(t *testing.T) {
	nested := logproto.InternalStreamAdapter{
		Labels: `{app="test"}`,
		Hash:   1234,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs: []push.LabelAdapter{{Name: "host", Value: "host-1"}},
			ScopeLogs: []logproto.ScopeLogs{{
				Attrs: []push.LabelAdapter{{Name: "scope", Value: "lib"}},
				Entries: []push.Entry{
					{Timestamp: time.Unix(0, 1), Line: "a", StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: "1"}}},
					{Timestamp: time.Unix(0, 2), Line: "b"},
				},
			}},
		}},
	}

	decoder, err := NewDecoder()
	require.NoError(t, err)

	got, ls, err := decoder.Decode(nestedRecord(t, nested).Value)
	require.NoError(t, err)

	require.Equal(t, `{app="test"}`, ls.String())
	require.Equal(t, uint64(1234), got.Hash)
	require.Len(t, got.Entries, 2)

	require.Equal(t, "a", got.Entries[0].Line)
	require.Equal(t, push.LabelsAdapter{
		{Name: "trace_id", Value: "1"},
		{Name: "host", Value: "host-1"},
		{Name: "scope", Value: "lib"},
	}, got.Entries[0].StructuredMetadata)

	require.Equal(t, "b", got.Entries[1].Line)
	require.Equal(t, push.LabelsAdapter{
		{Name: "host", Value: "host-1"},
		{Name: "scope", Value: "lib"},
	}, got.Entries[1].StructuredMetadata)
}

// TestDecodeInterleavedEncodings guards the reuse the Decoder does between calls: nothing
// from a record may survive into the next one, in either direction.
func TestDecodeInterleavedEncodings(t *testing.T) {
	flatStream := logproto.Stream{Labels: `{app="flat"}`, Hash: 999, Entries: []push.Entry{
		{Timestamp: time.Unix(0, 1), Line: "flat line"},
	}}
	nestedStream := logproto.InternalStreamAdapter{
		Labels: `{app="nested"}`,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs: []push.LabelAdapter{{Name: "host", Value: "host-1"}},
			ScopeLogs: []logproto.ScopeLogs{{
				Entries: []push.Entry{{Timestamp: time.Unix(0, 2), Line: "nested line"}},
			}},
		}},
	}

	flatRecords, err := Encode(0, "test-tenant", flatStream, 10<<20)
	require.NoError(t, err)
	nestedValue := nestedRecord(t, nestedStream).Value

	decoder, err := NewDecoder()
	require.NoError(t, err)

	// Twice through, so that each encoding is decoded both first and after the other.
	for range 2 {
		got, ls, err := decoder.Decode(flatRecords[0].Value)
		require.NoError(t, err)
		require.Equal(t, `{app="flat"}`, ls.String())
		require.Equal(t, uint64(999), got.Hash)
		require.Len(t, got.Entries, 1)
		require.Equal(t, "flat line", got.Entries[0].Line)
		require.Empty(t, got.Entries[0].StructuredMetadata)

		got, ls, err = decoder.Decode(nestedValue)
		require.NoError(t, err)
		require.Equal(t, `{app="nested"}`, ls.String())
		require.Zero(t, got.Hash, "hash of the previous record leaked into this one")
		require.Len(t, got.Entries, 1)
		require.Equal(t, "nested line", got.Entries[0].Line)
		require.Equal(t, push.LabelsAdapter{{Name: "host", Value: "host-1"}}, got.Entries[0].StructuredMetadata)
	}
}

func TestDecodeRejectsDataInNeitherEncoding(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	_, err = decoder.DecodeWithoutLabels([]byte("invalid data"))
	require.ErrorContains(t, err, "nested:")
	require.ErrorContains(t, err, "flat:")

	_, _, err = decoder.Decode([]byte("invalid data"))
	require.Error(t, err)
}

// TestDecodeReportsBothFailuresForATruncatedNestedRecord covers the case the combined error
// exists for: the record was written in the nested encoding, so the flat failure alone would
// describe a wire format it was never in.
func TestDecodeReportsBothFailuresForATruncatedNestedRecord(t *testing.T) {
	nested := logproto.InternalStreamAdapter{
		Labels: `{app="test"}`,
		ResourceLogs: []logproto.ResourceLogs{{
			Attrs: []push.LabelAdapter{{Name: "host", Value: "host-1"}},
			ScopeLogs: []logproto.ScopeLogs{{
				Entries: []push.Entry{{Timestamp: time.Unix(0, 1), Line: "a line long enough to cut"}},
			}},
		}},
	}

	decoder, err := NewDecoder()
	require.NoError(t, err)

	value := nestedRecord(t, nested).Value
	truncated := value[:len(value)-8]
	_, err = decoder.DecodeWithoutLabels(truncated)
	require.ErrorContains(t, err, "unexpected EOF", "the nested failure is the one that explains the record")
}

// decodeFlatOnly is DecodeWithoutLabels as it stood before this change: unmarshal as a flat
// stream, with no second encoding to fall back to.
//
// It exists only so BenchmarkDecode can price the fallback against what it replaced. A record
// in the nested encoding fails here, which is the rollout invariant asserted by
// TestEncodingsAreMutuallyUndecodable.
func decodeFlatOnly(data []byte) (logproto.Stream, error) {
	stream := logproto.Stream{}
	if err := stream.Unmarshal(data); err != nil {
		return logproto.Stream{}, fmt.Errorf("failed to unmarshal stream: %w", err)
	}
	return stream, nil
}

// BenchmarkDecode prices the change: what a record in the old flat encoding cost to decode
// before the fallback existed, what the same record costs now that the nested message is
// attempted first, and what a record in the new nested encoding costs.
//
// Both record shapes are measured because the failed attempt is a cost per record, not per
// entry: it is plain on a record holding one entry and invisible on one holding a thousand.
func BenchmarkDecode(b *testing.B) {
	shapes := []struct {
		name    string
		entries int
	}{
		{"1 entry per record", 1},
		{"1000 entries per record", 1000},
	}

	for _, shape := range shapes {
		stream := generateStream(shape.entries, 200)

		flatRecords, err := Encode(0, "test-tenant", stream, 10<<20)
		require.NoError(b, err)
		require.Len(b, flatRecords, 1)

		// The shape the new encoding takes for traffic that arrived over the native push API:
		// nested, but with nothing lifted out of the entries.
		nested := logproto.FromStream(stream)

		// The shape it takes for OTLP traffic, where lifting the attributes out is the point.
		// Expanding them back is the work the new encoding adds to a decode.
		shared := logproto.FromStream(stream)
		shared.ResourceLogs[0].Attrs = []push.LabelAdapter{
			{Name: "host", Value: "host-1"},
			{Name: "cluster", Value: "prod-eu-west-2"},
			{Name: "namespace", Value: "loki-prod-029"},
		}

		// The same OTLP data as shared, in the encoding it uses today: every entry carrying
		// its own copy of the three attributes. This is the comparator that says what the
		// nesting buys, which the wire_bytes of shared alone does not show.
		expandedRecords, err := Encode(0, "test-tenant", shared.ToStream(), 10<<20)
		require.NoError(b, err)
		require.Len(b, expandedRecords, 1)

		// Two groups of three, one per kind of traffic. Within a group the first pair
		// isolates the code change with the format held fixed, and the second pair the
		// format change with the code held fixed. The fourth combination, a nested record
		// read by the prior code, is absent because it does not decode at all: that is the
		// rollout invariant, and it belongs in a test rather than a benchmark.
		cases := []struct {
			name      string
			data      []byte
			priorCode bool
		}{
			{name: "old format + old code", data: flatRecords[0].Value, priorCode: true},
			{name: "old format + new code", data: flatRecords[0].Value},
			{name: "new format + new code", data: nestedRecord(b, nested).Value},

			{name: "old format with attrs expanded + old code", data: expandedRecords[0].Value, priorCode: true},
			{name: "old format with attrs expanded + new code", data: expandedRecords[0].Value},
			{name: "new format with attrs shared + new code", data: nestedRecord(b, shared).Value},
		}

		for _, tc := range cases {
			b.Run(shape.name+"/"+tc.name, func(b *testing.B) {
				decoder, err := NewDecoder()
				require.NoError(b, err)

				for b.Loop() {
					var err error
					if tc.priorCode {
						_, err = decodeFlatOnly(tc.data)
					} else {
						_, err = decoder.DecodeWithoutLabels(tc.data)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				// Reported after the loop: b.Loop clears custom metrics on its first
				// iteration, so a value reported before it never reaches the output.
				b.ReportMetric(float64(len(tc.data)), "wire_bytes")
			})
		}
	}
}

// TestDecodeWithoutLabelsIsSafeForConcurrentUse pins the property the ingester's consumer
// relies on: it decodes records from several goroutines sharing one Decoder.
func TestDecodeWithoutLabelsIsSafeForConcurrentUse(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	var values [][]byte
	for i := range 8 {
		stream := logproto.Stream{
			Labels:  fmt.Sprintf(`{app="test-%d"}`, i),
			Hash:    uint64(i),
			Entries: []push.Entry{{Timestamp: time.Unix(0, int64(i+1)), Line: fmt.Sprintf("line-%d", i)}},
		}

		flat, err := Encode(0, "test-tenant", stream, 10<<20)
		require.NoError(t, err)
		values = append(values, flat[0].Value, nestedRecord(t, logproto.FromStream(stream)).Value)
	}

	done := make(chan struct{})
	for _, value := range values {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				if _, err := decoder.DecodeWithoutLabels(value); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for range values {
		<-done
	}
}

// nestedRecord marshals a stream in the nested encoding into one record.
//
// The tests put nested records on the wire themselves rather than through a producer: the
// decoder is what is under test, and deciding record boundaries from a size limit is the
// producer's concern.
func nestedRecord(t testing.TB, stream logproto.InternalStreamAdapter) *kgo.Record {
	t.Helper()
	data, err := stream.Marshal()
	require.NoError(t, err)
	return &kgo.Record{Key: []byte("test-tenant"), Value: data}
}

// nestedRecords writes stream in the nested encoding, starting a new record every
// entriesPerRecord entries. A stream with no entries still yields one record, as the flat
// encoder produces for the same input.
func nestedRecords(t testing.TB, stream logproto.Stream, entriesPerRecord int) []*kgo.Record {
	t.Helper()

	var records []*kgo.Record
	for start := 0; ; start += entriesPerRecord {
		end := min(start+entriesPerRecord, len(stream.Entries))
		records = append(records, nestedRecord(t, logproto.FromStream(logproto.Stream{
			Labels:  stream.Labels,
			Hash:    stream.Hash,
			Entries: stream.Entries[start:end],
		})))
		if end >= len(stream.Entries) {
			return records
		}
	}
}

// readStream is what a consumer sees: every record of a stream decoded and its entries
// concatenated. Record boundaries are deliberately not part of the result, because the two
// encoders split at different points and a consumer does not care where.
func readStream(t *testing.T, decoder *Decoder, records []*kgo.Record) logproto.Stream {
	t.Helper()
	require.NotEmpty(t, records)

	var out logproto.Stream
	for i, rec := range records {
		got, err := decoder.DecodeWithoutLabels(rec.Value)
		require.NoError(t, err)

		if i == 0 {
			out.Labels, out.Hash = got.Labels, got.Hash
		} else {
			require.Equal(t, out.Labels, got.Labels, "every record of a stream must carry the same labels")
			require.Equal(t, out.Hash, got.Hash, "every record of a stream must carry the same hash")
		}
		out.Entries = append(out.Entries, got.Entries...)
	}
	return out
}
