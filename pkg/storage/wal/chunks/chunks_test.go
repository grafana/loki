package chunks

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/wal/testdata"
)

func TestChunkReaderWriter(t *testing.T) {
	tests := []struct {
		name    string
		entries []*logproto.Entry
	}{
		{
			name: "Single entry",
			entries: []*logproto.Entry{
				{Timestamp: time.Now(), Line: "This is a single log entry."},
			},
		},
		{
			name: "Multiple entries",
			entries: []*logproto.Entry{
				{Timestamp: time.Now(), Line: "Log entry 1"},
				{Timestamp: time.Now().Add(1 * time.Second), Line: "Log entry 2"},
				{Timestamp: time.Now().Add(2 * time.Second), Line: "Log entry 3"},
			},
		},
		{
			name: "Different spacing",
			entries: []*logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "Log entry 1"},
				{Timestamp: time.Unix(0, 2), Line: "Log entry 2"},
				{Timestamp: time.Unix(0, 4), Line: "Log entry 3"},
				{Timestamp: time.Unix(0, 5), Line: "Log entry 4"},
			},
		},
		{
			// todo: fix dod for variable timestamp delta causing negative dod
			name: "Many entries",
			entries: func() []*logproto.Entry {
				entries := make([]*logproto.Entry, 1000)
				for i := 0; i < 1000; i++ {
					entries[i] = &logproto.Entry{
						Timestamp: time.Now().Add(time.Duration(i) * time.Second),
						Line:      "Log entry " + strconv.Itoa(i+1),
					}
				}
				return entries
			}(),
		},
		{
			name: "Entries with varying lengths",
			entries: []*logproto.Entry{
				{Timestamp: time.Now(), Line: "Short"},
				{Timestamp: time.Now().Add(1 * time.Second), Line: "A bit longer log entry"},
				{Timestamp: time.Now().Add(2 * time.Second), Line: "An even longer log entry than the previous one"},
			},
		},
		{
			name: "Empty lines",
			entries: []*logproto.Entry{
				{Timestamp: time.Now(), Line: ""},
				{Timestamp: time.Now().Add(1 * time.Second), Line: ""},
				{Timestamp: time.Now().Add(2 * time.Second), Line: ""},
			},
		},
		{
			name: "Some Empty lines",
			entries: []*logproto.Entry{
				{Timestamp: time.Now(), Line: ""},
				{Timestamp: time.Now().Add(1 * time.Second), Line: ""},
				{Timestamp: time.Now().Add(2 * time.Second), Line: "foo"},
				{Timestamp: time.Now().Add(4 * time.Second), Line: ""},
				{Timestamp: time.Now().Add(9 * time.Second), Line: "bar"},
			},
		},
		{
			name:    "No entries",
			entries: []*logproto.Entry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			// Write the chunk
			_, err := WriteChunk(&buf, tt.entries, EncodingSnappy)
			require.NoError(t, err, "writeChunk failed")

			// Read the chunk
			reader, err := NewChunkReader(buf.Bytes())
			require.NoError(t, err, "NewChunkReader failed")
			defer reader.Close()

			var readEntries []*logproto.Entry
			for reader.Next() {
				ts, l := reader.At()
				readEntries = append(readEntries, &logproto.Entry{
					Timestamp: time.Unix(0, ts),
					Line:      string(l),
				})
			}
			require.NoError(t, reader.Err(), "reader encountered error")
			require.Len(t, readEntries, len(tt.entries))
			for i, entry := range tt.entries {
				require.Equal(t, entry.Line, readEntries[i].Line, "Lines don't match", i)
				require.Equal(t, entry.Timestamp.UnixNano(), readEntries[i].Timestamp.UnixNano(), "Timestamps don't match", i)
			}
		})
	}
}

func TestChunkReaderWriterWithLogGenerator(t *testing.T) {
	filenames := testdata.Files()

	for _, filename := range filenames {
		t.Run(filename, func(t *testing.T) {
			gen := testdata.NewLogGenerator(t, filename)
			defer gen.Close()

			var entries []*logproto.Entry
			for more, line := gen.Next(); more; more, line = gen.Next() {
				entries = append(entries, &logproto.Entry{
					Timestamp: time.Now(),
					Line:      string(line),
				})
				if len(entries) >= 10000 {
					break
				}
			}

			var buf bytes.Buffer

			// Write the chunk
			_, err := WriteChunk(&buf, entries, EncodingSnappy)
			require.NoError(t, err, "writeChunk failed")

			// Read the chunk
			reader, err := NewChunkReader(buf.Bytes())
			require.NoError(t, err, "NewChunkReader failed")
			defer reader.Close()

			var readEntries []*logproto.Entry
			for reader.Next() {
				ts, l := reader.At()
				readEntries = append(readEntries, &logproto.Entry{
					Timestamp: time.Unix(0, ts),
					Line:      string(l),
				})
			}
			require.NoError(t, reader.Err(), "reader encountered error")
			require.Len(t, readEntries, len(entries))
			for i, entry := range entries {
				require.Equal(t, entry.Line, readEntries[i].Line, "Lines don't match", i)
				require.Equal(t, entry.Timestamp.UnixNano(), readEntries[i].Timestamp.UnixNano(), "Timestamps don't match", i)
			}
		})
	}
}

// BenchmarkWriteChunk benchmarks the writeChunk function.
func BenchmarkWriteChunk(b *testing.B) {
	// Generate sample log entries
	entries := generateLogEntries(1000)
	// Reset the buffer for each iteration
	buf := bytes.NewBuffer(make([]byte, 0, 5<<20))

	b.ReportAllocs()
	b.ResetTimer()

	// Run the benchmark
	for n := 0; n < b.N; n++ {
		buf.Reset()
		// Call the writeChunk function
		_, err := WriteChunk(buf, entries, EncodingSnappy)
		if err != nil {
			b.Fatalf("writeChunk failed: %v", err)
		}
	}
}

var (
	lineBuf []byte
	ts      int64
)

// Benchmark reads with log generator
func BenchmarkReadChunkWithLogGenerator(b *testing.B) {
	filenames := testdata.Files()
	for _, filename := range filenames {
		b.Run(filename, func(b *testing.B) {
			gen := testdata.NewLogGenerator(b, filename)
			defer gen.Close()

			var entries []*logproto.Entry
			for more, line := gen.Next(); more; more, line = gen.Next() {
				entries = append(entries, &logproto.Entry{
					Timestamp: time.Now(),
					Line:      string(line),
				})
				if len(entries) >= 100000 {
					break
				}
			}

			// Reset the buffer for each iteration
			buf := bytes.NewBuffer(make([]byte, 0, 5<<20))
			_, err := WriteChunk(buf, entries, EncodingSnappy)
			if err != nil {
				b.Fatalf("writeChunk failed: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()

			// Run the benchmark
			for n := 0; n < b.N; n++ {
				reader, err := NewChunkReader(buf.Bytes())
				require.NoError(b, err, "NewChunkReader failed")

				for reader.Next() {
					ts, lineBuf = reader.At()
				}
				reader.Close()
			}
		})
	}
}

// Benchmark with log generator
func BenchmarkWriteChunkWithLogGenerator(b *testing.B) {
	filenames := testdata.Files()

	for _, filename := range filenames {
		for _, count := range []int{1000, 10000, 100000} {
			b.Run(fmt.Sprintf("%s-%d", filename, count), func(b *testing.B) {
				gen := testdata.NewLogGenerator(b, filename)
				defer gen.Close()

				var entries []*logproto.Entry
				for more, line := gen.Next(); more; more, line = gen.Next() {
					entries = append(entries, &logproto.Entry{
						Timestamp: time.Now(),
						Line:      string(line),
					})
					if len(entries) >= count {
						break
					}
				}

				// Reset the buffer for each iteration
				buf := bytes.NewBuffer(make([]byte, 0, 5<<20))

				b.ReportAllocs()
				b.ResetTimer()

				// Run the benchmark
				for n := 0; n < b.N; n++ {
					buf.Reset()
					// Call the writeChunk function
					_, err := WriteChunk(buf, entries, EncodingSnappy)
					if err != nil {
						b.Fatalf("writeChunk failed: %v", err)
					}
				}
			})
		}
	}
}

// generateLogEntries generates a slice of logproto.Entry with the given count.
func generateLogEntries(count int) []*logproto.Entry {
	entries := make([]*logproto.Entry, count)
	for i := 0; i < count; i++ {
		entries[i] = &logproto.Entry{
			Timestamp: time.Now(),
			Line:      "This is a sample log entry.",
		}
	}
	return entries
}
