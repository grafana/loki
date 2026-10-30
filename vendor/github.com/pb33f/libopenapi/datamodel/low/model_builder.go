// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package low

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
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

// lowerIfNeeded returns the input unchanged when it contains no uppercase ASCII and no
// multibyte runes (the overwhelmingly common case for OpenAPI keys), avoiding the
// per-key allocation of strings.ToLower on the BuildModel hot path.
func lowerIfNeeded(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= utf8.RuneSelf || ('A' <= c && c <= 'Z') {
			return strings.ToLower(s)
		}
	}
	return s
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
	node = utils.ResolveMergeNodes(node)

	if reflect.ValueOf(model).Type().Kind() != reflect.Pointer {
		return fmt.Errorf("cannot build model on non-pointer: %v", reflect.ValueOf(model).Type().Kind())
	}

	// Build a map of lowercase YAML key -> index for O(1) lookup per field.
	// Preserves first-write-wins semantics matching FindKeyNodeTop behavior
	// (direct keys before merge-expanded keys).
	content := node.Content
	keyMap := make(map[string]int, len(content)/2)
	for j := 0; j < len(content)-1; j += 2 {
		k := lowerIfNeeded(utils.NodeAlias(content[j]).Value)
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

// setField*Type are the field types SetField switches on, computed once rather than on every call.
var (
	setFieldMapStringNodeReferenceYAMLNodeType                         = reflect.TypeFor[*orderedmap.Map[string, NodeReference[*yaml.Node]]]()
	setFieldMapStringNodeReferenceStringType                           = reflect.TypeFor[*orderedmap.Map[string, NodeReference[string]]]()
	setFieldNodeReferenceYAMLNodeType                                  = reflect.TypeFor[NodeReference[*yaml.Node]]()
	setFieldSliceOfNodeReferenceYAMLNodeType                           = reflect.TypeFor[[]NodeReference[*yaml.Node]]()
	setFieldNodeReferenceStringType                                    = reflect.TypeFor[NodeReference[string]]()
	setFieldValueReferenceStringType                                   = reflect.TypeFor[ValueReference[string]]()
	setFieldNodeReferenceBoolType                                      = reflect.TypeFor[NodeReference[bool]]()
	setFieldNodeReferenceIntType                                       = reflect.TypeFor[NodeReference[int]]()
	setFieldNodeReferenceInt64Type                                     = reflect.TypeFor[NodeReference[int64]]()
	setFieldNodeReferenceFloat32Type                                   = reflect.TypeFor[NodeReference[float32]]()
	setFieldNodeReferenceFloat64Type                                   = reflect.TypeFor[NodeReference[float64]]()
	setFieldSliceOfNodeReferenceStringType                             = reflect.TypeFor[[]NodeReference[string]]()
	setFieldSliceOfNodeReferenceFloat32Type                            = reflect.TypeFor[[]NodeReference[float32]]()
	setFieldSliceOfNodeReferenceFloat64Type                            = reflect.TypeFor[[]NodeReference[float64]]()
	setFieldSliceOfNodeReferenceIntType                                = reflect.TypeFor[[]NodeReference[int]]()
	setFieldSliceOfNodeReferenceInt64Type                              = reflect.TypeFor[[]NodeReference[int64]]()
	setFieldSliceOfNodeReferenceBoolType                               = reflect.TypeFor[[]NodeReference[bool]]()
	setFieldMapKeyReferenceStringValueReferenceStringType              = reflect.TypeFor[*orderedmap.Map[KeyReference[string], ValueReference[string]]]()
	setFieldKeyReferenceMapKeyReferenceStringValueReferenceStringType  = reflect.TypeFor[KeyReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]]()
	setFieldNodeReferenceMapKeyReferenceStringValueReferenceStringType = reflect.TypeFor[NodeReference[*orderedmap.Map[KeyReference[string], ValueReference[string]]]]()
	setFieldNodeReferenceSliceValueReferenceStringType                 = reflect.TypeFor[NodeReference[[]ValueReference[string]]]()
	setFieldNodeReferenceSliceValueReferenceYAMLNodeType               = reflect.TypeFor[NodeReference[[]ValueReference[*yaml.Node]]]()
)

// SetField accepts a field reflection value, a yaml.Node valueNode and a yaml.Node keyNode. Using reflection, the
// function will attempt to set the value of the field based on the key and value nodes. This method is only useful
// for low-level models, it has no value to high-level ones.
func SetField(field *reflect.Value, valueNode *yaml.Node, keyNode *yaml.Node) {
	if valueNode == nil {
		return
	}

	// Typed map fields read effective contents without rewriting the source
	// mapping retained by NodeReference metadata or another model builder.
	content := valueNode.Content
	if utils.IsNodeMap(valueNode) {
		content = utils.ResolveMergeNodes(valueNode).Content
	}

	switch field.Type() {

	case setFieldMapStringNodeReferenceYAMLNodeType:

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[string, NodeReference[*yaml.Node]]()
				var currentLabel string
				for i, sliceItem := range content {
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

	case setFieldMapStringNodeReferenceStringType:

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[string, NodeReference[string]]()
				var currentLabel string
				for i, sliceItem := range content {
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

	case setFieldNodeReferenceYAMLNodeType:

		if field.CanSet() {
			or := NodeReference[*yaml.Node]{Value: valueNode, ValueNode: valueNode, KeyNode: keyNode}
			field.Set(reflect.ValueOf(or))
		}

	case setFieldSliceOfNodeReferenceYAMLNodeType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[*yaml.Node], 0, len(content))
				for _, sliceItem := range content {
					items = append(items, NodeReference[*yaml.Node]{
						Value:     sliceItem,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case setFieldNodeReferenceStringType:

		if field.CanSet() {
			nr := NodeReference[string]{
				Value:     valueNode.Value,
				ValueNode: valueNode,
				KeyNode:   keyNode,
			}
			field.Set(reflect.ValueOf(nr))
		}

	case setFieldValueReferenceStringType:

		if field.CanSet() {
			nr := ValueReference[string]{
				Value:     valueNode.Value,
				ValueNode: valueNode,
			}
			field.Set(reflect.ValueOf(nr))
		}

	case setFieldNodeReferenceBoolType:

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

	case setFieldNodeReferenceIntType:

		if utils.IsNodeIntValue(valueNode) || utils.IsNodeFloatValue(valueNode) {
			if field.CanSet() {
				if fv, ok := parseIntNode(valueNode.Value); ok {
					nr := NodeReference[int]{
						Value:     fv,
						ValueNode: valueNode,
						KeyNode:   keyNode,
					}
					field.Set(reflect.ValueOf(nr))
				}
			}
		}

	case setFieldNodeReferenceInt64Type:

		if utils.IsNodeIntValue(valueNode) || utils.IsNodeFloatValue(valueNode) {
			if field.CanSet() {
				if fv, ok := parseInt64Node(valueNode.Value); ok {
					nr := NodeReference[int64]{
						Value:     fv,
						ValueNode: valueNode,
						KeyNode:   keyNode,
					}
					field.Set(reflect.ValueOf(nr))
				}
			}
		}

	case setFieldNodeReferenceFloat32Type:

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

	case setFieldNodeReferenceFloat64Type:

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

	case setFieldSliceOfNodeReferenceStringType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[string], 0, len(content))
				for _, sliceItem := range content {
					items = append(items, NodeReference[string]{
						Value:     sliceItem.Value,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case setFieldSliceOfNodeReferenceFloat32Type:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[float32], 0, len(content))
				for _, sliceItem := range content {
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

	case setFieldSliceOfNodeReferenceFloat64Type:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[float64], 0, len(content))
				for _, sliceItem := range content {
					fv, _ := strconv.ParseFloat(sliceItem.Value, 64)
					items = append(items, NodeReference[float64]{Value: fv, ValueNode: sliceItem})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case setFieldSliceOfNodeReferenceIntType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[int], 0, len(content))
				for _, sliceItem := range content {
					iv, _ := parseIntNode(sliceItem.Value)
					items = append(items, NodeReference[int]{
						Value:     iv,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case setFieldSliceOfNodeReferenceInt64Type:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[int64], 0, len(content))
				for _, sliceItem := range content {
					iv, _ := parseInt64Node(sliceItem.Value)
					items = append(items, NodeReference[int64]{
						Value:     iv,
						ValueNode: sliceItem,
						KeyNode:   valueNode,
					})
				}
				field.Set(reflect.ValueOf(items))
			}
		}

	case setFieldSliceOfNodeReferenceBoolType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]NodeReference[bool], 0, len(content))
				for _, sliceItem := range content {
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
	case setFieldMapKeyReferenceStringValueReferenceStringType:

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				for i := 0; i < len(content)-1; i += 2 {
					cf := content[i]
					sliceItem := content[i+1]
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

	case setFieldKeyReferenceMapKeyReferenceStringValueReferenceStringType:

		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				for i := 0; i < len(content)-1; i += 2 {
					cf := content[i]
					sliceItem := content[i+1]
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
	case setFieldNodeReferenceMapKeyReferenceStringValueReferenceStringType:
		if utils.IsNodeMap(valueNode) {
			if field.CanSet() {
				items := orderedmap.New[KeyReference[string], ValueReference[string]]()
				for i := 0; i < len(content)-1; i += 2 {
					cf := content[i]
					sliceItem := content[i+1]
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
	case setFieldNodeReferenceSliceValueReferenceStringType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]ValueReference[string], 0, len(content))
				for _, sliceItem := range content {
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

	case setFieldNodeReferenceSliceValueReferenceYAMLNodeType:

		if utils.IsNodeArray(valueNode) {
			if field.CanSet() {
				items := make([]ValueReference[*yaml.Node], 0, len(content))
				for _, sliceItem := range content {
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

// parseInt64Node parses the text of an int or float YAML node as an int64.
//
// Integers use strconv.ParseInt, so out-of-range values clamp to math.MaxInt64 / math.MinInt64.
// Floats with a whole-number value are converted (2.0 -> 2, 1e3 -> 1000), and floats at or beyond
// ±2^63 (including YAML .inf / -.inf) clamp the same way. Fractions and NaN return false.
func parseInt64Node(value string) (int64, bool) {
	i, err := strconv.ParseInt(value, 10, 64)
	if err == nil || errors.Is(err, strconv.ErrRange) {
		return i, true
	}
	// YAML spells infinity with a leading dot (.inf, +.inf, -.inf), which strconv does not accept.
	// .nan fails to parse and is rejected below.
	switch strings.ToLower(strings.TrimPrefix(value, "+")) {
	case ".inf":
		return math.MaxInt64, true
	case "-.inf":
		return math.MinInt64, true
	}
	f, err := strconv.ParseFloat(value, 64)
	switch {
	case err != nil || math.IsNaN(f):
		return 0, false
	case f >= math.MaxInt64:
		return math.MaxInt64, true
	case f <= math.MinInt64:
		return math.MinInt64, true
	case f != math.Trunc(f):
		return 0, false
	}
	return int64(f), true
}

// parseIntNode is parseInt64Node for an int field. Where int is 32 bits, strconv.Atoi clamps the
// result to math.MaxInt / math.MinInt instead of letting it wrap.
func parseIntNode(value string) (int, bool) {
	v, ok := parseInt64Node(value)
	i, _ := strconv.Atoi(strconv.FormatInt(v, 10))
	return i, ok
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
