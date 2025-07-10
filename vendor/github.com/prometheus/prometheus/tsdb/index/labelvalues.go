package index

import (
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/prometheus/prometheus/util/annotations"
)

// LabelValuesFor returns LabelValues for the given label name in the series referred to by postings.
func (r *Reader) LabelValuesFor(postings Postings, name string) storage.LabelValues {
	return r.labelValuesFor(postings, name, true)
}

// LabelValuesExcluding returns LabelValues for the given label name in all other series than those referred to by postings.
// This is useful for obtaining label values for other postings than the ones you wish to exclude.
func (r *Reader) LabelValuesExcluding(postings Postings, name string) storage.LabelValues {
	return r.labelValuesFor(postings, name, false)
}

func (r *Reader) labelValuesFor(postings Postings, name string, includeMatches bool) storage.LabelValues {
	if r.version == FormatV1 {
		return r.labelValuesForV1(postings, name, includeMatches)
	}

	e := r.postings[name]
	if len(e) == 0 {
		return storage.EmptyLabelValues()
	}

	d := encoding.NewDecbufAt(r.b, int(r.toc.PostingsTable), nil)
	// Skip to start
	d.Skip(e[0].off)
	lastVal := e[len(e)-1].value

	return &intersectLabelValues{
		d:              &d,
		b:              r.b,
		dec:            r.dec,
		lastVal:        lastVal,
		postings:       NewPostingsCloner(postings),
		includeMatches: includeMatches,
	}
}

func (r *Reader) labelValuesForV1(postings Postings, name string, includeMatches bool) storage.LabelValues {
	e := r.postingsV1[name]
	if len(e) == 0 {
		return storage.EmptyLabelValues()
	}
	vals := make([]string, 0, len(e))
	for v := range e {
		vals = append(vals, v)
	}
	slices.Sort(vals)
	return &intersectLabelValuesV1{
		postingOffsets: e,
		values:         storage.NewListLabelValues(vals),
		postings:       NewPostingsCloner(postings),
		b:              r.b,
		dec:            r.dec,
		includeMatches: includeMatches,
	}
}

type intersectLabelValuesV1 struct {
	postingOffsets map[string]uint64
	values         *storage.ListLabelValues
	postings       *PostingsCloner
	b              ByteSlice
	dec            *Decoder
	cur            string
	err            error
	includeMatches bool
}

func (it *intersectLabelValuesV1) Next() bool {
	if it.err != nil {
		return false
	}

	// Look for a value with intersecting postings
	for it.values.Next() {
		val := it.values.At()

		postingsOff := it.postingOffsets[val]
		// Read from the postings table.
		d := encoding.NewDecbufAt(it.b, int(postingsOff), castagnoliTable)
		_, curPostings, err := it.dec.DecodePostings(d)
		if err != nil {
			it.err = fmt.Errorf("decode postings: %w", err)
			return false
		}

		isMatch := false
		if it.includeMatches {
			isMatch = intersect(it.postings.Clone(), curPostings)
		} else {
			// We only want to include this value if curPostings is not fully contained
			// by the postings iterator (which is to be excluded).
			isMatch = !contains(it.postings.Clone(), curPostings)
		}
		if isMatch {
			it.cur = val
			return true
		}
	}

	return false
}

func (it *intersectLabelValuesV1) At() string {
	return it.cur
}

func (it *intersectLabelValuesV1) Err() error {
	return it.err
}

func (it *intersectLabelValuesV1) Warnings() annotations.Annotations {
	return nil
}

func (it *intersectLabelValuesV1) Close() error {
	return nil
}

type intersectLabelValues struct {
	d              *encoding.Decbuf
	b              ByteSlice
	dec            *Decoder
	postings       *PostingsCloner
	lastVal        string
	skip           int
	cur            string
	exhausted      bool
	err            error
	includeMatches bool
}

func (it *intersectLabelValues) Next() bool {
	if it.exhausted || it.err != nil {
		return false
	}

	for !it.exhausted && it.d.Err() == nil {
		if it.skip == 0 {
			// These are always the same number of bytes,
			// and it's faster to skip than to parse.
			it.skip = it.d.Len()
			// Key count
			it.d.Uvarint()
			// Label name
			it.d.UvarintBytes()
			it.skip -= it.d.Len()
		} else {
			it.d.Skip(it.skip)
		}

		// Label value
		val := it.d.UvarintBytes()
		postingsOff := int(it.d.Uvarint64())
		// Read from the postings table
		postingsDec := encoding.NewDecbufAt(it.b, postingsOff, castagnoliTable)
		_, curPostings, err := it.dec.DecodePostings(postingsDec)
		if err != nil {
			it.err = fmt.Errorf("decode postings: %w", err)
			return false
		}
		it.exhausted = string(val) == it.lastVal
		isMatch := false
		if it.includeMatches {
			isMatch = intersect(it.postings.Clone(), curPostings)
		} else {
			// We only want to include this value if curPostings is not fully contained
			// by the postings iterator (which is to be excluded).
			isMatch = !contains(it.postings.Clone(), curPostings)
		}
		if isMatch {
			// Make sure to allocate a new string
			it.cur = string(val)
			return true
		}
	}
	if it.d.Err() != nil {
		it.err = fmt.Errorf("get postings offset entry: %w", it.d.Err())
	}

	return false
}

func (it *intersectLabelValues) At() string {
	return it.cur
}

func (it *intersectLabelValues) Err() error {
	return it.err
}

func (it *intersectLabelValues) Warnings() annotations.Annotations {
	return nil
}

func (it *intersectLabelValues) Close() error {
	return nil
}

// LabelValuesFor returns LabelValues for the given label name in the series referred to by postings.
func (p *MemPostings) LabelValuesFor(postings Postings, name string) storage.LabelValues {
	return p.labelValuesFor(postings, name, true)
}

// LabelValuesExcluding returns LabelValues for the given label name in all other series than those referred to by postings.
// This is useful for obtaining label values for other postings than the ones you wish to exclude.
func (p *MemPostings) LabelValuesExcluding(postings Postings, name string) storage.LabelValues {
	return p.labelValuesFor(postings, name, false)
}

func (p *MemPostings) labelValuesFor(postings Postings, name string, includeMatches bool) storage.LabelValues {
	p.mtx.RLock()

	e := p.m[name]
	if len(e) == 0 {
		p.mtx.RUnlock()
		return storage.EmptyLabelValues()
	}

	// With thread safety in mind and due to random key ordering in map, we have to construct the array in memory
	vals := make([]string, 0, len(e))
	candidates := make([]Postings, 0, len(e))
	// Allocate a slice for all needed ListPostings, so no need to allocate them one by one.
	lps := make([]ListPostings, 0, len(e))
	for val, srs := range e {
		vals = append(vals, val)
		lps = append(lps, ListPostings{list: srs})
		candidates = append(candidates, &lps[len(lps)-1])
	}

	var (
		indexes []int
		err     error
	)
	if includeMatches {
		indexes, err = FindIntersectingPostings(postings, candidates)
	} else {
		// We wish to exclude the postings when finding label values, meaning that
		// we want to filter down candidates to only those not fully contained
		// by postings (a fully contained postings iterator should be excluded).
		indexes, err = findNonContainedPostings(postings, candidates)
	}
	p.mtx.RUnlock()
	if err != nil {
		return storage.ErrLabelValues(err)
	}

	// Filter the values
	if len(vals) != len(indexes) {
		slices.Sort(indexes)
		for i, index := range indexes {
			vals[i] = vals[index]
		}
		vals = vals[:len(indexes)]
	}

	slices.Sort(vals)
	return storage.NewListLabelValues(vals)
}

// intersect returns whether p1 and p2 have at least one series in common.
func intersect(p1, p2 Postings) bool {
	if !p1.Next() || !p2.Next() {
		return false
	}

	cur := p1.At()
	if p2.At() > cur {
		cur = p2.At()
	}

	for p1.Seek(cur) {
		if p1.At() > cur {
			cur = p1.At()
		}
		if !p2.Seek(cur) {
			break
		}
		if p2.At() > cur {
			cur = p2.At()
			continue
		}

		return true
	}

	return false
}

// contains returns whether subp is contained in p.
func contains(p, subp Postings) bool {
	for subp.Next() {
		if needle := subp.At(); !p.Seek(needle) || p.At() != needle {
			return false
		}
	}

	// Couldn't find any value in subp which is not in p.
	return true
}
