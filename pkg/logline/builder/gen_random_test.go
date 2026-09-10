package builder

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

// TestGenerateSyntheticRandom writes a pathological capture file where every
// log line is random [a-z0-9]. Random content makes almost every 6-gram unique,
// so the flat builder's dedup collapses to ~nothing — the worst case for a
// dedup-based architecture (maximal run volume, sort work, and unique terms).
//
// Randomness is an AES-CTR keystream (hardware-accelerated, ~GB/s) mapped to a
// 36-char alphabet. Deterministic (fixed key/IV) so the dataset is reproducible.
//
//	GEN_RANDOM=1 go test ./pkg/logline/builder/ -run TestGenerateSyntheticRandom -v -timeout 20m
//
// Env: GEN_OUT (path), GEN_MB (target value MiB, default 250),
// GEN_LINE_LEN (chars/line, default 100), GEN_ENTRIES (entries/record, default 50).
func TestGenerateSyntheticRandom(t *testing.T) {
	if os.Getenv("GEN_RANDOM") == "" {
		t.Skip("Set GEN_RANDOM=1 to generate the synthetic random dataset")
	}
	out := envOr("GEN_OUT", "testdata/kafka_records_random250.bin")
	targetBytes := int64(envInt("GEN_MB", 250)) << 20
	lineLen := envInt("GEN_LINE_LEN", 100)
	entriesPerRecord := envInt("GEN_ENTRIES", 50)

	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	// AES-CTR keystream generator: encrypting zeros yields the keystream.
	var key [16]byte
	var iv [16]byte
	copy(key[:], "logline-random!!")
	block, err := aes.NewCipher(key[:])
	require.NoError(t, err)
	ctr := cipher.NewCTR(block, iv[:])
	const ksChunk = 1 << 20
	zeros := make([]byte, ksChunk)
	ks := make([]byte, ksChunk)
	ksPos := ksChunk // force initial refill
	nextByte := func() byte {
		if ksPos >= len(ks) {
			ctr.XORKeyStream(ks, zeros)
			ksPos = 0
		}
		b := ks[ksPos]
		ksPos++
		return b
	}

	f, err := os.Create(out)
	require.NoError(t, err)
	w := bufio.NewWriterSize(f, 1<<20)

	// count placeholder (patched at the end).
	require.NoError(t, binary.Write(w, binary.LittleEndian, uint32(0)))

	// Spread timestamps uniformly over the last 7 days so multiple date buckets
	// (and, at high shard counts, many (date,shard) files) are exercised.
	now := time.Now().UTC()
	spanNanos := int64(7 * 24 * time.Hour)

	lineBuf := make([]byte, lineLen)
	var (
		valueBytes  int64
		recordCount uint32
	)
	for valueBytes < targetBytes {
		entries := make([]logproto.Entry, entriesPerRecord)
		for i := 0; i < entriesPerRecord; i++ {
			for j := 0; j < lineLen; j++ {
				lineBuf[j] = alphabet[nextByte()%36]
			}
			// Deterministic-but-scattered timestamp from keystream bytes.
			var r uint64
			for k := 0; k < 6; k++ {
				r = (r << 8) | uint64(nextByte())
			}
			offset := int64(r % uint64(spanNanos))
			entries[i] = logproto.Entry{
				Timestamp: now.Add(-time.Duration(offset)),
				Line:      string(lineBuf),
			}
		}
		stream := logproto.Stream{Labels: `{job="synthetic"}`, Entries: entries}
		val, err := stream.Marshal()
		require.NoError(t, err)

		require.NoError(t, binary.Write(w, binary.LittleEndian, now.UnixNano()))
		require.NoError(t, binary.Write(w, binary.LittleEndian, uint32(len(val))))
		_, err = w.Write(val)
		require.NoError(t, err)

		valueBytes += int64(len(val))
		recordCount++
	}
	require.NoError(t, w.Flush())

	// Patch the record count at offset 0.
	_, err = f.Seek(0, 0)
	require.NoError(t, err)
	require.NoError(t, binary.Write(f, binary.LittleEndian, recordCount))
	require.NoError(t, f.Close())

	t.Logf("wrote %s: %d records, %d entries, %.1f MiB of values",
		out, recordCount, int(recordCount)*entriesPerRecord, float64(valueBytes)/(1<<20))
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
