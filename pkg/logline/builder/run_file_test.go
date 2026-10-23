package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/s2"
	"github.com/stretchr/testify/require"
)

// readShard drains one shard's records from a run file.
func readShard(t *testing.T, path string, shard int) []pair {
	t.Helper()
	rr, err := openRunShardReader(path, shard)
	require.NoError(t, err)
	defer rr.close()

	var out []pair
	for {
		k, d, ok, err := rr.next()
		require.NoError(t, err)
		if !ok {
			return out
		}
		out = append(out, pair{key: k, doc: d})
	}
}

func TestRunFile_RoundTrip_MultiShard(t *testing.T) {
	// Shard-contiguous layout: shard 0 has 3 records, shard 1 is empty
	// (count 0 in the directory), shard 2 has 2 records.
	keys := [][8]byte{
		{'a', 'a', 'a', 'a', 'a', 'a'},
		{'a', 'a', 'a', 'a', 'a', 'b'},
		{'a', 'a', 'a', 'a', 'a', 'c'},
		{'z', 'z', 'z', 'z', 'z', 'y'},
		{'z', 'z', 'z', 'z', 'z', 'z'},
	}
	docs := []uint32{1, 2, 3, 100, 200}
	counts := []int32{3, 0, 2}

	path := filepath.Join(t.TempDir(), "run_0.frun")
	var sw *s2.Writer
	size, err := writeRun(path, keys, docs, counts, &sw)
	require.NoError(t, err)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, fi.Size(), size, "writeRun must report the true file size")

	require.Equal(t, []pair{
		{key: keys[0], doc: 1},
		{key: keys[1], doc: 2},
		{key: keys[2], doc: 3},
	}, readShard(t, path, 0))

	require.Empty(t, readShard(t, path, 1), "empty shard must yield no records")

	require.Equal(t, []pair{
		{key: keys[3], doc: 100},
		{key: keys[4], doc: 200},
	}, readShard(t, path, 2))
}

func TestRunFile_AbsentShard(t *testing.T) {
	keys := [][8]byte{{'a', 'b', 'c', 'd', 'e', 'f'}}
	docs := []uint32{7}

	path := filepath.Join(t.TempDir(), "run_0.frun")
	var sw *s2.Writer
	_, err := writeRun(path, keys, docs, []int32{1}, &sw)
	require.NoError(t, err)

	// Shard index beyond the file's shard count → immediately exhausted reader,
	// not an error (older single-shard runs stay readable at higher counts).
	require.Empty(t, readShard(t, path, 5))
}

func TestRunFile_ManyRecordsSingleShard(t *testing.T) {
	// Enough records to span multiple s2 blocks, verifying the stream framing.
	pairs := randomPairs(t, 50_000, false)
	keys, docs := splitSoA(pairs)

	path := filepath.Join(t.TempDir(), "run_0.frun")
	var sw *s2.Writer
	_, err := writeRun(path, keys, docs, []int32{int32(len(pairs))}, &sw)
	require.NoError(t, err)

	require.Equal(t, pairs, readShard(t, path, 0))
}

func TestRunFile_BadMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bogus.frun")
	require.NoError(t, os.WriteFile(path, []byte("NOTARUNFILE-----"), 0o644))

	_, err := openRunShardReader(path, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad run magic")
}

func TestRunFile_BadVersion(t *testing.T) {
	// A structurally valid run whose version byte was written by a different
	// (hypothetical) run layout must be rejected before the directory is
	// parsed, not mis-read.
	keys := [][8]byte{{'a', 'b', 'c', 'd', 'e', 'f'}}
	docs := []uint32{7}

	path := filepath.Join(t.TempDir(), "run_0.frun")
	var sw *s2.Writer
	_, err := writeRun(path, keys, docs, []int32{1}, &sw)
	require.NoError(t, err)

	fh, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt([]byte{runVersion + 1}, 4) // version byte follows the 4-byte magic
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	_, err = openRunShardReader(path, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported run version")
}
