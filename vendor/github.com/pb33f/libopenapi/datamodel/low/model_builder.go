// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

type buildModelField struct {
	lookupKey string
	index     int
	kind      reflect.Kind
}

var buildModelFieldCache sync.Map

func buildModelFields(modelType reflect.Type) []buildModelField {
	if cached, ok := buildModelFieldCache.Load(modelType); ok {
		return cached.([]buildModelField)
	}

	fields := make([]buildModelField, 0, modelType.NumField())
	for i := 0; i < modelType.NumField(); i++ {
		structField := modelType.Field(i)
		if !structField.IsExported() || structField.Anonymous {
			continue
		}
		if structField.Name == "Extensions" || structField.Name == "PathItems" {
			continue
		}
		fields = append(fields, buildModelField{
			lookupKey: strings.ToLower(structField.Name),
			index:     i,
			kind:      structField.Type.Kind(),
		})
	}

	actual, _ := buildModelFieldCache.LoadOrStore(modelType, fields)
	return actual.([]buildModelField)
}

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

	// Build a map of lowercase YAML key -> index for O(1) lookup per field.
	// Preserves first-write-wins semantics matching FindKeyNodeTop behavior
	// (direct keys before merge-expanded keys).
	content := node.Content
	keyMap := make(map[string]int, len(content)/2)
	for j := 0; j < len(content)-1; j += 2 {
		k := strings.ToLower(utils.NodeAlias(content[j]).Value)
		if _, exists := keyMap[k]; !exists {
			keyMap[k] = j
		}
	}

	v := reflect.ValueOf(model).Elem()
	for _, modelField := range buildModelFields(v.Type()) {
		idx, ok := keyMap[modelField.lookupKey]
		if !ok {
			continue
		}
		kn := utils.NodeAlias(content[idx])
		vn := utils.NodeAlias(content[idx+1])

		field := v.Field(modelField.index)
		switch modelField.kind {
		case reflect.Struct, reflect.Slice, reflect.Map, reflect.Pointer:
			vn = utils.NodeAlias(vn)
			SetField(&field, vn, kn)
		default:
			return fmt.Errorf("unable to parse unsupported type: %v", modelField.kind)
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
						Value:     sliceItem.Value,
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
				items := make([]NodeReference[*yaml.Node], 0, len(valueNode.Content))
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
				Value:     valueNode.Value,
				ValueNode: valueNode,
				KeyNode:   keyNode,
			}
			field.Set(reflect.ValueOf(nr))
		}

	case reflect.TypeOf(ValueReference[string]{}):

		if field.CanSet() {
			nr := ValueReference[string]{
				Value:     valueNode.Value,
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
				items := make([]NodeReference[string], 0, len(valueNode.Content))
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
				items := make([]NodeReference[float32], 0, len(valueNode.Content))
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
				items := make([]NodeReference[float64], 0, len(valueNode.Content))
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
				items := make([]NodeReference[int], 0, len(valueNode.Content))
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
				items := make([]NodeReference[int64], 0, len(valueNode.Content))
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
				items := make([]NodeReference[bool], 0, len(valueNode.Content))
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
				for i := 0; i < len(valueNode.Content)-1; i += 2 {
					cf := valueNode.Content[i]
					sliceItem := valueNode.Content[i+1]
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
				for i := 0; i < len(valueNode.Content)-1; i += 2 {
					cf := valueNode.Content[i]
					sliceItem := valueNode.Content[i+1]
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
				for i := 0; i < len(valueNode.Content)-1; i += 2 {
					cf := valueNode.Content[i]
					sliceItem := valueNode.Content[i+1]
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
				items := make([]ValueReference[string], 0, len(valueNode.Content))
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
				items := make([]ValueReference[*yaml.Node], 0, len(valueNode.Content))
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
