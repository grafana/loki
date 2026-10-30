package utils

import (
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/pb33f/go-yaml"
)

type enumValueIndex struct {
	values map[string]int
	nodes  map[*yaml.Node]int
	active map[*yaml.Node]bool
}

// Bound arbitrary-precision parsing and the size of retained numeric keys.
const (
	maxEnumNumberLength   = 1024
	maxEnumNumberExponent = 400
)

// CheckEnumForDuplicates returns each occurrence after the first of an equal
// JSON value, preserving input order and node identity. Numbers compare by value,
// arrays by ordered elements, and objects by members irrespective of key order.
// YAML aliases are followed; cyclic or non-JSON values are ignored.
// Numeric literals longer than 1024 bytes or with exponents outside [-400, 400]
// are also ignored to bound the cost of arbitrary-precision comparison.
func CheckEnumForDuplicates(seq []*yaml.Node) []*yaml.Node {
	var duplicates []*yaml.Node
	seen := make(map[int]bool)
	index := enumValueIndex{
		values: make(map[string]int),
		nodes:  make(map[*yaml.Node]int),
		active: make(map[*yaml.Node]bool),
	}
	for _, node := range seq {
		id := index.valueID(node)
		if id == 0 {
			continue
		}
		if seen[id] {
			duplicates = append(duplicates, node)
		}
		seen[id] = true
	}
	return duplicates
}

// valueID interns each value using its children's IDs, keeping repeated aliases
// compact. Zero denotes an unsupported value or one exceeding numeric limits.
func (index *enumValueIndex) valueID(node *yaml.Node) int {
	if node == nil || index.active[node] {
		return 0
	}
	if id, ok := index.nodes[node]; ok {
		return id
	}
	index.active[node] = true
	defer delete(index.active, node)
	var id int
	if node.Kind == yaml.AliasNode {
		id = index.valueID(node.Alias)
	} else if key, ok := index.valueKey(node); ok {
		id = index.values[key]
		if id == 0 {
			id = len(index.values) + 1
			index.values[key] = id
		}
	}
	index.nodes[node] = id
	return id
}

func (index *enumValueIndex) valueKey(node *yaml.Node) (string, bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str":
			return "s" + strconv.Quote(node.Value), true
		case "!!null":
			return "null", true
		case "!!bool":
			var value bool
			if err := node.Decode(&value); err != nil {
				return "", false
			}
			return strconv.FormatBool(value), true
		case "!!int":
			if len(node.Value) > maxEnumNumberLength {
				return "", false
			}
			// YAML accepts base-prefixed integers and legacy octal (012 == 10).
			value, ok := new(big.Int).SetString(node.Value, 0)
			if !ok {
				return "", false
			}
			return "n" + value.String(), true
		case "!!float":
			if !enumFloatWithinLimits(node.Value) {
				return "", false
			}
			value, ok := new(big.Rat).SetString(node.Value)
			if !ok {
				return "", false
			}
			return "n" + value.RatString(), true
		}
	case yaml.SequenceNode:
		if node.ShortTag() != "!!seq" {
			return "", false
		}
		parts := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			id := index.valueID(child)
			if id == 0 {
				return "", false
			}
			parts = append(parts, strconv.Itoa(id))
		}
		return "[" + strings.Join(parts, ",") + "]", true
	case yaml.MappingNode:
		if node.ShortTag() != "!!map" || len(node.Content)%2 != 0 {
			return "", false
		}
		parts := make([]string, 0, len(node.Content)/2)
		keys := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			name := node.Content[i]
			if name == nil || name.Kind != yaml.ScalarNode || name.ShortTag() != "!!str" || keys[name.Value] {
				return "", false
			}
			keys[name.Value] = true
			id := index.valueID(node.Content[i+1])
			if id == 0 {
				return "", false
			}
			parts = append(parts, strconv.Quote(name.Value)+":"+strconv.Itoa(id))
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ",") + "}", true
	}
	return "", false
}

func enumFloatWithinLimits(value string) bool {
	if len(value) > maxEnumNumberLength {
		return false
	}
	// big.Rat also accepts binary exponents. In hexadecimal mantissas,
	// e and E are digits, so only p and P can introduce an exponent.
	markers := "eEpP"
	if strings.ContainsAny(value, "xX") {
		markers = "pP"
	}
	if i := strings.LastIndexAny(value, markers); i >= 0 {
		exponent, err := strconv.Atoi(strings.ReplaceAll(value[i+1:], "_", ""))
		return err == nil && exponent >= -maxEnumNumberExponent && exponent <= maxEnumNumberExponent
	}
	return true
}
