package builder

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/s2"
)

// Run files are the builder's on-disk spill unit. Each holds a sorted, deduped
// block of (ngram, docID) pairs — the analogue of the old architecture's LIDP
// partials, but carrying raw sorted pairs instead of per-term bitmasks. They
// are written during ingest and k-way merged into the final per-(date,shard)
// .lidx files at flush. s2 is used (not zstd) because run files are transient
// scratch: s2 compresses the 12-byte records fast enough to stay off the
// critical path while still cutting scratch disk I/O.
//
// Layout (little-endian):
//
//	[4] magic "FRUN"
//	[1] version
//	[1] reserved
//	[2] shardCount S
//	[S*16] directory: per shard (int64 fileOffset, uint64 recordCount)
//	[per-shard payloads]: for each shard with recordCount>0, an INDEPENDENT s2
//	                      stream of its records, starting at fileOffset.
//
// Because the pairs are shard-contiguous at spill and each shard is its own
// self-contained s2 stream, the merge can seek straight to a shard's offset and
// decode only that shard — the basis for the single-pass shard-by-shard merge
// (and for merging shards in parallel, should that ever be needed).
const (
	runMagic   = "FRUN"
	runVersion = 2
	// runRecordSize is the on-disk size of one (ngram[8], docID[4]) pair.
	runRecordSize   = 12
	runHeaderSize   = 8 // magic(4) + version(1) + reserved(1) + shardCount(2)
	runDirEntrySize = 16
	runBufferSize   = 1 << 20
)

// writeRun writes shard-contiguous keys/docs (counts[s] records for shard s,
// laid out in shard order) as a run file with per-shard seekable s2 streams,
// reusing the caller's s2 writer across shards and spills. Returns the file
// size.
func writeRun(path string, keys [][8]byte, docs []uint32, counts []int32, swp **s2.Writer) (_ int64, err error) {
	fh, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	// The flush retry loop retries a failed spill forever; without this
	// cleanup every failed attempt would leak one fd plus one partial .frun
	// until fd exhaustion kills the builder. Runs are appended to runPaths
	// only after writeRun succeeds, so removing the partial file is safe.
	defer func() {
		if err != nil {
			fh.Close()
			os.Remove(path)
		}
	}()
	bw := bufio.NewWriterSize(fh, runBufferSize)

	shardCount := len(counts)
	var hdr [runHeaderSize]byte
	copy(hdr[:4], runMagic)
	hdr[4] = runVersion
	binary.LittleEndian.PutUint16(hdr[6:], uint16(shardCount))
	if _, err := bw.Write(hdr[:]); err != nil {
		return 0, err
	}
	// Directory placeholder (patched after payloads are written).
	dir := make([]byte, shardCount*runDirEntrySize)
	if _, err := bw.Write(dir); err != nil {
		return 0, err
	}

	if *swp == nil {
		*swp = s2.NewWriter(bw)
	}
	pos := 0
	var rec [runRecordSize]byte
	for s := range shardCount {
		c := int(counts[s])
		if c == 0 {
			// offset 0 + count 0 => absent.
			continue
		}
		// Flush so the file position is the true start of this shard's stream.
		if err := bw.Flush(); err != nil {
			return 0, err
		}
		off, err := fh.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		binary.LittleEndian.PutUint64(dir[s*runDirEntrySize:], uint64(off))
		binary.LittleEndian.PutUint64(dir[s*runDirEntrySize+8:], uint64(c))

		(*swp).Reset(bw)
		sw := *swp
		for i := pos; i < pos+c; i++ {
			copy(rec[:8], keys[i][:])
			binary.LittleEndian.PutUint32(rec[8:], docs[i])
			if _, err := sw.Write(rec[:]); err != nil {
				return 0, err
			}
		}
		if err := sw.Close(); err != nil {
			return 0, err
		}
		pos += c
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	// Patch the directory in place.
	if _, err := fh.Seek(int64(runHeaderSize), io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := fh.Write(dir); err != nil {
		return 0, err
	}
	if err = fh.Close(); err != nil {
		return 0, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// runReader streams (ngram, docID) records for a single shard from a run file.
type runReader struct {
	f    *os.File
	sr   *s2.Reader
	left uint64
}

// openRunShardReader opens the run at path positioned to read exactly the given
// shard's records. A shard absent from this run yields a reader that is
// immediately exhausted (left == 0).
func openRunShardReader(path string, shard int) (*runReader, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var hdr [runHeaderSize]byte
	if _, err := io.ReadFull(fh, hdr[:]); err != nil {
		fh.Close()
		return nil, err
	}
	if string(hdr[:4]) != runMagic {
		fh.Close()
		return nil, fmt.Errorf("bad run magic in %s", path)
	}
	// Runs are transient scratch written and read by the same process, so any
	// other version means a stale or foreign file — reading it with this
	// layout would silently mis-parse the directory.
	if hdr[4] != runVersion {
		fh.Close()
		return nil, fmt.Errorf("unsupported run version %d in %s (want %d)", hdr[4], path, runVersion)
	}
	shardCount := int(binary.LittleEndian.Uint16(hdr[6:]))
	if shard >= shardCount {
		fh.Close()
		return &runReader{}, nil // absent → empty
	}
	if _, err := fh.Seek(int64(runHeaderSize+shard*runDirEntrySize), io.SeekStart); err != nil {
		fh.Close()
		return nil, err
	}
	var de [runDirEntrySize]byte
	if _, err := io.ReadFull(fh, de[:]); err != nil {
		fh.Close()
		return nil, err
	}
	off := binary.LittleEndian.Uint64(de[:8])
	count := binary.LittleEndian.Uint64(de[8:])
	if count == 0 {
		fh.Close()
		return &runReader{}, nil // present but empty
	}
	if _, err := fh.Seek(int64(off), io.SeekStart); err != nil {
		fh.Close()
		return nil, err
	}
	sr := s2.NewReader(bufio.NewReaderSize(fh, runBufferSize))
	return &runReader{f: fh, sr: sr, left: count}, nil
}

func (rr *runReader) next() (key [8]byte, doc uint32, ok bool, err error) {
	if rr.left == 0 {
		return key, 0, false, nil
	}
	var rec [runRecordSize]byte
	if _, err = io.ReadFull(rr.sr, rec[:]); err != nil {
		return key, 0, false, err
	}
	copy(key[:], rec[:8])
	doc = binary.LittleEndian.Uint32(rec[8:])
	rr.left--
	return key, doc, true, nil
}

func (rr *runReader) close() error {
	if rr.f != nil {
		return rr.f.Close()
	}
	return nil
}
