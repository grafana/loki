package kafka

import (
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

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
