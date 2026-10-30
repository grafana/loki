// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package high

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/high/nodes"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
)

// NodeBuilder is a structure used by libopenapi high-level objects, to render themselves back to YAML.
// this allows high-level objects to be 'mutable' because all changes will be rendered out.
type NodeBuilder struct {
	Version       float32
	Nodes         []*nodes.NodeEntry
	High          any
	Low           any
	Resolve       bool // If set to true, all references will be rendered inline
	RenderContext any  // Context for inline rendering cycle detection (*base.InlineRenderContext)
	Errors        []error
}

// RenderableInlineWithContext is an interface that can be implemented by types that support
// context-aware inline rendering for proper cycle detection in concurrent scenarios.
// The context parameter should be *base.InlineRenderContext but is typed as any to avoid import cycles.
type RenderableInlineWithContext interface {
	MarshalYAMLInlineWithContext(ctx any) (interface{}, error)
}

const renderZero = "renderZero"

func originalFloatLexeme(value float64, lowValue any) (string, bool) {
	vnut, ok := lowValue.(low.HasValueNodeUntyped)
	if !ok {
		return "", false
	}

	valueNode := vnut.GetValueNode()
	if valueNode == nil || !utils.IsNodeNumberValue(valueNode) {
		return "", false
	}

	parsed, err := strconv.ParseFloat(valueNode.Value, 64)
	if err != nil {
		return "", false
	}

	if parsed != value {
		return "", false
	}
	if value == 0 && math.Signbit(parsed) != math.Signbit(value) {
		return "", false
	}

	return valueNode.Value, true
}

// nodeBuilderField holds the reflection metadata NewNodeBuilder needs for one field of a high-level struct.
// It depends only on the high and low struct types, so it is derived once per type pair and cached rather
// than re-derived (field lookups by name, yaml tag parsing) for every object rendered.
type nodeBuilderField struct {
	index      int    // index of the field in the high-level struct
	name       string // field name, used as the NodeEntry key
	extensions bool   // the Extensions field, which renders its map entries rather than itself
	tagName    string // yaml tag name
	renderZero bool
	omitEmpty  bool

	// lowIndex is the index path of the same-named field in the low-level struct, nil when it has none.
	lowIndex []int

	// lowEmptier and lowValueNoder report whether the low field's value type (the element type for a
	// pointer field) implements IsEmpty and GetValueNode. When it does, those methods are called through
	// a pointer to the field instead of copying the field into an interface: the pointer's method set
	// carries the value-receiver methods, so the result is identical. lowDynamic marks a value type that
	// is itself an interface or pointer, whose methods depend on the value held at runtime.
	lowEmptier    bool
	lowValueNoder bool
	lowDynamic    bool
}

type nodeBuilderTypes struct {
	high reflect.Type
	low  reflect.Type
}

type lowEmptier interface{ IsEmpty() bool }

type lowValueNoder interface{ GetValueNode() *yaml.Node }

var (
	nodeBuilderFieldCache sync.Map // nodeBuilderTypes -> []nodeBuilderField

	lowEmptierType    = reflect.TypeFor[lowEmptier]()
	lowValueNoderType = reflect.TypeFor[lowValueNoder]()
	hasKeyNodeType    = reflect.TypeFor[low.HasKeyNode]()
	stringType        = reflect.TypeFor[string]()
)

// nodeBuilderFields returns the cached field metadata for a high-level struct type, paired with the
// low-level struct type (nil when there is no low-level model).
func nodeBuilderFields(highType, lowType reflect.Type) []nodeBuilderField {
	key := nodeBuilderTypes{high: highType, low: lowType}
	if cached, ok := nodeBuilderFieldCache.Load(key); ok {
		return cached.([]nodeBuilderField)
	}
	fields := make([]nodeBuilderField, 0, highType.NumField())
	for i := 0; i < highType.NumField(); i++ {
		sf := highType.Field(i)
		// only operate on exported fields.
		if unicode.IsLower(rune(sf.Name[0])) {
			continue
		}
		field := nodeBuilderField{index: i, name: sf.Name}
		if lowType != nil {
			if lsf, ok := lowType.FieldByName(sf.Name); ok {
				field.lowIndex = lsf.Index
				valueType := lsf.Type
				if valueType.Kind() == reflect.Ptr {
					valueType = valueType.Elem()
				}
				if valueType.Kind() == reflect.Interface || valueType.Kind() == reflect.Ptr {
					field.lowDynamic = true
				} else {
					field.lowEmptier = valueType.Implements(lowEmptierType)
					field.lowValueNoder = valueType.Implements(lowValueNoderType)
				}
			}
		}
		if sf.Name == "Extensions" {
			field.extensions = true
			fields = append(fields, field)
			continue
		}
		tag := sf.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		tagParts := strings.Split(tag, ",")
		field.tagName = tagParts[0]
		for _, part := range tagParts {
			if part == renderZero {
				field.renderZero = true
			}
			if part == "omitempty" {
				field.omitEmpty = true
			}
		}
		fields = append(fields, field)
	}
	actual, _ := nodeBuilderFieldCache.LoadOrStore(key, fields)
	return actual.([]nodeBuilderField)
}

// NewNodeBuilder will create a new NodeBuilder instance, this is the only way to create a NodeBuilder.
// The function accepts a high level object and a low level object (need to be siblings/same type).
//
// Using reflection, a map of every field in the high level object is created, ready to be rendered.
func NewNodeBuilder(high any, low any) *NodeBuilder {
	// create a new node builder
	nb := new(NodeBuilder)
	nb.High = high
	if low != nil {
		nb.Low = low
	}

	// resolve the low-level struct once; its fields supply line numbers and original styles.
	var lowStruct reflect.Value
	var lowType reflect.Type
	if low != nil {
		if lv := reflect.ValueOf(low); !lv.IsZero() {
			if lv.Kind() == reflect.Ptr {
				lowStruct = lv.Elem()
			} else {
				lowStruct = lv
			}
			lowType = lowStruct.Type()
		}
	}

	// extract fields from the high level object and add them into our node builder.
	// this will allow us to extract the line numbers from the low level object as well.
	highStruct := reflect.ValueOf(high).Elem()
	fields := nodeBuilderFields(highStruct.Type(), lowType)
	for i := range fields {
		nb.add(&fields[i], highStruct, lowStruct)
	}
	return nb
}

// lowFieldHasContent reports whether a low-level field holds content, which keeps a zero high-level value
// in the rendered output.
func lowFieldHasContent(field *nodeBuilderField, lowFieldValue reflect.Value) bool {
	if field.lowDynamic {
		return dynamicLowFieldHasContent(lowFieldValue)
	}
	if !field.lowEmptier && !field.lowValueNoder {
		return false
	}
	holder := lowFieldValue
	if holder.Kind() == reflect.Ptr {
		if holder.IsNil() {
			return false
		}
	} else if holder.CanAddr() {
		holder = holder.Addr()
	}
	h := holder.Interface()
	if field.lowEmptier && !h.(lowEmptier).IsEmpty() {
		return true
	}
	return field.lowValueNoder && h.(lowValueNoder).GetValueNode() != nil
}

// dynamicLowFieldHasContent is lowFieldHasContent for a field whose methods depend on the value it holds.
func dynamicLowFieldHasContent(lowFieldValue reflect.Value) bool {
	var lowInterface any
	if lowFieldValue.Kind() == reflect.Ptr {
		if lowFieldValue.IsNil() {
			return false
		}
		lowInterface = lowFieldValue.Elem().Interface()
	} else {
		lowInterface = lowFieldValue.Interface()
	}
	if emptier, ok := lowInterface.(lowEmptier); ok && !emptier.IsEmpty() {
		return true
	}
	if nodeGetter, ok := lowInterface.(lowValueNoder); ok {
		return nodeGetter.GetValueNode() != nil
	}
	return false
}

// lowestKeyLine returns the lowest key line of the items in a low-level slice, where items without a key
// node count as line zero.
func lowestKeyLine(value reflect.Value) int {
	elemType := value.Type().Elem()
	dynamic := elemType.Kind() == reflect.Interface
	if !dynamic && !elemType.Implements(hasKeyNodeType) {
		// no item can have a key node, so every item counts as line zero.
		return 0
	}
	lowest := 0
	for g := 0; g < value.Len(); g++ {
		item := value.Index(g)
		if !dynamic && item.Kind() != reflect.Ptr {
			item = item.Addr() // call through a pointer rather than copying the item
		}
		line := 0
		if we, ok := item.Interface().(low.HasKeyNode); ok {
			line = we.GetKeyNode().Line
		}
		if g == 0 || line < lowest {
			lowest = line
		}
	}
	return lowest
}

func (n *NodeBuilder) add(field *nodeBuilderField, highStruct, lowStruct reflect.Value) {
	var (
		lowFieldValue reflect.Value
		lowFieldValid bool
	)

	if lowStruct.IsValid() && field.lowIndex != nil {
		lowFieldValue = lowStruct.FieldByIndex(field.lowIndex)
		lowFieldValid = true
	}

	// if the key is 'Extensions' then we need to extract the keys from the map
	// and add them to the node builder.
	if field.extensions {
		ev := highStruct.Field(field.index).Interface()
		var extensions *orderedmap.Map[string, *yaml.Node]
		if ev != nil {
			extensions = ev.(*orderedmap.Map[string, *yaml.Node])
		}

		var lowExtensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
		if lowStruct.IsValid() {
			if j, ok := n.Low.(low.HasExtensionsUntyped); ok {
				lowExtensions = j.GetExtensions()
			}
		}

		j := 0
		if lowExtensions != nil {
			// If we have low extensions get the original lowest line number so we end up in the same place
			for ext := range lowExtensions.KeysFromOldest() {
				if j == 0 || ext.KeyNode.Line < j {
					j = ext.KeyNode.Line
				}
			}
		}

		for ext, node := range extensions.FromOldest() {
			nodeEntry := &nodes.NodeEntry{Tag: ext, Key: ext, Value: node, Line: j}

			if lowExtensions != nil {
				lowItem := low.FindItemInOrderedMap(ext, lowExtensions)
				nodeEntry.LowValue = lowItem
			}
			n.Nodes = append(n.Nodes, nodeEntry)
			j++
		}
		// done, extensions are handled separately.
		return
	}

	tagName := field.tagName
	renderZeroFlag, omitEmptyFlag := field.renderZero, field.omitEmpty

	// extract the value of the field
	fieldValue := highStruct.Field(field.index)
	f := fieldValue.Interface()
	value := reflect.ValueOf(f)
	var isZero bool
	if (value.Kind() == reflect.Interface || value.Kind() == reflect.Ptr) && value.IsNil() {
		isZero = true
	} else if zeroer, ok := f.(yaml.IsZeroer); ok && zeroer.IsZero() {
		isZero = true
	} else if f == nil || value.IsZero() {
		if tagName != "description" {
			isZero = true
		} else {
			if omitEmptyFlag {
				isZero = true
			}
		}
	}

	if isZero && lowFieldValid && lowFieldHasContent(field, lowFieldValue) {
		isZero = false
	}

	if !renderZeroFlag && isZero || omitEmptyFlag && isZero {
		return
	}

	// create a new node entry
	nodeEntry := &nodes.NodeEntry{Tag: tagName, Key: field.name}
	nodeEntry.RenderZero = renderZeroFlag
	switch value.Kind() {
	case reflect.Float64, reflect.Float32:
		nodeEntry.Value = value.Float()
		x := float64(int(value.Float()*100)) / 100 // trim this down
		nodeEntry.StringValue = strconv.FormatFloat(x, 'f', -1, 64)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		nodeEntry.Value = value.Int()
		nodeEntry.StringValue = value.String()
	case reflect.String:
		if value.Type() == stringType {
			nodeEntry.Value = f // already boxed as a plain string
		} else {
			nodeEntry.Value = value.String()
		}
	case reflect.Bool:
		nodeEntry.Value = value.Bool()
	case reflect.Slice:
		if tagName == "type" {
			if value.Len() == 1 {
				nodeEntry.Value = value.Index(0).String()
			} else {
				nodeEntry.Value = f
			}
		} else {
			if renderZeroFlag || (!value.IsNil() && !isZero) {
				nodeEntry.Value = f
			}
		}
	case reflect.Ptr:
		if !value.IsNil() {
			nodeEntry.Value = f
		}
	default:
		nodeEntry.Value = f
	}

	// if there is no low-level object, then we cannot extract line numbers,
	// so skip and default to 0, which means a new entry to the spec.
	// this will place new content and the top of the rendered object.
	if lowFieldValid {
		fLow := lowFieldValue.Interface()
		value = reflect.ValueOf(fLow)

		nodeEntry.LowValue = fLow
		switch value.Kind() {

		case reflect.Slice:
			nodeEntry.Line = lowestKeyLine(value)
		case reflect.Struct:
			nodeEntry.Line = 9999 + field.index
			if nb, ok := fLow.(low.HasValueNodeUntyped); ok {
				if nb.IsReference() {
					if jk, kj := fLow.(low.HasKeyNode); kj {
						nodeEntry.Line = jk.GetKeyNode().Line
						break
					}
				}
				if nb.GetValueNode() != nil {
					nodeEntry.Line = nb.GetValueNode().Line
				}
			}
		default:
			// everything else, weight it to the bottom of the rendered object.
			// this is things that we have no way of knowing where they should be placed.
			nodeEntry.Line = 9999 + field.index
		}
	}
	if nodeEntry.Value != nil {
		n.Nodes = append(n.Nodes, nodeEntry)
	}
}

func (n *NodeBuilder) renderReference(fg low.IsReferenced) *yaml.Node {
	origNode := fg.GetReferenceNode()
	if origNode == nil {
		return utils.CreateRefNode(fg.GetReference())
	}
	return origNode
}

// Render will render the NodeBuilder back to a YAML node, iterating over every NodeEntry defined
func (n *NodeBuilder) Render() *yaml.Node {
	if len(n.Nodes) == 0 {
		return utils.CreateEmptyMapNode()
	}

	// order nodes by line number, retain original order
	m := utils.CreateEmptyMapNode()
	if fg, ok := n.Low.(low.IsReferenced); ok {
		g := reflect.ValueOf(fg)
		if !g.IsNil() {
			if fg.IsReference() && !n.Resolve {
				return n.renderReference(n.Low.(low.IsReferenced))
			}
		}
	}

	sort.Slice(n.Nodes, func(i, j int) bool {
		if n.Nodes[i].Line != n.Nodes[j].Line {
			return n.Nodes[i].Line < n.Nodes[j].Line
		}
		return false
	})

	for i := range n.Nodes {
		node := n.Nodes[i]
		n.AddYAMLNode(m, node)
	}
	return m
}

// encodeSafeValue returns a value safe to pass to (*yaml.Node).Encode. When the
// value is a *yaml.Node, or a slice of them, it returns a deep copy: Encode desolves
// the represented graph in place (Desolve rewrites Tag/Style), and the representer
// aliases input nodes, so encoding a model-owned node would mutate it. With concurrent
// renders (e.g. linters running rules in parallel) that mutation races with readers of
// the same node. Encoding a copy keeps shared nodes immutable.
func encodeSafeValue(value any) any {
	switch v := value.(type) {
	case *yaml.Node:
		return utils.CloneYAMLNode(v)
	case []*yaml.Node:
		cloned := make([]*yaml.Node, len(v))
		for i, n := range v {
			cloned[i] = utils.CloneYAMLNode(n)
		}
		return cloned
	}
	return value
}

// AddYAMLNode will add a new *yaml.Node to the parent node, using the tag, key and value provided.
// If the value is nil, then the node will not be added. This method is recursive, so it will dig down
// into any non-scalar types.
func (n *NodeBuilder) AddYAMLNode(parent *yaml.Node, entry *nodes.NodeEntry) *yaml.Node {
	if entry.Value == nil {
		return parent
	}

	// check the type
	t := reflect.TypeOf(entry.Value)
	var l *yaml.Node
	if entry.Tag != "" {
		l = utils.CreateStringNode(entry.Tag)
		l.Style = entry.KeyStyle
	}

	value := entry.Value
	line := entry.Line

	var nodeErrors []error
	var ne error

	var valueNode *yaml.Node
	switch t.Kind() {
	case reflect.String:
		val := value.(string)
		valueNode = utils.CreateStringNode(val)
		valueNode.Line = line

		if entry.LowValue != nil {
			if vnut, ok := entry.LowValue.(low.HasValueNodeUntyped); ok {
				vn := vnut.GetValueNode()
				if vn != nil {
					valueNode.Style = vn.Style
				}
			}
		}
	case reflect.Bool:
		val := value.(bool)
		if !val {
			valueNode = utils.CreateBoolNode("false")
		} else {
			valueNode = utils.CreateBoolNode("true")
		}
		valueNode.Line = line
	case reflect.Int:
		val := strconv.Itoa(value.(int))
		valueNode = utils.CreateIntNode(val)
		valueNode.Line = line
	case reflect.Int64:
		val := strconv.FormatInt(value.(int64), 10)
		valueNode = utils.CreateIntNode(val)
		valueNode.Line = line
	case reflect.Float32:
		val := strconv.FormatFloat(float64(value.(float32)), 'f', 2, 64)
		valueNode = utils.CreateFloatNode(val)
		valueNode.Line = line
	case reflect.Float64:
		precision := -1
		if entry.StringValue != "" && strings.Contains(entry.StringValue, ".") {
			precision = len(strings.Split(fmt.Sprint(entry.StringValue), ".")[1])
		}
		val := strconv.FormatFloat(value.(float64), 'f', precision, 64)
		if original, ok := originalFloatLexeme(value.(float64), entry.LowValue); ok {
			val = original
		}
		// Always create float node for float64 values, even if they don't contain decimal points
		// This handles cases like negative zero (-0.0) which formats as "-0" but should remain float
		valueNode = utils.CreateFloatNode(val)
		valueNode.Line = line
	case reflect.Slice:
		var rawNode yaml.Node
		m := reflect.ValueOf(value)
		sl := utils.CreateEmptySequenceNode()
		skip := false
		for i := 0; i < m.Len(); i++ {
			// Reset skip at the start of each iteration to handle items without low-level models
			// (e.g., newly created high-level objects appended to an existing slice)
			skip = false
			sqi := m.Index(i).Interface()
			// check if this is a reference.
			if glu, ok := sqi.(GoesLowUntyped); ok {
				if glu != nil {
					ut := glu.GoLowUntyped()
					if ut != nil && !reflect.ValueOf(ut).IsNil() {
						r := ut.(low.IsReferenced)
						if ut != nil && r.GetReference() != "" &&
							ut.(low.IsReferenced).IsReference() {
							if !n.Resolve {
								sl.Content = append(sl.Content, n.renderReference(glu.GoLowUntyped().(low.IsReferenced)))
								skip = true
							}
						}
					}
				}
			}
			if !skip {
				if er, ko := sqi.(Renderable); ko {
					var rend interface{}
					if !n.Resolve {
						rend, ne = er.MarshalYAML()
						nodeErrors = append(nodeErrors, ne)
					} else {
						// try and render inline, if we can, otherwise treat as normal.
						// Prefer a context-aware method when RenderContext is available
						if n.RenderContext != nil {
							if ctxRenderer, ko := er.(RenderableInlineWithContext); ko {
								rend, ne = ctxRenderer.MarshalYAMLInlineWithContext(n.RenderContext)
								nodeErrors = append(nodeErrors, ne)
							} else if inliner, ko := er.(RenderableInline); ko {
								rend, ne = inliner.MarshalYAMLInline()
								nodeErrors = append(nodeErrors, ne)
							} else {
								rend, ne = er.MarshalYAML()
								nodeErrors = append(nodeErrors, ne)
							}
						} else if inliner, ko := er.(RenderableInline); ko {
							rend, ne = inliner.MarshalYAMLInline()
							nodeErrors = append(nodeErrors, ne)
						} else {
							rend, ne = er.MarshalYAML()
							nodeErrors = append(nodeErrors, ne)
						}
					}
					// check if this is a pointer or not.
					if _, ok := rend.(*yaml.Node); ok {
						sl.Content = append(sl.Content, rend.(*yaml.Node))
					}
					if _, ok := rend.(yaml.Node); ok {
						k := rend.(yaml.Node)
						sl.Content = append(sl.Content, &k)
					}
				}
			}
		}

		// a skipped item always leaves its reference in sl, so reaching the encoder means nothing was skipped.
		if len(sl.Content) > 0 {
			valueNode = sl
			break
		}

		if err := encodeValue(&rawNode, value); err != nil {
			// an item that failed to render has already reported why, and the encoder only echoes it.
			if errors.Join(nodeErrors...) == nil {
				nodeErrors = append(nodeErrors, err)
			}
			break
		}
		if entry.LowValue != nil {
			if vnut, ok := entry.LowValue.(low.HasValueNodeUntyped); ok {
				vn := vnut.GetValueNode()
				if vn != nil && vn.Kind == yaml.SequenceNode {
					for i := range vn.Content {
						if len(rawNode.Content) > i {
							rawNode.Content[i].Style = vn.Content[i].Style
						}
					}
				}
			}
		}

		valueNode = &rawNode

	case reflect.Struct:
		if r, ok := value.(low.ValueReference[any]); ok {
			valueNode = r.GetValueNode()
			break
		}
		if r, ok := value.(low.ValueReference[string]); ok {
			valueNode = r.GetValueNode()
			break
		}
		if r, ok := value.(low.NodeReference[string]); ok {
			valueNode = r.GetValueNode()
			break
		}
		return parent

	case reflect.Ptr:
		if m, ok := value.(orderedmap.MapToYamlNoder); ok {
			p := m.ToYamlNode(n, entry.LowValue)
			if p.Content != nil {
				valueNode = p
			}
		} else if r, ok := value.(Renderable); ok {
			if gl, lg := value.(GoesLowUntyped); lg {
				lut := gl.GoLowUntyped()
				if lut != nil {
					lr := lut.(low.IsReferenced)
					ut := reflect.ValueOf(lr)
					if !ut.IsNil() {
						if lr != nil && lr.IsReference() {
							if !n.Resolve {
								valueNode = n.renderReference(lut.(low.IsReferenced))
								break
							}
						}
					}
				}
			}
			var rawRender interface{}
			if !n.Resolve {
				rawRender, ne = r.MarshalYAML()
				nodeErrors = append(nodeErrors, ne)
			} else {
				// try an inline render if we can, otherwise there is no option but to default to the
				// full render. Prefer a context-aware method when RenderContext is available
				if n.RenderContext != nil {
					if ctxRenderer, ko := r.(RenderableInlineWithContext); ko {
						rawRender, ne = ctxRenderer.MarshalYAMLInlineWithContext(n.RenderContext)
						nodeErrors = append(nodeErrors, ne)
					} else if inliner, ko := r.(RenderableInline); ko {
						rawRender, ne = inliner.MarshalYAMLInline()
						nodeErrors = append(nodeErrors, ne)
					} else {
						rawRender, ne = r.MarshalYAML()
						nodeErrors = append(nodeErrors, ne)
					}
				} else if inliner, ko := r.(RenderableInline); ko {
					rawRender, ne = inliner.MarshalYAMLInline()
					nodeErrors = append(nodeErrors, ne)
				} else {
					rawRender, ne = r.MarshalYAML()
					nodeErrors = append(nodeErrors, ne)
				}
			}
			if rawRender != nil {
				if _, ko := rawRender.(*yaml.Node); ko {
					valueNode = rawRender.(*yaml.Node)
				}
				if _, ko := rawRender.(yaml.Node); ko {
					d := rawRender.(yaml.Node)
					valueNode = &d
				}
			}
		} else {

			encodeSkip := false
			// check if the value is a bool, int or float
			if b, bok := value.(*bool); bok {
				encodeSkip = true
				if *b {
					valueNode = utils.CreateBoolNode("true")
					valueNode.Line = line
				} else {
					if entry.RenderZero {
						valueNode = utils.CreateBoolNode("false")
						valueNode.Line = line
					}
				}
			}
			if b, bok := value.(*int64); bok {
				encodeSkip = true
				if *b != 0 || entry.RenderZero {
					valueNode = utils.CreateIntNode(strconv.Itoa(int(*b)))
					valueNode.Line = line
				}
			}
			if b, bok := value.(*float64); bok {
				encodeSkip = true
				if *b != 0 || entry.RenderZero {
					formatFloat := strconv.FormatFloat(*b, 'f', -1, 64)
					if original, ok := originalFloatLexeme(*b, entry.LowValue); ok {
						formatFloat = original
					}

					// Always create float node for float64 values, even if they're whole numbers
					// This handles cases like negative zero (-0.0) and ensures type consistency
					valueNode = utils.CreateFloatNode(formatFloat)

					valueNode.Line = line
				}
			}
			if b, bok := value.(*yaml.Node); bok && b.Kind == yaml.ScalarNode && b.Tag == "!!null" {
				encodeSkip = true
				valueNode = utils.CreateEmptyScalarNode()
				valueNode.Line = line
			}
			if !encodeSkip {
				var rawNode yaml.Node
				if value != nil {
					// check if is a node and it's null
					if v, ko := value.(*yaml.Node); ko {
						if v.Tag == "!!null" {
							return parent
						}
					}

					if err := encodeValue(&rawNode, value); err != nil {
						nodeErrors = append(nodeErrors, err)
					} else {
						valueNode = &rawNode
						valueNode.Line = line
					}
				}
			}
		}

	}
	if nodeErrors != nil && len(nodeErrors) > 0 {
		n.Errors = append(n.Errors, nodeErrors...)
	}
	if valueNode == nil {
		return parent
	}
	if l != nil {
		parent.Content = append(parent.Content, l, valueNode)
	} else {
		parent.Content = valueNode.Content
	}
	return parent
}

// Renderable is an interface that can be implemented by types that provide a custom MarshalYAML method.
type Renderable interface {
	MarshalYAML() (interface{}, error)
}

// RenderableInline is an interface that can be implemented by types that provide a custom MarshalYAML method.
type RenderableInline interface {
	MarshalYAMLInline() (interface{}, error)
}
