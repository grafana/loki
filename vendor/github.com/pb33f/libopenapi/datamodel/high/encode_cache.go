// Copyright 2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package high

import (
	"encoding/binary"
	"sync"

	"github.com/pb33f/go-yaml"
)

// The node builder encodes raw values (enum lists, required lists, examples and extension values) with
// (*yaml.Node).Encode, which serializes the value to YAML text and parses it back. The same small values
// repeat throughout a specification, and again every time a schema is rendered inline. Encode's result is
// a pure function of the value's content (yaml ignores node positions when encoding), so a value with the
// same content always encodes to the same tree: the tree is kept, and each render gets its own copy.

const (
	// encodeCacheMaxNodes bounds the values that are fingerprinted; larger ones are encoded directly.
	encodeCacheMaxNodes = 64

	// encodeCacheMaxKeyBytes bounds a fingerprint, so a large scalar is never held as a key.
	encodeCacheMaxKeyBytes = 2048

	// encodeCacheLimit bounds the entries kept; the cache is emptied when it fills.
	encodeCacheLimit = 4096
)

var encodeCache = struct {
	sync.RWMutex
	entries map[string]*encodedValue
}{entries: make(map[string]*encodedValue)}

// ClearEncodeCache empties the cache of encoded values kept by the node builder. It holds no references
// to model nodes, only its own copies of small encoded values, and is bounded in size.
func ClearEncodeCache() {
	encodeCache.Lock()
	clear(encodeCache.entries)
	encodeCache.Unlock()
}

// encodedValue is an encoded tree, with the node and Content slot counts of everything below its root.
type encodedValue struct {
	root     yaml.Node
	nodes    int
	contents int
}

// encodeValue stores in rawNode what rawNode.Encode(encodeSafeValue(value)) stores, reusing the result
// of an earlier encoding of a value with the same content.
func encodeValue(rawNode *yaml.Node, value any) error {
	var scratch [256]byte
	key, cacheable := appendEncodeFingerprint(scratch[:0], value)
	if !cacheable || len(key) > encodeCacheMaxKeyBytes {
		return rawNode.Encode(encodeSafeValue(value))
	}
	encodeCache.RLock()
	cached := encodeCache.entries[string(key)]
	encodeCache.RUnlock()
	if cached != nil {
		cached.copyInto(rawNode)
		return nil
	}
	if err := rawNode.Encode(encodeSafeValue(value)); err != nil {
		return err
	}
	entry := &encodedValue{}
	entry.root = *rawNode
	entry.copyChildren(&entry.root, rawNode, nil, nil)
	encodeCache.Lock()
	if len(encodeCache.entries) >= encodeCacheLimit {
		clear(encodeCache.entries)
	}
	encodeCache.entries[string(key)] = entry
	encodeCache.Unlock()
	return nil
}

// copyInto stores a copy of the encoded tree in dst, allocating its nodes and Content slices in one go.
func (e *encodedValue) copyInto(dst *yaml.Node) {
	nodes := make([]yaml.Node, e.nodes)
	contents := make([]*yaml.Node, e.contents)
	*dst = e.root
	e.copyChildren(dst, &e.root, &nodes, &contents)
}

// copyChildren deep copies src's children into dst, which already holds src's fields. With no slabs it
// allocates each node and counts the tree into e; otherwise it carves the copies from the slabs.
func (e *encodedValue) copyChildren(dst, src *yaml.Node, nodes *[]yaml.Node, contents *[]*yaml.Node) {
	if src.Content == nil {
		return
	}
	n := len(src.Content)
	if contents == nil {
		dst.Content = make([]*yaml.Node, n)
		e.contents += n
	} else {
		dst.Content = (*contents)[:n:n]
		*contents = (*contents)[n:]
	}
	for i, child := range src.Content {
		var c *yaml.Node
		if nodes == nil {
			c = new(yaml.Node)
			e.nodes++
		} else {
			c = &(*nodes)[0]
			*nodes = (*nodes)[1:]
		}
		*c = *child
		dst.Content[i] = c
		e.copyChildren(c, child, nodes, contents)
	}
}

// appendEncodeFingerprint appends an unambiguous encoding of everything Encode reads from value, and
// reports whether the value can be cached: a node, a slice of nodes or a slice of strings, of bounded
// size, with no anchors or aliases (whose encoding depends on nodes outside the value).
func appendEncodeFingerprint(b []byte, value any) ([]byte, bool) {
	budget := encodeCacheMaxNodes
	switch v := value.(type) {
	case *yaml.Node:
		return appendNodeFingerprint(append(b, 'n'), v, &budget)
	case []*yaml.Node:
		b = binary.AppendUvarint(append(b, 'l'), uint64(len(v)))
		for _, n := range v {
			var ok bool
			if b, ok = appendNodeFingerprint(b, n, &budget); !ok {
				return b, false
			}
		}
		return b, true
	case []string:
		if len(v) > encodeCacheMaxNodes {
			return b, false
		}
		b = binary.AppendUvarint(append(b, 's'), uint64(len(v)))
		for _, s := range v {
			b = appendFingerprintString(b, s)
		}
		return b, true
	}
	return b, false
}

func appendNodeFingerprint(b []byte, n *yaml.Node, budget *int) ([]byte, bool) {
	*budget--
	if n == nil || *budget < 0 || n.Anchor != "" || n.Alias != nil || n.Stream != nil ||
		(n.Kind != yaml.ScalarNode && n.Kind != yaml.SequenceNode && n.Kind != yaml.MappingNode) {
		return b, false
	}
	b = binary.AppendUvarint(b, uint64(n.Kind))
	b = binary.AppendUvarint(b, uint64(n.Style))
	b = appendFingerprintString(b, n.Tag)
	b = appendFingerprintString(b, n.Value)
	b = appendFingerprintString(b, n.HeadComment)
	b = appendFingerprintString(b, n.LineComment)
	b = appendFingerprintString(b, n.FootComment)
	if n.Content == nil {
		return append(b, 0), true
	}
	b = binary.AppendUvarint(append(b, 1), uint64(len(n.Content)))
	for _, c := range n.Content {
		var ok bool
		if b, ok = appendNodeFingerprint(b, c, budget); !ok {
			return b, false
		}
	}
	return b, true
}

func appendFingerprintString(b []byte, s string) []byte {
	return append(binary.AppendUvarint(b, uint64(len(s))), s...)
}
