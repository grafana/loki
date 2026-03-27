// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package high

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/pb33f/libopenapi/datamodel/high/nodes"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
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

	// extract fields from the high level object and add them into our node builder.
	// this will allow us to extract the line numbers from the low level object as well.
	v := reflect.ValueOf(high).Elem()
	num := v.NumField()
	for i := 0; i < num; i++ {
		nb.add(v.Type().Field(i).Name, i)
	}
	return nb
}

func (n *NodeBuilder) add(key string, i int) {
	// only operate on exported fields.
	if unicode.IsLower(rune(key[0])) {
		return
	}

	var (
		lowFieldValue reflect.Value
		lowFieldValid bool
	)

	if n.Low != nil && !reflect.ValueOf(n.Low).IsZero() {
		low := reflect.ValueOf(n.Low)
		if low.Kind() == reflect.Ptr && !low.IsNil() {
			elem := low.Elem()
			if elem.IsValid() {
				field := elem.FieldByName(key)
				if field.IsValid() {
					lowFieldValue = field
					lowFieldValid = true
				}
			}
		} else if low.IsValid() {
			field := low.FieldByName(key)
			if field.IsValid() {
				lowFieldValue = field
				lowFieldValid = true
			}
		}
	}

	// if the key is 'Extensions' then we need to extract the keys from the map
	// and add them to the node builder.
	if key == "Extensions" {
		ev := reflect.ValueOf(n.High).Elem().FieldByName(key).Interface()
		var extensions *orderedmap.Map[string, *yaml.Node]
		if ev != nil {
			extensions = ev.(*orderedmap.Map[string, *yaml.Node])
		}

		var lowExtensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
		if n.Low != nil && !reflect.ValueOf(n.Low).IsZero() {
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

	// find the field with the tag supplied.
	field, _ := reflect.TypeOf(n.High).Elem().FieldByName(key)
	tag := string(field.Tag.Get("yaml"))
	tagName := strings.Split(tag, ",")[0]

	if tag == "-" {
		return
	}

	var renderZeroFlag, omitEmptyFlag bool
	tagParts := strings.Split(tag, ",")
	for _, part := range tagParts {
		if part == renderZero {
			renderZeroFlag = true
		}
		if part == "omitempty" {
			omitEmptyFlag = true
		}
	}

	// extract the value of the field
	fieldValue := reflect.ValueOf(n.High).Elem().FieldByName(key)
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

	if isZero && lowFieldValid {
		var lowInterface any
		if lowFieldValue.Kind() == reflect.Ptr {
			if !lowFieldValue.IsNil() {
				lowInterface = lowFieldValue.Elem().Interface()
			}
		} else {
			lowInterface = lowFieldValue.Interface()
		}
		if lowInterface != nil {
			if emptier, ok := lowInterface.(interface{ IsEmpty() bool }); ok && !emptier.IsEmpty() {
				isZero = false
			} else if nodeGetter, ok := lowInterface.(interface{ GetValueNode() *yaml.Node }); ok {
				if node := nodeGetter.GetValueNode(); node != nil {
					isZero = false
				}
			}
		}
	}

	if !renderZeroFlag && isZero || omitEmptyFlag && isZero {
		return
	}

	// create a new node entry
	nodeEntry := &nodes.NodeEntry{Tag: tagName, Key: key}
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
		nodeEntry.Value = value.String()
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
	if n.Low != nil && !reflect.ValueOf(n.Low).IsZero() {
		if lowFieldValid {
			fLow := lowFieldValue.Interface()
			value = reflect.ValueOf(fLow)

			nodeEntry.LowValue = fLow
			switch value.Kind() {

			case reflect.Slice:
				l := value.Len()
				lines := make([]int, l)
				for g := 0; g < l; g++ {
					qw := value.Index(g).Interface()
					if we, wok := qw.(low.HasKeyNode); wok {
						lines[g] = we.GetKeyNode().Line
					}
				}
				sort.Slice(lines, func(i, j int) bool {
					return lines[i] < lines[j]
				})
				if len(lines) > 0 {
					nodeEntry.Line = lines[0]
				}
			case reflect.Struct:
				y := value.Interface()
				nodeEntry.Line = 9999 + i
				if nb, ok := y.(low.HasValueNodeUntyped); ok {
					if nb.IsReference() {
						if jk, kj := y.(low.HasKeyNode); kj {
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
				nodeEntry.Line = 9999 + i
			}
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

		if len(sl.Content) > 0 {
			valueNode = sl
			break
		}
		if skip {
			break
		}

		err := rawNode.Encode(value)
		if err != nil {
			return parent
		} else {
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
		}

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
			l := entry.LowValue

			if l == nil {
				if gl, ok := value.(GoesLowUntyped); ok && gl.GoLowUntyped() != nil {
					l = gl.GoLowUntyped()
				}
			}

			p := m.ToYamlNode(n, l)
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

					err := rawNode.Encode(value)
					if err != nil {
						return parent
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
