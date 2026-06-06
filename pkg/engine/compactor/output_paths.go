package compactor

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"time"
)

// indexMergePath computes deterministic object-storage keys for IndexMerge
// task outputs. The zero value is ready for use; the same value can drive any
// number of Build calls back-to-back without re-allocating its internal
// scratch buffers (after they grow to their high-water mark).
//
// indexMergePath is not safe for concurrent use; each goroutine that needs
// to compute paths must hold its own.
type indexMergePath struct {
	buf    []byte                // canonical-form scratch (input to sha256.Sum256)
	sorted []string              // reusable sorted section-id scratch
	hexBuf [sha256.Size * 2]byte // hex.Encode destination (64 bytes)
}

// Build returns the deterministic object-storage key for one IndexMerge
// task's output. Two coordinators that read the same source ToC and plan
// the same task produce identical paths and dedupe via the bucket's view:
// the second worker's existence-check finds the file and short-circuits.
//
// Path layout: indexes/tenants/<tenant>/<sha2>/<sha-rest>
//
// SHA-256 input (newline-separated, canonical form):
//
//	v=<planVersion>
//	t=<tenant>
//	w=<windowStartUnixNanos>
//	b=<binIndex>
//	s=<sortedSectionID>   (one line per ID, lexicographically sorted)
//	...
//
// The recommended sectionID form is "<objectPath>#<sectionIndex>" — any
// tuple unique within the input set works. Build sorts a copy so callers
// may pass sectionIDs in whatever order the planner emits. The tenant
// string is mixed into the hash so outputs for different tenants are
// guaranteed to differ even with otherwise-identical inputs.
func (p *indexMergePath) Build(
	tenant string,
	windowStart time.Time,
	planVersion uint,
	binIndex int,
	sectionIDs []string,
) string {
	p.sorted = append(p.sorted[:0], sectionIDs...)
	sort.Strings(p.sorted)

	p.buf = p.buf[:0]
	p.buf = appendKVUint(p.buf, "v=", uint64(planVersion))
	p.buf = appendKVStr(p.buf, "t=", tenant)
	p.buf = appendKVInt(p.buf, "w=", windowStart.UTC().UnixNano())
	p.buf = appendKVInt(p.buf, "b=", int64(binIndex))
	for _, id := range p.sorted {
		p.buf = appendKVStr(p.buf, "s=", id)
	}

	sum := sha256.Sum256(p.buf)
	hex.Encode(p.hexBuf[:], sum[:])

	// allocate a string for response so buffers are available for the next call
	encoded := string(p.hexBuf[:])
	return "indexes/tenants/" + tenant + "/" + encoded[:2] + "/" + encoded[2:]
}

func appendKVUint(dst []byte, key string, v uint64) []byte {
	dst = append(dst, key...)
	dst = strconv.AppendUint(dst, v, 10)
	return append(dst, '\n')
}

func appendKVInt(dst []byte, key string, v int64) []byte {
	dst = append(dst, key...)
	dst = strconv.AppendInt(dst, v, 10)
	return append(dst, '\n')
}

func appendKVStr(dst []byte, key, val string) []byte {
	dst = append(dst, key...)
	dst = append(dst, val...)
	return append(dst, '\n')
}
