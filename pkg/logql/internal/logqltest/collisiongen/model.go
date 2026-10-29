package collisiongen

import "sort"

// suffix is the text that a key gets when its name is taken.
const suffix = "_extracted"

// labelSet maps label names to values.
type labelSet map[string]string

// item is one key of a log line. A null item stands for a JSON null.
type item struct {
	key, val string
	null     bool
}

// pipe is the label state of one line while it moves through the stages of a pipeline. It follows
// the rules at the top of testdata/label_collisions.logqltest. It never looks at the output of the
// engine, so the engine can be tested against it.
type pipe struct {
	stream  labelSet        // base stream labels, without `app`
	meta    labelSet        // the structured metadata category
	parsed  labelSet        // the parsed category
	ext     map[string]bool // names recorded as extracted (the parser hints)
	dropped map[string]bool
	line    string
	alive   bool
}

func newPipe(stream, metadata labelSet) *pipe {
	p := &pipe{
		stream:  labelSet{},
		meta:    labelSet{},
		parsed:  labelSet{},
		ext:     map[string]bool{},
		dropped: map[string]bool{},
		alive:   true,
	}
	for k, v := range stream {
		p.stream[k] = v
	}
	// Metadata enters in the order of the key names. A key whose name a stream label has moves to
	// <name>_extracted. It is dropped when a label already has that name.
	for _, k := range sortedKeys(metadata) {
		name := k
		if _, ok := p.stream[k]; ok {
			name = k + suffix
			if has(p.stream, name) || has(p.meta, name) || has(p.parsed, name) {
				continue
			}
		}
		p.meta[name] = metadata[k]
	}
	return p
}

func has(m labelSet, k string) bool {
	_, ok := m[k]
	return ok
}

func sortedKeys(m labelSet) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (p *pipe) baseHas(k string) bool { return has(p.stream, k) && !p.dropped[k] }

// holdsName reports whether a stream label or a metadata value has the name. A parsed key never
// replaces such a label.
func (p *pipe) holdsName(k string) bool { return p.baseHas(k) || has(p.meta, k) }

func (p *pipe) setParsed(k, v string) {
	delete(p.meta, k)
	p.parsed[k] = v
	p.ext[k] = true
}

func (p *pipe) drop(k string) {
	delete(p.meta, k)
	delete(p.parsed, k)
	p.dropped[k] = true
}

// exists reports whether the result has a label with the name.
func (p *pipe) exists(k string) bool {
	if has(p.parsed, k) || has(p.meta, k) {
		return true
	}
	return has(p.stream, k) && !p.dropped[k] && p.stream[k] != ""
}

// final returns the three categories of the result.
func (p *pipe) final() (stream, meta, parsed labelSet) {
	meta, stream, parsed = labelSet{}, labelSet{}, labelSet{}
	for k, v := range p.meta {
		if !has(p.parsed, k) {
			meta[k] = v
		}
	}
	for k, v := range p.stream {
		if !p.dropped[k] && !has(meta, k) && !has(p.parsed, k) {
			stream[k] = v
		}
	}
	for k, v := range p.parsed {
		parsed[k] = v
	}
	return stream, meta, parsed
}

// merged returns all labels of the result by name.
func (p *pipe) merged() labelSet {
	stream, meta, parsed := p.final()
	m := labelSet{}
	for _, c := range []labelSet{stream, meta, parsed} {
		for k, v := range c {
			m[k] = v
		}
	}
	return m
}

// rename returns the final name of a key and whether the key was renamed.
func rename(p *pipe, k string) (string, bool) {
	if p.baseHas(k) || has(p.meta, k) {
		return k + suffix, true
	}
	return k, false
}

// plainApply is the first-writer rule of json, logfmt, regexp and pattern. The keys are in line
// order. It returns the names that it wrote.
func plainApply(p *pipe, items []item) []string {
	renamedHere := map[string]bool{}
	var written []string
	for _, it := range items {
		k, renamed := rename(p, it.key)
		if renamed && p.holdsName(k) {
			continue
		}
		if p.ext[k] && (renamed || !renamedHere[k]) {
			continue
		}
		p.setParsed(k, it.val)
		written = append(written, k)
		if renamed {
			renamedHere[k] = true
		} else {
			delete(renamedHere, k)
		}
	}
	return written
}

// plainApplyUpfront is plainApply for unpack: it decides the rename of every key before the stage
// writes anything. The generator checks that both give the same labels for every shape.
func plainApplyUpfront(p *pipe, items []item) {
	type decided struct {
		key     string
		renamed bool
		val     string
	}
	var all []decided
	for _, it := range items {
		k, renamed := rename(p, it.key)
		all = append(all, decided{k, renamed, it.val})
	}
	renamedHere := map[string]bool{}
	for _, d := range all {
		if d.renamed && p.holdsName(d.key) {
			continue
		}
		if p.ext[d.key] && (d.renamed || !renamedHere[d.key]) {
			continue
		}
		p.setParsed(d.key, d.val)
		if d.renamed {
			renamedHere[d.key] = true
		} else {
			delete(renamedHere, d.key)
		}
	}
}

func contains(ids []string, k string) bool {
	for _, id := range ids {
		if id == k {
			return true
		}
	}
	return false
}

// jsonExprApply is `json a, b`. The first occurrence of a key counts, then the first-writer rule
// applies. A JSON null is an empty value. A name that no key wrote gets an empty label, unless a
// label has the name.
func jsonExprApply(p *pipe, ids []string, items []item) {
	seen := map[string]bool{}
	var pairs []item
	for _, it := range items {
		if contains(ids, it.key) && !seen[it.key] {
			seen[it.key] = true
			pairs = append(pairs, item{key: it.key, val: it.val})
		}
	}
	written := plainApply(p, pairs)
	if len(written) < len(ids) {
		for _, id := range ids {
			if !p.exists(id) {
				p.setParsed(id, "")
			}
		}
	}
}

// logfmtExprApply is `logfmt a, b`. The first-writer rule applies. A name that no key wrote gets an
// empty label, unless a label has the name or an earlier stage extracted it.
func logfmtExprApply(p *pipe, ids []string, items []item) {
	var asked []item
	for _, it := range items {
		if contains(ids, it.key) {
			asked = append(asked, it)
		}
	}
	written := map[string]bool{}
	for _, k := range plainApply(p, asked) {
		written[k] = true
	}
	for _, id := range ids {
		if written[id] || p.ext[id] {
			continue
		}
		if !p.baseHas(id) && !has(p.meta, id) {
			p.setParsed(id, "")
		}
	}
}
