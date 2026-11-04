package wal

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

// RecordType represents the type of the WAL/Checkpoint record.
type RecordType byte

const (
	_ = iota // ignore first value so the zero value doesn't look like a record type.
	// WALRecordSeries is the type for the WAL record for series.
	WALRecordSeries RecordType = iota
	// WALRecordEntriesV1 is the type for the WAL record for samples.
	WALRecordEntriesV1
	// CheckpointRecord is the type for the Checkpoint record based on protos.
	CheckpointRecord
	// WALRecordEntriesV2 is the type for the WAL record for samples with an
	// additional counter value for use in replaying without the ordering constraint.
	WALRecordEntriesV2
	// WALRecordEntriesV3 is the type for the WAL record for samples with structured metadata.
	WALRecordEntriesV3
	// WALRecordEntriesV4 adds shared metadata for contiguous entry ranges.
	WALRecordEntriesV4
)

// The current type of Entries that this distribution writes.
// Loki can read in a backwards compatible manner, but will write the newest variant.
const CurrentEntriesRec = WALRecordEntriesV4

// Record is a struct combining the series and samples record.
type Record struct {
	UserID string
	Series []record.RefSeries

	// entryIndexMap coordinates the RefEntries index associated with a particular fingerprint.
	// This is helpful for constant time lookups during ingestion and is ignored when restoring
	// from the WAL.
	entryIndexMap map[uint64]int
	RefEntries    []RefEntries
}

func (r *Record) IsEmpty() bool {
	return len(r.Series) == 0 && len(r.RefEntries) == 0
}

func (r *Record) Reset() {
	r.UserID = ""
	clear(r.Series)
	clear(r.RefEntries)
	if len(r.Series) > 0 {
		r.Series = r.Series[:0]
	}

	r.RefEntries = r.RefEntries[:0]
	r.entryIndexMap = make(map[uint64]int)
}

// AddEntries appends entries without shared metadata.
func (r *Record) AddEntries(fp uint64, counter int64, entries ...logproto.Entry) {
	r.AddEntriesWithMetadata(fp, counter, entries, nil)
}

// AddEntriesWithMetadata appends accepted entries and their shared metadata.
// Groups must be ordered, non-overlapping ranges within entries. Entries and
// attribute slices must remain immutable until the record has been logged.
func (r *Record) AddEntriesWithMetadata(fp uint64, counter int64, entries []logproto.Entry, groups []MetadataGroup) {
	if len(entries) == 0 {
		return
	}
	idx, ok := r.entryIndexMap[fp]
	if !ok {
		idx = len(r.RefEntries)
		r.entryIndexMap[fp] = idx
		r.RefEntries = append(r.RefEntries, RefEntries{Ref: chunks.HeadSeriesRef(fp)})
	}
	ref := &r.RefEntries[idx]
	offset := len(ref.Entries)
	if offset == 0 {
		ref.Entries = entries
	} else {
		ref.Entries = append(ref.Entries, entries...)
	}
	for _, group := range groups {
		group.Start += offset
		group.End += offset
		ref.MetadataGroups = append(ref.MetadataGroups, group)
	}
	ref.Counter = counter
}

// MetadataGroup holds the fallback attributes for Entries[Start:End]. Entry
// metadata takes precedence, including empty values. Gaps have no shared metadata.
type MetadataGroup struct {
	Start, End int
	Attrs      []logproto.LabelAdapter
}

// RefEntries contains accepted entries for one stream and its replay counter.
type RefEntries struct {
	Counter        int64
	Ref            chunks.HeadSeriesRef
	Entries        []logproto.Entry
	MetadataGroups []MetadataGroup
}

func (r *Record) EncodeSeries(b []byte) []byte {
	buf := encoding.EncWith(b)
	buf.PutByte(byte(WALRecordSeries))
	buf.PutUvarintStr(r.UserID)

	var enc record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series(r.Series, encoded[len(encoded):])...)

	return encoded
}

func (r *Record) EncodeEntries(version RecordType, b []byte) []byte {
	buf := encoding.EncWith(b)
	buf.PutByte(byte(version))
	buf.PutUvarintStr(r.UserID)

	// Placeholder for the first timestamp of any sample encountered.
	// All others in this record will store their timestamps as diffs relative to this
	// as a space optimization.
	var first int64

outer:
	for _, ref := range r.RefEntries {
		for _, entry := range ref.Entries {
			first = entry.Timestamp.UnixNano()
			buf.PutBE64int64(first)
			break outer
		}
	}

	for _, ref := range r.RefEntries {
		// ignore refs with 0 entries
		if len(ref.Entries) < 1 {
			continue
		}
		buf.PutBE64(uint64(ref.Ref)) // write fingerprint

		if version >= WALRecordEntriesV2 {
			buf.PutBE64int64(ref.Counter) // write highest counter value
		}

		buf.PutUvarint(len(ref.Entries)) // write number of entries

		groupIndex := 0
		for i, entry := range ref.Entries {
			buf.PutVarint64(entry.Timestamp.UnixNano() - first)
			buf.PutUvarintStr(entry.Line)

			if version >= WALRecordEntriesV3 {
				attrs := entry.StructuredMetadata
				// Older formats cannot represent shared attributes.
				if version < WALRecordEntriesV4 {
					for groupIndex < len(ref.MetadataGroups) && i >= ref.MetadataGroups[groupIndex].End {
						groupIndex++
					}
					if groupIndex < len(ref.MetadataGroups) && i >= ref.MetadataGroups[groupIndex].Start {
						attrs = logproto.AppendMetadata(nil, attrs, ref.MetadataGroups[groupIndex].Attrs)
					}
				}
				buf.PutUvarint(len(attrs))
				for _, attr := range attrs {
					buf.PutUvarintStr(attr.Name)
					buf.PutUvarintStr(attr.Value)
				}
			}
		}
		if version >= WALRecordEntriesV4 {
			buf.PutUvarint(len(ref.MetadataGroups))
			for _, group := range ref.MetadataGroups {
				buf.PutUvarint(group.Start)
				buf.PutUvarint(group.End)
				buf.B = appendMetadata(buf.B, group.Attrs)
			}
		}
	}
	return buf.Get()
}

func DecodeEntries(b []byte, version RecordType, rec *Record) error {
	if len(b) == 0 {
		return nil
	}

	dec := encoding.DecWith(b)
	baseTime := dec.Be64int64()

	for len(dec.B) > 0 && dec.Err() == nil {
		refEntries := RefEntries{
			Ref: chunks.HeadSeriesRef(dec.Be64()),
		}

		if version >= WALRecordEntriesV2 {
			refEntries.Counter = dec.Be64int64()
		}

		nEntries := decodeCount(&dec, 2)
		refEntries.Entries = make([]logproto.Entry, 0, nEntries)
		rem := nEntries
		for ; dec.Err() == nil && rem > 0; rem-- {
			timeOffset := dec.Varint64()
			lineLength := dec.Uvarint()
			if lineLength < 0 {
				return errInvalidSize
			}
			line := dec.Bytes(lineLength)

			var structuredMetadata []logproto.LabelAdapter
			if version >= WALRecordEntriesV3 {
				nMetadata := decodeCount(&dec, 2)
				if nMetadata > 0 {
					structuredMetadata = make([]logproto.LabelAdapter, 0, nMetadata)
					for i := 0; i < nMetadata && dec.Err() == nil; i++ {
						nameLength := dec.Uvarint()
						if nameLength < 0 {
							return errInvalidSize
						}
						name := dec.Bytes(nameLength)
						valueLength := dec.Uvarint()
						if valueLength < 0 {
							return errInvalidSize
						}
						value := dec.Bytes(valueLength)
						structuredMetadata = append(structuredMetadata, logproto.LabelAdapter{Name: string(name), Value: string(value)})
					}
				}
			}

			refEntries.Entries = append(refEntries.Entries, logproto.Entry{
				Timestamp:          time.Unix(0, baseTime+timeOffset),
				Line:               string(line),
				StructuredMetadata: structuredMetadata,
			})
		}

		if dec.Err() != nil {
			return fmt.Errorf("entry decode error after %d RefEntries: %w", nEntries-rem, dec.Err())
		}

		if version >= WALRecordEntriesV4 {
			nGroups := decodeCount(&dec, 3)
			lastEnd := 0
			for i := 0; i < nGroups && dec.Err() == nil; i++ {
				group := MetadataGroup{Start: dec.Uvarint(), End: dec.Uvarint()}
				if dec.Err() != nil {
					return fmt.Errorf("metadata group decode error: %w", dec.Err())
				}
				if group.Start < lastEnd || group.End <= group.Start || group.End > nEntries {
					return fmt.Errorf("invalid metadata range [%d:%d] after %d for %d entries", group.Start, group.End, lastEnd, nEntries)
				}
				group.Attrs = decodeMetadata(&dec)
				refEntries.MetadataGroups = append(refEntries.MetadataGroups, group)
				lastEnd = group.End
			}
			if dec.Err() != nil {
				return fmt.Errorf("metadata groups decode error: %w", dec.Err())
			}
		}

		rec.RefEntries = append(rec.RefEntries, refEntries)
	}

	if dec.Err() != nil {
		return fmt.Errorf("refEntry decode error: %w", dec.Err())
	}

	if len(dec.B) > 0 {
		return fmt.Errorf("unexpected %d bytes left in entry", len(dec.B))
	}
	return nil
}

func DecodeRecord(b []byte, walRec *Record) (err error) {
	var (
		userID  string
		dec     record.Decoder
		rSeries []record.RefSeries

		decbuf = encoding.DecWith(b)
		t      = RecordType(decbuf.Byte())
	)

	switch t {
	case WALRecordSeries:
		userID = decodeString(&decbuf)
		rSeries, err = dec.Series(decbuf.B, walRec.Series)
	case WALRecordEntriesV1, WALRecordEntriesV2, WALRecordEntriesV3, WALRecordEntriesV4:
		userID = decodeString(&decbuf)
		err = DecodeEntries(decbuf.B, t, walRec)
	default:
		return errors.New("unknown record type")
	}

	// We reach here only if its a record with type header.
	if decbuf.Err() != nil {
		return decbuf.Err()
	}

	if err != nil {
		return err
	}

	walRec.UserID = userID
	walRec.Series = rSeries
	return nil
}

// Entry metadata uses the same layout, with its loop inlined on the hot path.
func appendMetadata(b []byte, attrs []logproto.LabelAdapter) []byte {
	buf := encoding.EncWith(b)
	buf.PutUvarint(len(attrs))
	for _, attr := range attrs {
		buf.PutUvarintStr(attr.Name)
		buf.PutUvarintStr(attr.Value)
	}
	return buf.Get()
}

func decodeMetadata(dec *encoding.Decbuf) []logproto.LabelAdapter {
	n := decodeCount(dec, 2)
	if n == 0 {
		return nil
	}
	attrs := make([]logproto.LabelAdapter, 0, n)
	for i := 0; i < n && dec.Err() == nil; i++ {
		attrs = append(attrs, logproto.LabelAdapter{Name: decodeString(dec), Value: decodeString(dec)})
	}
	return attrs
}

// Bound allocations and lengths by the remaining input, including on corrupt WALs.
func decodeCount(dec *encoding.Decbuf, minSize int) int {
	n := dec.Uvarint()
	if n == 0 {
		return 0
	}
	if n < 0 || n > len(dec.B)/minSize {
		dec.E = errInvalidSize
		return 0
	}
	return n
}

var errInvalidSize = errors.New("invalid WAL count or length")

func decodeString(dec *encoding.Decbuf) string {
	n := dec.Uvarint64()
	if dec.Err() != nil {
		return ""
	}
	if n > uint64(len(dec.B)) {
		dec.E = errInvalidSize
		return ""
	}
	s := string(dec.B[:n])
	dec.B = dec.B[n:]
	return s
}
