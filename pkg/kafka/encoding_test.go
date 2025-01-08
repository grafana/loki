package kafka

import (
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestEncoderDecoder(t *testing.T) {
	tests := []struct {
		name        string
		stream      logproto.Stream
		maxSize     int
		expectSplit bool
	}{
		{
			name:        "Small stream, no split",
			stream:      generateStream(10, 100),
			maxSize:     1024 * 1024,
			expectSplit: false,
		},
		{
			name:        "Large stream, expect split",
			stream:      generateStream(1000, 1000),
			maxSize:     1024 * 10,
			expectSplit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoder, err := NewDecoder()
			require.NoError(t, err)

			records, err := Encode(0, "test-tenant", tt.stream, tt.maxSize)
			require.NoError(t, err)

			if tt.expectSplit {
				require.Greater(t, len(records), 1)
			} else {
				require.Equal(t, 1, len(records))
			}

			var decodedEntries []logproto.Entry
			var decodedLabels labels.Labels

			for _, record := range records {
				stream, ls, err := decoder.Decode(record.Value)
				require.NoError(t, err)
				decodedEntries = append(decodedEntries, stream.Entries...)
				if decodedLabels == nil {
					decodedLabels = ls
				} else {
					require.Equal(t, decodedLabels, ls)
				}
			}

			require.Equal(t, tt.stream.Labels, decodedLabels.String())
			require.Equal(t, len(tt.stream.Entries), len(decodedEntries))
			for i, entry := range tt.stream.Entries {
				require.Equal(t, entry.Timestamp.UTC(), decodedEntries[i].Timestamp.UTC())
				require.Equal(t, entry.Line, decodedEntries[i].Line)
			}
		})
	}
}

func TestEncoderSingleEntryTooLarge(t *testing.T) {
	stream := generateStream(1, 1000)

	_, err := Encode(0, "test-tenant", stream, 100)
	require.Error(t, err)
	require.Contains(t, err.Error(), "single entry size")
}

func TestDecoderInvalidData(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	_, _, err = decoder.Decode([]byte("invalid data"))
	require.Error(t, err)
}

func TestEncoderDecoderEmptyStream(t *testing.T) {
	decoder, err := NewDecoder()
	require.NoError(t, err)

	stream := logproto.Stream{
		Labels: `{app="test"}`,
	}

	records, err := Encode(0, "test-tenant", stream, 10<<20)
	require.NoError(t, err)
	require.Len(t, records, 1)

	decodedStream, decodedLabels, err := decoder.Decode(records[0].Value)
	require.NoError(t, err)
	require.Equal(t, stream.Labels, decodedLabels.String())
	require.Empty(t, decodedStream.Entries)
}

func BenchmarkEncodeDecode(b *testing.B) {
	decoder, _ := NewDecoder()
	stream := generateStream(1000, 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		records, err := Encode(0, "test-tenant", stream, 10<<20)
		if err != nil {
			b.Fatal(err)
		}
		for _, record := range records {
			_, _, err := decoder.Decode(record.Value)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

// Helper function to generate a test stream
func generateStream(entries, lineLength int) logproto.Stream {
	stream := logproto.Stream{
		Labels:  `{app="test", env="prod"}`,
		Entries: make([]logproto.Entry, entries),
	}

	for i := 0; i < entries; i++ {
		stream.Entries[i] = logproto.Entry{
			Timestamp: time.Now(),
			Line:      generateRandomString(lineLength),
		}
	}

	return stream
}

// Helper function to generate a random string
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func TestEncodeDecodeStreamMetadata(t *testing.T) {
	tests := []struct {
		name       string
		stream     logproto.Stream
		partition  int32
		topic      string
		tenantID   string
		lastSeenAt time.Time
		expectErr  bool
	}{
		{
			name: "Valid metadata",
			stream: logproto.Stream{
				Labels: `{app="test"}`,
				Hash:   12345,
			},
			partition:  1,
			topic:      "logs",
			tenantID:   "tenant-1",
			lastSeenAt: time.Now().Truncate(time.Millisecond),
			expectErr:  false,
		},
		{
			name: "Empty labels - should error",
			stream: logproto.Stream{
				Labels: "",
				Hash:   67890,
			},
			partition:  2,
			topic:      "metrics",
			tenantID:   "tenant-2",
			lastSeenAt: time.Now().Truncate(time.Millisecond),
			expectErr:  true,
		},
		{
			name: "Zero hash - should error",
			stream: logproto.Stream{
				Labels: `{app="test"}`,
				Hash:   0,
			},
			partition:  3,
			topic:      "traces",
			tenantID:   "tenant-3",
			lastSeenAt: time.Now().Truncate(time.Millisecond),
			expectErr:  true,
		},
		{
			name: "Empty labels and zero hash - should error",
			stream: logproto.Stream{
				Labels: "",
				Hash:   0,
			},
			partition:  4,
			topic:      "traces",
			tenantID:   "tenant-4",
			lastSeenAt: time.Now().Truncate(time.Millisecond),
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode metadata
			record := EncodeStreamMetadata(tt.partition, tt.topic, tt.tenantID, tt.stream, tt.lastSeenAt)
			if tt.expectErr {
				require.Nil(t, record)
				return
			}

			require.NotNil(t, record)
			require.NotNil(t, record.Value)
			require.Equal(t, tt.topic+metadataTopicSuffix, record.Topic)
			require.Equal(t, tt.partition, record.Partition)
			require.Equal(t, []byte(tt.tenantID), record.Key)

			// Decode metadata
			metadata, err := DecodeStreamMetadata(record)
			require.NoError(t, err)
			require.NotNil(t, metadata)

			// Verify decoded values
			require.Equal(t, tt.stream.Hash, metadata.StreamHash)
			require.Equal(t, tt.lastSeenAt.UnixNano(), metadata.LastSeenAt)

			// Return metadata to pool
			metadataPool.Put(metadata)
		})
	}

	t.Run("Decode nil record", func(t *testing.T) {
		metadata, err := DecodeStreamMetadata(nil)
		require.Error(t, err)
		require.Nil(t, metadata)
	})

	t.Run("Decode nil value", func(t *testing.T) {
		record := &kgo.Record{
			Value: nil,
		}
		metadata, err := DecodeStreamMetadata(record)
		require.Error(t, err)
		require.Nil(t, metadata)
	})

	t.Run("Decode invalid value", func(t *testing.T) {
		record := &kgo.Record{
			Value: []byte("invalid data"),
		}
		metadata, err := DecodeStreamMetadata(record)
		require.Error(t, err)
		require.Nil(t, metadata)
	})
}
