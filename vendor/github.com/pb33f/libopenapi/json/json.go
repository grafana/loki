package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/orderedmap"
)

// YAMLNodeToJSON converts yaml/json stored in a yaml.Node to json ordered matching the original yaml/json
func YAMLNodeToJSON(node *yaml.Node, indentation string) ([]byte, error) {
	w := writer{}
	if err := w.writeNode(node); err != nil {
		return nil, err
	}
	if w.marshalFailed {
		// a value encoding/json cannot represent (NaN or an infinity) is reported by marshaling the whole
		// converted value, which is where and how that error has always surfaced.
		return convertAndMarshal(node, indentation)
	}
	var out bytes.Buffer
	_ = json.Indent(&out, w.buf, "", indentation) // w.buf is valid JSON by construction
	return out.Bytes(), nil
}

// convertAndMarshal converts the node tree to ordered values and marshals them with encoding/json.
func convertAndMarshal(node *yaml.Node, indentation string) ([]byte, error) {
	c := converter{aliasesInFlight: make(map[*yaml.Node]struct{})}
	v, err := c.handleYAMLNode(node)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", indentation)
}

// dupScanLimit is the mapping size up to which repeated keys are found by scanning the keys written so
// far; larger mappings track their keys in a set.
const dupScanLimit = 16

// writer writes the compact JSON that encoding/json produces for the ordered values the converter
// builds, directly from the node tree. Values are written as json.Marshal writes them, so indenting
// the result gives exactly what json.MarshalIndent gives for the converted value. Anything outside the
// common shapes (scalars that need yaml's full decoding, complex keys, mappings with repeated keys) is
// converted by the converter and marshaled by encoding/json, as before.
type writer struct {
	buf             []byte
	keys            []string // keys of the mappings being written, for spotting repeated keys
	aliasesInFlight map[*yaml.Node]struct{}
	marshalFailed   bool // a value could not be marshaled
}

func (w *writer) converter() converter {
	if w.aliasesInFlight == nil {
		w.aliasesInFlight = make(map[*yaml.Node]struct{})
	}
	return converter{aliasesInFlight: w.aliasesInFlight}
}

func (w *writer) writeNode(node *yaml.Node) error {
	if node == nil {
		return errors.New("nil yaml node")
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return errors.New("empty yaml document")
		}
		return w.writeNode(node.Content[0])
	case yaml.SequenceNode:
		w.buf = append(w.buf, '[')
		for i, n := range node.Content {
			if i > 0 {
				w.buf = append(w.buf, ',')
			}
			if err := w.writeNode(n); err != nil {
				return err
			}
		}
		w.buf = append(w.buf, ']')
		return nil
	case yaml.MappingNode:
		return w.writeMapping(node)
	case yaml.ScalarNode:
		if b, ok := appendScalar(w.buf, node); ok {
			w.buf = b
			return nil
		}
		v, err := handleScalarNode(node)
		if err != nil {
			return err
		}
		w.writeMarshaled(v)
		return nil
	case yaml.AliasNode:
		c := w.converter()
		if _, inFlight := c.aliasesInFlight[node.Alias]; inFlight {
			return fmt.Errorf("recursive alias '%s' at line %d, column %d", node.Value, node.Line, node.Column)
		}
		c.aliasesInFlight[node.Alias] = struct{}{}
		defer delete(c.aliasesInFlight, node.Alias)
		return w.writeNode(node.Alias)
	default:
		return fmt.Errorf("unknown node kind: %v", node.Kind)
	}
}

// writeMarshaled writes a value converted from the nodes. A value that cannot be marshaled is noted
// rather than returned: conversion errors anywhere in the tree take precedence over it.
func (w *writer) writeMarshaled(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.marshalFailed = true
		data = []byte("null")
	}
	w.buf = append(w.buf, data...)
}

func (w *writer) writeMapping(node *yaml.Node) error {
	start, keysBase := len(w.buf), len(w.keys)
	var seen map[string]struct{}
	if len(node.Content)/2 > dupScanLimit {
		seen = make(map[string]struct{}, len(node.Content)/2)
	}
	w.buf = append(w.buf, '{')
	for i := 1; i < len(node.Content); i += 2 {
		key, err := w.keyString(node.Content[i-1])
		if err != nil {
			return err
		}
		if w.repeatedKey(key, keysBase, seen) {
			// a repeated key keeps its first position and takes the last value: the ordered map does that.
			w.buf, w.keys = w.buf[:start], w.keys[:keysBase]
			v, err := w.converter().handleMappingNode(node)
			if err != nil {
				return err
			}
			w.writeMarshaled(v)
			return nil
		}
		if i > 1 {
			w.buf = append(w.buf, ',')
		}
		w.buf = append(appendJSONString(w.buf, key), ':')
		if err := w.writeNode(node.Content[i]); err != nil {
			return err
		}
	}
	w.keys = w.keys[:keysBase]
	w.buf = append(w.buf, '}')
	return nil
}

// repeatedKey reports whether key was already written in the current mapping, and records it.
func (w *writer) repeatedKey(key string, keysBase int, seen map[string]struct{}) bool {
	if seen != nil {
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
		return false
	}
	for _, k := range w.keys[keysBase:] {
		if k == key {
			return true
		}
	}
	w.keys = append(w.keys, key)
	return false
}

// keyString returns the ordered map key the converter makes of a key node: a string key as it is, and
// any other key as its JSON encoding.
func (w *writer) keyString(keyNode *yaml.Node) (string, error) {
	if keyNode != nil && keyNode.Kind == yaml.ScalarNode {
		if keyNode.Tag == "!!str" {
			return keyNode.Value, nil
		}
		var scratch [32]byte
		if b, ok := appendScalar(scratch[:0], keyNode); ok {
			return string(b), nil
		}
	}
	kv, err := w.converter().handleYAMLNode(keyNode)
	if err != nil {
		return "", err
	}
	if key, isString := kv.(string); isString {
		return key, nil
	}
	keyData, err := json.Marshal(kv)
	if err != nil {
		return "", err
	}
	return string(keyData), nil
}

// appendScalar appends the JSON encoding of the value yaml decodes a scalar node into, for the tags and
// values whose decoding is known exactly, and reports false for anything else.
func appendScalar(b []byte, n *yaml.Node) ([]byte, bool) {
	switch n.Tag {
	case "!!str":
		return appendJSONString(b, n.Value), true
	case "!!int":
		// a canonical decimal in range decodes to an integer that encodes back to the same digits.
		if isJSONInteger(n.Value) && n.Value != "-0" {
			if _, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
				return append(b, n.Value...), true
			}
		}
	case "!!bool":
		switch n.Value {
		case "true", "True", "TRUE":
			return append(b, "true"...), true
		case "false", "False", "FALSE":
			return append(b, "false"...), true
		}
	case "!!null":
		switch n.Value {
		case "", "~", "null", "Null", "NULL":
			return append(b, "null"...), true
		}
	case "!!float":
		// yaml resolves -0 to negative zero, and an integer beyond int64 to an unsigned value it will
		// not accept as a float; everything else in JSON number form decodes like ParseFloat.
		if n.Value == "-0" || n.Value == "-0.0" || !isJSONNumber(n.Value) {
			return b, false
		}
		if isJSONInteger(n.Value) {
			if _, err := strconv.ParseInt(n.Value, 10, 64); err != nil {
				return b, false
			}
		}
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return appendJSONFloat(b, f), true
		}
	}
	return b, false
}

// isJSONInteger reports whether s is a JSON integer: an optional minus and digits without a leading zero.
func isJSONInteger(s string) bool {
	return len(s) > 0 && jsonNumberEnd(s, false) == len(s)
}

// isJSONNumber reports whether s is a JSON number.
func isJSONNumber(s string) bool {
	return len(s) > 0 && jsonNumberEnd(s, true) == len(s)
}

// jsonNumberEnd returns the length of the JSON number (or integer, without fraction or exponent) at the
// start of s, or -1 when s does not start with one.
func jsonNumberEnd(s string, fractional bool) int {
	i := 0
	if s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	default:
		return -1
	}
	if !fractional {
		return i
	}
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return -1
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return -1
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	return i
}

// appendJSONFloat appends a finite float64 formatted as encoding/json formats one.
func appendJSONFloat(b []byte, f float64) []byte {
	format := byte('f')
	if abs := math.Abs(f); abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		// clean up e-09 to e-9
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

const hexDigits = "0123456789abcdef"

// appendJSONString appends s as a JSON string, escaped as encoding/json's Marshal escapes it: quotes,
// backslashes and control characters, the HTML characters <, > and &, U+2028 and U+2029, with invalid
// UTF-8 replaced by U+FFFD.
func appendJSONString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		if c := s[i]; c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' && c != '<' && c != '>' && c != '&' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			switch c {
			case '"', '\\':
				b = append(b, '\\', c)
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b = append(b, s[start:i]...)
			b = append(b, `\ufffd`...)
		case r == '\u2028' || r == '\u2029':
			b = append(b, s[start:i]...)
			b = append(b, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
		default:
			i += size
			continue
		}
		i += size
		start = i
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// converter builds ordered values from a node tree for encoding/json to marshal. It tracks alias targets
// currently being expanded, so a self-referencing anchor (e.g. `a: &x [1, *x]`) is reported as an error
// instead of recursing forever.
type converter struct {
	aliasesInFlight map[*yaml.Node]struct{}
}

func (c converter) handleYAMLNode(node *yaml.Node) (any, error) {
	if node == nil {
		return nil, errors.New("nil yaml node")
	}
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, errors.New("empty yaml document")
		}
		return c.handleYAMLNode(node.Content[0])
	case yaml.SequenceNode:
		return c.handleSequenceNode(node)
	case yaml.MappingNode:
		return c.handleMappingNode(node)
	case yaml.ScalarNode:
		return handleScalarNode(node)
	case yaml.AliasNode:
		return c.handleAliasNode(node)
	default:
		return nil, fmt.Errorf("unknown node kind: %v", node.Kind)
	}
}

func (c converter) handleAliasNode(node *yaml.Node) (any, error) {
	if _, inFlight := c.aliasesInFlight[node.Alias]; inFlight {
		return nil, fmt.Errorf("recursive alias '%s' at line %d, column %d", node.Value, node.Line, node.Column)
	}
	c.aliasesInFlight[node.Alias] = struct{}{}
	defer delete(c.aliasesInFlight, node.Alias)
	return c.handleYAMLNode(node.Alias)
}

func (c converter) handleMappingNode(node *yaml.Node) (any, error) {
	v := orderedmap.New[string, any]()
	for i, n := range node.Content {
		if i%2 == 0 {
			continue
		}
		keyNode := node.Content[i-1]
		kv, err := c.handleYAMLNode(keyNode)
		if err != nil {
			return nil, err
		}

		key, isString := kv.(string)
		if !isString {
			keyData, err := json.Marshal(kv)
			if err != nil {
				return nil, err
			}
			key = string(keyData)
		}

		vv, err := c.handleYAMLNode(n)
		if err != nil {
			return nil, err
		}

		v.Set(key, vv)
	}

	return v, nil
}

func (c converter) handleSequenceNode(node *yaml.Node) (any, error) {
	v := make([]any, len(node.Content))
	for i, n := range node.Content {
		vv, err := c.handleYAMLNode(n)
		if err != nil {
			return nil, err
		}

		v[i] = vv
	}

	return v, nil
}

func handleScalarNode(node *yaml.Node) (any, error) {
	var v any

	if err := node.Decode(&v); err != nil {
		return nil, err
	}

	return v, nil
}
