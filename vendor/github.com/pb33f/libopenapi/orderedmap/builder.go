package orderedmap

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/nodes"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
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

// ToYamlNode converts the ordered map to a yaml node ready for marshalling.
func (o *Map[K, V]) ToYamlNode(n NodeBuilder, l any) *yaml.Node {
	p := utils.CreateEmptyMapNode()
	if o != nil {
		p.Content = make([]*yaml.Node, 0)
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

	for pair := First(o); pair != nil; pair = pair.Next() {
		var k any = pair.Key()
		if m, ok := k.(marshaler); ok { // TODO marshal inline?
			mk, _ := m.MarshalYAML()
			b, _ := yaml.Marshal(mk)
			k = strings.TrimSpace(string(b))
		}

		ks := k.(string)

		var keyStyle yaml.Style
		keyNode := findKeyNode(ks, vn)
		if keyNode != nil {
			keyStyle = keyNode.Style
		}

		var lv any
		if l != nil {
			if hvut, ok := l.(hasValueUntyped); ok {
				vut := hvut.GetValueUntyped()
				if m, ok := vut.(findValueUntyped); ok {
					lv = m.FindValueUntyped(ks)
				}
			}
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
