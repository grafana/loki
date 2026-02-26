// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// BuildModel accepts a yaml.Node pointer and a model, which can be any struct. Using reflection, the model is
// analyzed and the names of all the properties are extracted from the model and subsequently looked up from within
// the yaml.Node.Content value.
//
// BuildModel is non-recursive and will only build out a single layer of the node tree.
func BuildModel(node *yaml.Node, model interface{}) error {
	if node == nil {
		return nil
	}
	node = utils.NodeAlias(node)
	utils.CheckForMergeNodes(node)

	if reflect.ValueOf(model).Type().Kind() != reflect.Pointer {
		return fmt.Errorf("cannot build model on non-pointer: %v", reflect.ValueOf(model).Type().Kind())
	}
	v := reflect.ValueOf(model).Elem()
	num := v.NumField()
	for i := 0; i < num; i++ {

		fName := v.Type().Field(i).Name

		if fName == "Extensions" {
			continue // internal construct
		}

		if fName == "PathItems" {
			continue // internal construct
		}

		kn, vn := utils.FindKeyNodeTop(fName, node.Content)
		if vn == nil {
			// no point in going on.
			continue
		}

		field := v.FieldByName(fName)
		kind := field.Kind()
		switch kind {
		case reflect.Struct, reflect.Slice, reflect.Map, reflect.Pointer:
			vn = utils.NodeAlias(vn)
			SetField(&field, vn, kn)
		default:
			return fmt.Errorf("unable to parse unsupported type: %v", kind)
		}

	}

	return nil
}

// SetField accepts a field reflection value, a yaml.Node valueNode and a yaml.Node keyNode. Using reflection, the
// function will attempt to set the value of the field based on the key and value nodes. This method is only useful
// for low-level models, it has no value to high-level ones.
func SetField(field *reflect.Value, valueNode *yaml.Node, keyNode *yaml.Node) {
	if valueNode == nil {
		return
	}

	switch field.Type() {

	case reflect.TypeOf(orderedmap.New[string, NodeReference[*yaml.Node]]()):

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[string, NodeReference[*yaml.Node]]()
				var currentLabel string
				for i, sliceItem := range valueNode.Content {
					if i%2 == 0 {
						currentLabel = sliceItem.Value
						continue
					}
					items.Set(currentLabel, NodeReference[*yaml.Node]{
						Value:     sliceItem,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf(orderedmap.New[string, NodeReference[string]]()):

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[string, NodeReference[string]]()
				var currentLabel string
				for i, sliceItem := range valueNode.Content {
					if i%2 == 0 {
						currentLabel = sliceItem.Value
						continue
					}
					items.Set(currentLabel, NodeReference[string]{
						Value:     fmt.Sprintf("%v", sliceItem.Value),
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf(NodeReference[*yaml.Node]{}):

		if field.CanSet() {
			or := NodeReference[*yaml.Node]{Value: valueNode, ValueNode: valueNode, KeyNode: keyNode}
			field.Set(reflect.ValueOf(or))
		}

	case reflect.TypeOf([]NodeReference[*yaml.Node]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[*yaml.Node]
				for _, sliceItem := range valueNode.Content {
					items = append(items, NodeReference[*yaml.Node]{
						Value:     sliceItem,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf(NodeReference[string]{}):

		if field.CanSet() {
			nr := NodeReference[string]{
				Value:     fmt.Sprintf("%v", valueNode.Value),
				ValueNode: valueNode,
				KeyNode:   keyNode,
			}
			field.Set(reflect.ValueOf(nr))
		}

	case reflect.TypeOf(ValueReference[string]{}):

		if field.CanSet() {
			nr := ValueReference[string]{
				Value:     fmt.Sprintf("%v", valueNode.Value),
				ValueNode: valueNode,
			}
			field.Set(reflect.ValueOf(nr))
		}

	case reflect.TypeOf(NodeReference[bool]{}):

		if utils.IsNodeBoolValue(valueNode) {
			if field.CanSet() {
				bv, _ := strconv.ParseBool(valueNode.Value)
				nr := NodeReference[bool]{
					Value:     bv,
					ValueNode: valueNode,
					KeyNode:   keyNode,
				}
				field.Set(reflect.ValueOf(nr))
			}
		}

	case reflect.TypeOf(NodeReference[int]{}):

		if utils.IsNodeIntValue(valueNode) {
			if field.CanSet() {
				fv, _ := strconv.Atoi(valueNode.Value)
				nr := NodeReference[int]{
					Value:     fv,
					ValueNode: valueNode,
					KeyNode:   keyNode,
				}
				field.Set(reflect.ValueOf(nr))
			}
		}

	case reflect.TypeOf(NodeReference[int64]{}):

		if utils.IsNodeIntValue(valueNode) || utils.IsNodeFloatValue(valueNode) {
			if field.CanSet() {
				fv, _ := strconv.ParseInt(valueNode.Value, 10, 64)
				nr := NodeReference[int64]{
					Value:     fv,
					ValueNode: valueNode,
					KeyNode:   keyNode,
				}
				field.Set(reflect.ValueOf(nr))
			}
		}

	case reflect.TypeOf(NodeReference[float32]{}):

		if utils.IsNodeNumberValue(valueNode) {
			if field.CanSet() {
				fv, _ := strconv.ParseFloat(valueNode.Value, 32)
				nr := NodeReference[float32]{
					Value:     float32(fv),
					ValueNode: valueNode,
					KeyNode:   keyNode,
				}
				field.Set(reflect.ValueOf(nr))
			}
		}

	case reflect.TypeOf(NodeReference[float64]{}):

		if utils.IsNodeNumberValue(valueNode) {
			if field.CanSet() {
				fv, _ := strconv.ParseFloat(valueNode.Value, 64)
				nr := NodeReference[float64]{
					Value:     fv,
					ValueNode: valueNode,
					KeyNode:   keyNode,
				}
				field.Set(reflect.ValueOf(nr))
			}
		}

	case reflect.TypeOf([]NodeReference[string]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[string]
				for _, sliceItem := range valueNode.Content {
					items = append(items, NodeReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf([]NodeReference[float32]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[float32]
				for _, sliceItem := range valueNode.Content {
					fv, _ := strconv.ParseFloat(sliceItem.Value, 32)
					items = append(items, NodeReference[float32]{
						Value:     float32(fv),
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf([]NodeReference[float64]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[float64]
				for _, sliceItem := range valueNode.Content {
					fv, _ := strconv.ParseFloat(sliceItem.Value, 64)
					items = append(items, NodeReference[float64]{Value: fv, ValueNode: sliceItem})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf([]NodeReference[int]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[int]
				for _, sliceItem := range valueNode.Content {
					iv, _ := strconv.Atoi(sliceItem.Value)
					items = append(items, NodeReference[int]{
						Value:     iv,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf([]NodeReference[int64]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[int64]
				for _, sliceItem := range valueNode.Content {
					iv, _ := strconv.ParseInt(sliceItem.Value, 10, 64)
					items = append(items, NodeReference[int64]{
						Value:     iv,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf([]NodeReference[bool]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []NodeReference[bool]
				for _, sliceItem := range valueNode.Content {
					bv, _ := strconv.ParseBool(sliceItem.Value)
					items = append(items, NodeReference[bool]{
						Value:     bv,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	// helper for unpacking string maps.
	case reflect.TypeOf(orderedmap.New[KeyReference[string], ValueReference[string]]()):

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				var cf *yaml.Node
				for i, sliceItem := range valueNode.Content {
					if i%2 == 0 {
						cf = sliceItem
						continue
					}
					items.Set(KeyReference[string]{
						Value:   cf.Value,
						KeyNode: cf,
					}, ValueReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case reflect.TypeOf(KeyReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]{}):

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				var cf *yaml.Node
				for i, sliceItem := range valueNode.Content {
					if i%2 == 0 {
						cf = sliceItem
						continue
					}
					items.Set(KeyReference[string]{
						Value:   cf.Value,
						KeyNode: cf,
					}, ValueReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
					})
				}
				ref := KeyReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]{
					Value:   items,
					KeyNode: keyNode,
				}
				field.Set(reflect.ValueOf(ref))
			}
		}
	case reflect.TypeOf(NodeReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]{}):
		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				var cf *yaml.Node
				for i, sliceItem := range valueNode.Content {
					if i%2 == 0 {
						cf = sliceItem
						continue
					}
					items.Set(KeyReference[string]{
						Value:   cf.Value,
						KeyNode: cf,
					}, ValueReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
					})
				}
				ref := NodeReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]{
					Value:     items,
					KeyNode:   keyNode,
					ValueNode: valueNode,
				}
				field.Set(reflect.ValueOf(ref))
			}
		}
	case reflect.TypeOf(NodeReference[[]ValueReference[string]]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []ValueReference[string]
				for _, sliceItem := range valueNode.Content {
					items = append(items, ValueReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
					})
				}
				n := NodeReference[[]ValueReference[string]]{
					Value:     items,
					KeyNode:   keyNode,
					ValueNode: valueNode,
				}
				field.Set(reflect.ValueOf(n))
			}
		}

	case reflect.TypeOf(NodeReference[[]ValueReference[*yaml.Node]]{}):

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				var items []ValueReference[*yaml.Node]
				for _, sliceItem := range valueNode.Content {
					items = append(items, ValueReference[*yaml.Node]{
						Value:     sliceItem,
						ValueNode: sliceItem,
					})
				}
				n := NodeReference[[]ValueReference[*yaml.Node]]{
					Value:     items,
					KeyNode:   keyNode,
					ValueNode: valueNode,
				}
				field.Set(reflect.ValueOf(n))
			}
		}

	default:
		// we want to ignore everything else, each model handles its own complex types.
		break
	}
}

// BuildModelAsync is a convenience function for calling BuildModel from a goroutine, requires a sync.WaitGroup
func BuildModelAsync(n *yaml.Node, model interface{}, lwg *sync.WaitGroup, errors *[]error) {
	if n != nil {
		err := BuildModel(n, model)
		if err != nil {
			*errors = append(*errors, err)
		}
	}
	lwg.Done()
}
