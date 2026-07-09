// SPDX-License-Identifier: AGPL-3.0-only
//
// Streaming label enumeration methods: LabelValues, LabelNames,
// LabelValueFor, LabelNamesFor.

package index

import (
	"context"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// LabelValues mirrors Reader.LabelValues. matchers is not supported (parity
// with the mmap reader).
func (r *StreamReader) LabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, errors.Errorf("matchers parameter is not implemented: %+v", matchers)
	}
	ctx := context.Background()

	if r.version == FormatV1 {
		e, ok := r.streamPostingsV1[name]
		if !ok {
			return nil, nil
		}
		values := make([]string, 0, len(e))
		for k := range e {
			values = append(values, k)
		}
		return values, nil
	}

	e, ok := r.streamPostings[name]
	if !ok || len(e) == 0 {
		return nil, nil
	}
	values := make([]string, 0, len(e)*symbolFactor)

	d := r.factory.NewDecbufAtUnchecked(ctx, int(r.toc.PostingsTable))
	if err := d.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()

	d.ResetAt(e[0].off)
	lastVal := e[len(e)-1].value

	for d.Err() == nil {
		if k := d.Uvarint(); k != 2 {
			return nil, errors.Errorf("unexpected number of keys for postings offset table %d", k)
		}
		d.SkipUvarintBytes() // Label name.
		v := d.UvarintStr()  // Label value (copy).
		d.Uvarint64()        // Postings offset.
		values = append(values, v)
		if v == lastVal {
			break
		}
	}
	if err := d.Err(); err != nil {
		return nil, errors.Wrap(err, "get postings offset entry")
	}
	return values, nil
}

// LabelNames mirrors Reader.LabelNames. matchers is not supported.
func (r *StreamReader) LabelNames(matchers ...*labels.Matcher) ([]string, error) {
	if len(matchers) > 0 {
		return nil, errors.Errorf("matchers parameter is not implemented: %+v", matchers)
	}

	labelNames := make([]string, 0, len(r.streamPostings))
	for name := range r.streamPostings {
		if name == allPostingsKey.Name {
			continue
		}
		labelNames = append(labelNames, name)
	}
	sort.Strings(labelNames)
	return labelNames, nil
}

// LabelValueFor mirrors Reader.LabelValueFor.
func (r *StreamReader) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	offset := id
	if r.version >= FormatV2 {
		offset = id * 16
	}
	content, err := r.readUvarintSection(context.Background(), int(offset))
	if err != nil {
		return "", errors.Wrap(err, "label values for")
	}

	dec := r.decoder()
	value, err := dec.LabelValueFor(content, label)
	if err != nil {
		return "", storage.ErrNotFound
	}
	if value == "" {
		return "", storage.ErrNotFound
	}
	return value, nil
}

// LabelNamesFor mirrors Reader.LabelNamesFor.
func (r *StreamReader) LabelNamesFor(ids ...storage.SeriesRef) ([]string, error) {
	offsetsMap := make(map[uint32]struct{})
	for _, id := range ids {
		offset := id
		if r.version >= FormatV2 {
			offset = id * 16
		}
		content, err := r.readUvarintSection(context.Background(), int(offset))
		if err != nil {
			return nil, errors.Wrap(err, "get buffer for series")
		}
		dec := r.decoder()
		offsets, err := dec.LabelNamesOffsetsFor(content)
		if err != nil {
			return nil, errors.Wrap(err, "get label name offsets")
		}
		for _, off := range offsets {
			offsetsMap[off] = struct{}{}
		}
	}

	names := make([]string, 0, len(offsetsMap))
	for off := range offsetsMap {
		name, err := r.lookupSymbol(off)
		if err != nil {
			return nil, errors.Wrap(err, "lookup symbol in LabelNamesFor")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
