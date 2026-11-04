package orderedmap

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/high/nodes"
	"github.com/pb33f/libopenapi/utils"
)

type marshaler interface {
	MarshalYAML() (interface{}, error)
}

type NodeBuilder interface {
	AddYAMLNode(parent *yaml.Node, entry *nodes.NodeEntry) *yaml.Node
}

type MapToYamlNoder interface {
	ToYamlNode(n NodeBuilder, l any) *yaml.Node
}

type hasValueNode interface {
	GetValueNode() *yaml.Node
}

type hasValueUntyped interface {
	GetValueUntyped() any
}

type findValueUntyped interface {
	FindValueUntyped(k string) any
}

// MarshalYAML implements yaml.Marshaler for libopenapi's ordered map wrapper.
func (o *Map[K, V]) MarshalYAML() (interface{}, error) {
	if o == nil {
		return nil, nil
	}

	node := yaml.Node{Kind: yaml.MappingNode}
	for pair := First(o); pair != nil; pair = pair.Next() {
		keyNode := &yaml.Node{}
		keyValue, err := encodeMarshalYAMLValue(pair.Key())
		if err != nil {
			return nil, err
		}
		if err = keyNode.Encode(keyValue); err != nil {
			return nil, err
		}

		valueNode := &yaml.Node{}
		value, err := encodeMarshalYAMLValue(pair.Value())
		if err != nil {
			return nil, err
		}
		if err = valueNode.Encode(value); err != nil {
			return nil, err
		}

		node.Content = append(node.Content, keyNode, valueNode)
	}

	return &node, nil
}

func encodeMarshalYAMLValue(value any) (any, error) {
	for {
		if value == nil {
			return nil, nil
		}
		if node, ok := value.(*yaml.Node); ok {
			return utils.CloneYAMLNode(node), nil
		}

		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Ptr && rv.IsNil() {
			return value, nil
		}

		m, ok := value.(marshaler)
		if !ok {
			return value, nil
		}

		marshaled, err := m.MarshalYAML()
		if err != nil {
			return nil, err
		}
		value = marshaled
	}
}

// ToYamlNode converts the ordered map to a yaml node ready for marshalling.
func (o *Map[K, V]) ToYamlNode(n NodeBuilder, l any) *yaml.Node {
	p := utils.CreateEmptyMapNode()
	if o != nil {
		p.Content = make([]*yaml.Node, 0, 2*o.Len())
	}

	var vn *yaml.Node

	i := 99999
	if l != nil {
		if hvn, ok := l.(hasValueNode); ok {
			vn = hvn.GetValueNode()
			if vn != nil && len(vn.Content) > 0 {
				i = vn.Content[0].Line
			}
		}
	}

	keyNodes := keyNodeIndex{mapNode: vn}
	var lowValues *untypedValueIndex
	var lowFinder findValueUntyped
	lowResolved := false

	for pair := First(o); pair != nil; pair = pair.Next() {
		var k any = pair.Key()
		if m, ok := k.(marshaler); ok { // TODO marshal inline?
			mk, _ := m.MarshalYAML()
			b, _ := yaml.Marshal(mk)
			k = strings.TrimSpace(string(b))
		}

		ks := k.(string)

		var keyStyle yaml.Style
		keyNode := keyNodes.find(ks)
		if keyNode != nil {
			keyStyle = keyNode.Style
		}

		// resolve the low-level map once, indexing it so each key is found without a scan.
		if !lowResolved {
			lowResolved = true
			if hvut, ok := l.(hasValueUntyped); ok {
				vut := hvut.GetValueUntyped()
				if indexer, ok := vut.(untypedValueIndexer); ok {
					lowValues = indexer.untypedValueIndex()
				} else if m, ok := vut.(findValueUntyped); ok {
					lowFinder = m
				}
			}
		}

		var lv any
		if lowValues != nil {
			lv = lowValues.find(ks)
		} else if lowFinder != nil {
			lv = lowFinder.FindValueUntyped(ks)
		}

		n.AddYAMLNode(p, &nodes.NodeEntry{
			Tag:      ks,
			Key:      ks,
			Line:     i,
			Value:    pair.Value(),
			KeyStyle: keyStyle,
			LowValue: lv,
		})
		i++
	}

	return p
}

// indexScanLimit is the entry count up to which an index answers lookups by scanning; above it, a hash
// map is built on first use.
const indexScanLimit = 16

// keyNodeIndex finds key nodes of a mapping node with the same first-match semantics as findKeyNode.
type keyNodeIndex struct {
	mapNode *yaml.Node
	byKey   map[string]*yaml.Node
}

func (x *keyNodeIndex) find(key string) *yaml.Node {
	if x.mapNode == nil || len(x.mapNode.Content) <= 2*indexScanLimit {
		return findKeyNode(key, x.mapNode)
	}
	if x.byKey == nil {
		x.byKey = make(map[string]*yaml.Node, len(x.mapNode.Content)/2)
		for i := 0; i < len(x.mapNode.Content); i += 2 {
			if _, seen := x.byKey[x.mapNode.Content[i].Value]; !seen {
				x.byKey[x.mapNode.Content[i].Value] = x.mapNode.Content[i]
			}
		}
	}
	return x.byKey[key]
}

// untypedValueIndexer is implemented by Map. It lets ToYamlNode resolve the low-level value of every key
// in one pass over the low-level map, rather than one FindValueUntyped scan per key.
type untypedValueIndexer interface {
	untypedValueIndex() *untypedValueIndex
}

// untypedValueIndex answers FindValueUntyped lookups with identical results: a pair matches a key when
// the string form of its untyped key value, or of the key itself, equals the key, and the oldest
// matching pair wins.
type untypedValueIndex struct {
	names  []string // match strings, oldest pair first
	values []any    // values[i] is the value of the pair names[i] came from
	byName map[string]int

	// braceForms is set when the keys are structs printed as "{...}". Those forms are not indexed, so a
	// key starting with a brace is answered by the exact scan instead.
	braceForms bool
	finder     findValueUntyped
}

func (o *Map[K, V]) untypedValueIndex() *untypedValueIndex {
	x := &untypedValueIndex{finder: o, braceForms: formatsAsStructLiteral(reflect.TypeFor[K]())}
	if o == nil {
		return x
	}
	for pair := o.Oldest(); pair != nil; pair = pair.Next() {
		var k any = pair.Key
		value := any(pair.Value)
		if hvut, ok := k.(hasValueUntyped); ok {
			x.names = append(x.names, formatUntyped(hvut.GetValueUntyped()))
			x.values = append(x.values, value)
		}
		if !x.braceForms {
			x.names = append(x.names, formatUntyped(k))
			x.values = append(x.values, value)
		}
	}
	return x
}

func (x *untypedValueIndex) find(key string) any {
	if x.braceForms && strings.HasPrefix(key, "{") {
		return x.finder.FindValueUntyped(key)
	}
	if len(x.names) <= indexScanLimit {
		for i, name := range x.names {
			if name == key {
				return x.values[i]
			}
		}
		return nil
	}
	if x.byName == nil {
		x.byName = make(map[string]int, len(x.names))
		for i, name := range x.names {
			if _, seen := x.byName[name]; !seen {
				x.byName[name] = i
			}
		}
	}
	if i, ok := x.byName[key]; ok {
		return x.values[i]
	}
	return nil
}

// formatUntyped returns fmt.Sprintf("%v", v), skipping the formatter for plain strings.
func formatUntyped(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// formatsAsStructLiteral reports whether %v prints values of t as "{...}": a struct with no method that
// would take over its formatting.
func formatsAsStructLiteral(t reflect.Type) bool {
	return t.Kind() == reflect.Struct &&
		!t.Implements(reflect.TypeFor[fmt.Formatter]()) &&
		!t.Implements(reflect.TypeFor[fmt.Stringer]()) &&
		!t.Implements(reflect.TypeFor[error]())
}

func findKeyNode(key string, m *yaml.Node) *yaml.Node {
	if m == nil {
		return nil
	}

	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i]
		}
	}
	return nil
}

// FindValueUntyped finds a value in the ordered map by key if the stored value for that key implements GetValueUntyped otherwise just returns the value.
func (o *Map[K, V]) FindValueUntyped(key string) any {
	for pair := First(o); pair != nil; pair = pair.Next() {
		var k any = pair.Key()
		if hvut, ok := k.(hasValueUntyped); ok {
			if fmt.Sprintf("%v", hvut.GetValueUntyped()) == key {
				return pair.Value()
			}
		}
		if fmt.Sprintf("%v", k) == key {
			return pair.Value()
		}
	}

	return nil
}
