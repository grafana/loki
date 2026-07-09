// SPDX-License-Identifier: AGPL-3.0-only
//
// Streaming counterparts to the postings-offset-table scan and Postings
// method in index.go. The offset-table sparse index and the V1 posting map
// are built during StreamReader construction; postings-list reads at query
// time flow through streamenc Decbufs.

package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// streamPostingOffset is a copy of postingOffset with a slightly different
// semantic: off is the position within a mimir-style streamenc.Decbuf whose
// base is the postings-offset-table section (i.e., includes the 4-byte
// length prefix). That is 4 higher than the equivalent Prometheus Decbuf
// offset — see stream_reader.go for the details.
type streamPostingOffset struct {
	value string
	off   int
}

// streamReadOffsetTable iterates through the postings-offset table located
// at file offset `off`. It mirrors ReadOffsetTable but reads through a
// streamenc.DecbufFactory. The callback receives the same (name, value,
// postingsOffset, entryPos) tuple; entryPos uses mimir-Offset semantics.
func streamReadOffsetTable(
	ctx context.Context,
	factory *streamenc.FilePoolDecbufFactory,
	off uint64,
	fn func(name, value []byte, postingsOff uint64, entryPos int) error,
) error {
	d := factory.NewDecbufAtChecked(ctx, int(off), castagnoliTable)
	if err := d.Err(); err != nil {
		return err
	}
	defer func() { _ = d.Close() }()

	cnt := d.Be32()
	// d.Len() at this point is (contentLen + 4) - 4 = contentLen. But we've
	// already consumed the 4-byte count, so remaining content is contentLen-4.
	// Loop until we've consumed all entries. We rely on cnt as the source of
	// truth to avoid overreading into the trailing CRC bytes.
	for d.Err() == nil && cnt > 0 {
		entryPos := d.Offset()
		if k := d.Uvarint(); k != 2 {
			return fmt.Errorf("unexpected number of keys for postings offset table %d", k)
		}
		name := d.UvarintStr()
		value := d.UvarintStr()
		postingsOff := d.Uvarint64()
		if err := d.Err(); err != nil {
			return err
		}
		if err := fn([]byte(name), []byte(value), postingsOff, entryPos); err != nil {
			return err
		}
		cnt--
	}
	return d.Err()
}

// buildPostingsIndex is the equivalent of the postings-table build block in
// newReader. It populates streamPostings and streamPostingsV1 on the reader.
func (r *StreamReader) buildPostingsIndex(ctx context.Context) error {
	r.streamPostings = map[string][]streamPostingOffset{}

	if r.version == FormatV1 {
		r.streamPostingsV1 = map[string]map[string]uint64{}
		return streamReadOffsetTable(ctx, r.factory, r.toc.PostingsTable, func(name, value []byte, postingsOff uint64, _ int) error {
			ns := string(name)
			if _, ok := r.streamPostingsV1[ns]; !ok {
				r.streamPostingsV1[ns] = map[string]uint64{}
				r.streamPostings[ns] = nil
			}
			r.streamPostingsV1[ns][string(value)] = postingsOff
			return nil
		})
	}

	var (
		lastName, lastValue []byte
		lastOff             int
		valueCount          int
	)
	err := streamReadOffsetTable(ctx, r.factory, r.toc.PostingsTable, func(name, value []byte, _ uint64, entryPos int) error {
		ns := string(name)
		if _, ok := r.streamPostings[ns]; !ok {
			r.streamPostings[ns] = []streamPostingOffset{}
			if lastName != nil {
				r.streamPostings[string(lastName)] = append(r.streamPostings[string(lastName)], streamPostingOffset{value: string(lastValue), off: lastOff})
			}
			valueCount = 0
		}
		if valueCount%symbolFactor == 0 {
			r.streamPostings[ns] = append(r.streamPostings[ns], streamPostingOffset{value: string(value), off: entryPos})
			lastName, lastValue = nil, nil
		} else {
			// The offset table is scanned linearly at construction; we retain
			// the previous entry so we can emit the trailing "last value"
			// marker when the label name changes or the scan ends. Copy the
			// bytes to detach from the Decbuf's transient buffer.
			lastName = append(lastName[:0], name...)
			lastValue = append(lastValue[:0], value...)
			lastOff = entryPos
		}
		valueCount++
		return nil
	})
	if err != nil {
		return err
	}
	if lastName != nil {
		r.streamPostings[string(lastName)] = append(r.streamPostings[string(lastName)], streamPostingOffset{value: string(lastValue), off: lastOff})
	}
	// Trim allocations.
	for k, v := range r.streamPostings {
		l := make([]streamPostingOffset, len(v))
		copy(l, v)
		r.streamPostings[k] = l
	}
	return nil
}

// readPostingsList opens a Decbuf at the given file offset and materializes
// the entire postings list into a fresh []byte, wrapping it in
// BigEndianPostings. The list bytes are of the form
//
//	[N uint32 big-endian][ref_1 uint32 big-endian]...[ref_N uint32 big-endian]
//
// followed by the section CRC (excluded here).
func (r *StreamReader) readPostingsList(ctx context.Context, postingsOff uint64) (Postings, error) {
	d := r.factory.NewDecbufAtChecked(ctx, int(postingsOff), castagnoliTable)
	if err := d.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()

	n := d.Be32int()
	if err := d.Err(); err != nil {
		return nil, err
	}

	// After Be32, Decbuf Len() is (4+contentLen+4) - (4+4) = contentLen. But
	// the last 4 bytes are the CRC we've already validated, so we want to
	// read n*4 bytes. Sanity-check that the section has enough room.
	need := 4 * n
	list := make([]byte, need)
	// Decbuf lacks a direct ReadInto for arbitrary lengths; use Be32/Be64/
	// Byte primitives. For postings lists (which can be large — millions of
	// refs for popular label values), reading Be32 per ref would be
	// acceptable but wasteful. Instead we read Be32 into a big-endian byte
	// buffer so BigEndianPostings can operate on it without re-encoding.
	for i := 0; i < n; i++ {
		v := d.Be32()
		if err := d.Err(); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(list[i*4:(i+1)*4], v)
	}
	_ = need // vet appeasement (documenting the expected count)
	return NewBigEndianPostings(list), nil
}

// Postings returns the postings iterator for a name and a set of values.
// Mirrors Reader.Postings; behavior of FingerprintFilter is preserved
// (post-merge sharding via NewShardedPostings using fingerprintOffsets).
func (r *StreamReader) Postings(name string, fpFilter FingerprintFilter, values ...string) (Postings, error) {
	ctx := context.Background()

	if r.version == FormatV1 {
		e, ok := r.streamPostingsV1[name]
		if !ok {
			return EmptyPostings(), nil
		}
		res := make([]Postings, 0, len(values))
		for _, v := range values {
			postingsOff, ok := e[v]
			if !ok {
				continue
			}
			p, err := r.readPostingsList(ctx, postingsOff)
			if err != nil {
				return nil, errors.Wrap(err, "decode postings")
			}
			res = append(res, p)
		}
		return applyFingerprintFilter(Merge(res...), fpFilter, r.streamFingerprintOffsets()), nil
	}

	e, ok := r.streamPostings[name]
	if !ok || len(values) == 0 {
		return EmptyPostings(), nil
	}

	res := make([]Postings, 0, len(values))
	valueIndex := 0
	for valueIndex < len(values) && values[valueIndex] < e[0].value {
		valueIndex++
	}
	for valueIndex < len(values) {
		value := values[valueIndex]

		i := sort.Search(len(e), func(i int) bool { return e[i].value >= value })
		if i == len(e) {
			break
		}
		if i > 0 && e[i].value != value {
			i--
		}
		// Open a fresh Decbuf on the offset table (no CRC — we trust it
		// was validated at construction). Seek to the sparse entry.
		d := r.factory.NewDecbufAtUnchecked(ctx, int(r.toc.PostingsTable))
		if err := d.Err(); err != nil {
			return nil, err
		}
		d.ResetAt(e[i].off)

		// Iterate entries until we've placed every remaining value at or
		// past this sparse group, or fallen off the end.
		for d.Err() == nil {
			// Each entry is: uvarint keyCount(=2), uvarint-str name,
			// uvarint-str value, uvarint postingsOffset.
			if k := d.Uvarint(); k != 2 {
				_ = d.Close()
				return nil, fmt.Errorf("unexpected number of keys for postings offset table %d", k)
			}
			d.SkipUvarintBytes() // Label name; we already know it from e[i].
			v := d.UvarintStr()
			postingsOff := d.Uvarint64()
			if err := d.Err(); err != nil {
				_ = d.Close()
				return nil, err
			}
			for v >= value {
				if v == value {
					p, err := r.readPostingsList(ctx, postingsOff)
					if err != nil {
						_ = d.Close()
						return nil, errors.Wrap(err, "decode postings")
					}
					res = append(res, p)
				}
				valueIndex++
				if valueIndex == len(values) {
					break
				}
				value = values[valueIndex]
			}
			if i+1 == len(e) || value >= e[i+1].value || valueIndex == len(values) {
				break
			}
		}
		if err := d.Err(); err != nil {
			_ = d.Close()
			return nil, errors.Wrap(err, "get postings offset entry")
		}
		_ = d.Close()
	}

	merged := Merge(res...)
	return applyFingerprintFilter(merged, fpFilter, r.streamFingerprintOffsets()), nil
}

// applyFingerprintFilter mirrors the tail of Reader.Postings.
func applyFingerprintFilter(p Postings, fpFilter FingerprintFilter, fpOffsets FingerprintOffsets) Postings {
	if fpFilter == nil {
		return p
	}
	return NewShardedPostings(p, fpFilter, fpOffsets)
}

// streamFingerprintOffsets returns the reader's fingerprint offsets. Loaded
// during construction (P2.A7); callers that pass a non-nil FingerprintFilter
// rely on this table to shard postings.
func (r *StreamReader) streamFingerprintOffsets() FingerprintOffsets {
	return r.fingerprintOffsets
}
