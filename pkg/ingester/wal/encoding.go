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
	// WALRecordEntriesV4 is the type for the WAL record for samples whose structured metadata
	// is split between the entry's own and a per stream pool of shared sets the entries
	// reference, mirroring the push wire format so that a replay reconstructs exactly what was
	// pushed. See Record.EntriesVersion for when it is written.
	WALRecordEntriesV4
)

// The current type of Entries that this distribution writes.
// Loki can read in a backwards compatible manner, but will write the newest variant.
// TODO: Change to WALRecordEntriesV3?
const CurrentEntriesRec = WALRecordEntriesV3

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
	if len(r.Series) > 0 {
		r.Series = r.Series[:0]
	}

	// Drop the references the retained RefEntries hold, so that a pooled record sitting in the
	// pool does not pin the entries and the shared sets of the push it came from.
	for i := range r.RefEntries {
		r.RefEntries[i].Entries = nil
		r.RefEntries[i].SharedStructuredMetadataSets = nil
	}
	r.RefEntries = r.RefEntries[:0]
	r.entryIndexMap = make(map[uint64]int)
}

// AddEntries adds entries for a stream to the record. sets is the stream's pool of shared
// structured metadata sets, which the entries reference by index, and is nil for a push that
// shares nothing.
//
// Entries for a fingerprint that shares nothing are merged into a single RefEntries, as they
// always were. A push that carries a pool always gets a RefEntries of its own instead: its
// entries' references are indexes into that specific pool, so entries from two pushes cannot
// share one RefEntries without remapping them. Only pool-less RefEntries are therefore tracked
// in entryIndexMap, which keeps a later pool-less push from merging into a pooled RefEntries.
// Two pooled pushes for one fingerprint in a single record is a rare case, and costs only a
// repeated fingerprint and counter in the encoding.
func (r *Record) AddEntries(fp uint64, counter int64, sets []logproto.SharedStructuredMetadataSet, entries ...logproto.Entry) {
	if len(sets) == 0 {
		if idx, ok := r.entryIndexMap[fp]; ok {
			r.RefEntries[idx].Entries = append(r.RefEntries[idx].Entries, entries...)
			r.RefEntries[idx].Counter = counter
			return
		}
		r.entryIndexMap[fp] = len(r.RefEntries)
	}

	r.RefEntries = append(r.RefEntries, RefEntries{
		Counter:                      counter,
		Ref:                          chunks.HeadSeriesRef(fp),
		Entries:                      entries,
		SharedStructuredMetadataSets: sets,
	})
}

type RefEntries struct {
	Counter int64
	Ref     chunks.HeadSeriesRef
	Entries []logproto.Entry

	// SharedStructuredMetadataSets is the pool of shared structured metadata sets of the stream
	// these entries belong to, which they reference by 1-based index exactly as they do on the
	// push wire format. Only carried by WALRecordEntriesV4 records; empty otherwise, in which
	// case the entries hold all of their structured metadata themselves.
	SharedStructuredMetadataSets []logproto.SharedStructuredMetadataSet
}

// EntriesVersion is the record type Entries of this record must be encoded as.
//
// A pool of shared structured metadata sets can only be expressed by WALRecordEntriesV4, so a
// record carrying one is written as V4 and a record carrying none stays on CurrentEntriesRec.
// That keeps the blast radius of the new version to the tenants that actually enable deferred
// structured metadata expansion: every other tenant's segments stay byte for byte what they
// were, and remain readable by an ingester that predates V4.
//
// The version is a property of the whole record, so a record that mixes pooled and pool-less
// streams is written as V4 throughout; the pool-less streams in it simply encode an empty pool.
func (r *Record) EntriesVersion() RecordType {
	for i := range r.RefEntries {
		if len(r.RefEntries[i].SharedStructuredMetadataSets) > 0 {
			return WALRecordEntriesV4
		}
	}
	return CurrentEntriesRec
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

		if version >= WALRecordEntriesV4 {
			// The stream's pool of shared structured metadata sets, written once before the
			// entries that reference it by index.
			buf.PutUvarint(len(ref.SharedStructuredMetadataSets))
			for _, set := range ref.SharedStructuredMetadataSets {
				putLabels(&buf, set.Attrs)
			}
		}

		buf.PutUvarint(len(ref.Entries)) // write number of entries

		for _, s := range ref.Entries {
			buf.PutVarint64(s.Timestamp.UnixNano() - first)
			buf.PutUvarint(len(s.Line))
			buf.PutString(s.Line)

			if version >= WALRecordEntriesV3 {
				// structured metadata. From V4 on this is the entry's own structured metadata
				// only, the shared part being carried by the pool above.
				putLabels(&buf, s.StructuredMetadata)
			}

			if version >= WALRecordEntriesV4 {
				buf.PutUvarint(int(s.SharedResourceRef))
				buf.PutUvarint(int(s.SharedScopeRef))
			}
		}
	}
	return buf.Get()
}

// putLabels encodes a label list the way structured metadata has been encoded since
// WALRecordEntriesV3: a count followed by length prefixed name and value pairs.
func putLabels(buf *encoding.Encbuf, lbls []logproto.LabelAdapter) {
	buf.PutUvarint(len(lbls))
	for _, l := range lbls {
		buf.PutUvarint(len(l.Name))
		buf.PutString(l.Name)
		buf.PutUvarint(len(l.Value))
		buf.PutString(l.Value)
	}
}

// decodeLabels is the putLabels counterpart. It returns nil rather than an empty slice for an
// empty list, so that a decoded entry compares equal to one that never had any.
func decodeLabels(dec *encoding.Decbuf) []logproto.LabelAdapter {
	n := dec.Uvarint()
	if n == 0 {
		return nil
	}

	lbls := make([]logproto.LabelAdapter, 0, n)
	for i := 0; dec.Err() == nil && i < n; i++ {
		nameLength := dec.Uvarint()
		name := dec.Bytes(nameLength)
		valueLength := dec.Uvarint()
		value := dec.Bytes(valueLength)
		lbls = append(lbls, logproto.LabelAdapter{
			Name:  string(name),
			Value: string(value),
		})
	}

	return lbls
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

		if version >= WALRecordEntriesV4 {
			if nSets := dec.Uvarint(); nSets > 0 {
				refEntries.SharedStructuredMetadataSets = make([]logproto.SharedStructuredMetadataSet, 0, nSets)
				for i := 0; dec.Err() == nil && i < nSets; i++ {
					refEntries.SharedStructuredMetadataSets = append(refEntries.SharedStructuredMetadataSets,
						logproto.SharedStructuredMetadataSet{Attrs: decodeLabels(&dec)})
				}
			}
		}

		nEntries := dec.Uvarint()
		refEntries.Entries = make([]logproto.Entry, 0, nEntries)
		rem := nEntries
		for ; dec.Err() == nil && rem > 0; rem-- {
			timeOffset := dec.Varint64()
			lineLength := dec.Uvarint()
			line := dec.Bytes(lineLength)

			var structuredMetadata []logproto.LabelAdapter
			if version >= WALRecordEntriesV3 {
				structuredMetadata = decodeLabels(&dec)
			}

			var resourceRef, scopeRef uint32
			if version >= WALRecordEntriesV4 {
				resourceRef = uint32(dec.Uvarint())
				scopeRef = uint32(dec.Uvarint())
			}

			refEntries.Entries = append(refEntries.Entries, logproto.Entry{
				Timestamp:          time.Unix(0, baseTime+timeOffset),
				Line:               string(line),
				StructuredMetadata: structuredMetadata,
				SharedResourceRef:  resourceRef,
				SharedScopeRef:     scopeRef,
			})
		}

		if dec.Err() != nil {
			return fmt.Errorf("entry decode error after %d RefEntries: %w", nEntries-rem, dec.Err())
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
		userID = decbuf.UvarintStr()
		rSeries, err = dec.Series(decbuf.B, walRec.Series)
	case WALRecordEntriesV1, WALRecordEntriesV2, WALRecordEntriesV3, WALRecordEntriesV4:
		userID = decbuf.UvarintStr()
		err = DecodeEntries(decbuf.B, t, walRec)
	default:
		// An ingester that predates a record version reaches this, and the caller counts it as
		// a WAL corruption and skips the record. See Record.EntriesVersion for why a version is
		// only written when it is actually needed.
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
