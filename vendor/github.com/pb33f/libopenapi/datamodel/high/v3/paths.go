// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"fmt"
	"sort"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	v3low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// Paths represents a high-level OpenAPI 3+ Paths object, that is backed by a low-level one.
//
// Holds the relative paths to the individual endpoints and their operations. The path is appended to the URL from the
// Server Object in order to construct the full URL. The Paths MAY be empty, due to Access Control List (ACL)
// constraints.
//   - https://spec.openapis.org/oas/v3.1.0#paths-object
type Paths struct {
	PathItems  *orderedmap.Map[string, *PathItem]  `json:"-" yaml:"-"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *v3low.Paths
}

// NewPaths creates a new high-level instance of Paths from a low-level one.
func NewPaths(paths *v3low.Paths) *Paths {
	p := new(Paths)
	p.low = paths
	p.Extensions = high.ExtractExtensions(paths.Extensions)
	items := orderedmap.New[string, *PathItem]()

	type pathItemResult struct {
		key   string
		value *PathItem
	}

	translateFunc := func(pair orderedmap.Pair[low.KeyReference[string], low.ValueReference[*v3low.PathItem]]) (pathItemResult, error) {
		return pathItemResult{key: pair.Key().Value, value: NewPathItem(pair.Value().Value)}, nil
	}
	resultFunc := func(value pathItemResult) error {
		items.Set(value.key, value.value)
		return nil
	}
	_ = datamodel.TranslateMapParallel[low.KeyReference[string], low.ValueReference[*v3low.PathItem], pathItemResult](
		paths.PathItems, translateFunc, resultFunc,
	)
	p.PathItems = items
	return p
}

// GoLow returns the low-level Paths instance used to create the high-level one.
func (p *Paths) GoLow() *v3low.Paths {
	return p.low
}

// GoLowUntyped will return the low-level Paths instance that was used to create the high-level one, with no type
func (p *Paths) GoLowUntyped() any {
	return p.low
}

// Render will return a YAML representation of the Paths object as a byte slice.
func (p *Paths) Render() ([]byte, error) {
	return yaml.Marshal(p)
}

func (p *Paths) RenderInline() ([]byte, error) {
	d, _ := p.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the Paths object.
func (p *Paths) MarshalYAML() (interface{}, error) {
	// map keys correctly.
	m := utils.CreateEmptyMapNode()
	type pathItem struct {
		pi       *PathItem
		path     string
		line     int
		style    yaml.Style
		rendered *yaml.Node
	}
	var mapped []*pathItem

	for k, pi := range p.PathItems.FromOldest() {
		ln := 9999 // default to a high value to weight new content to the bottom.
		var style yaml.Style
		if p.low != nil {
			lpi := p.low.FindPath(k)
			if lpi != nil {
				ln = lpi.ValueNode.Line
			}

			for lk := range p.low.PathItems.KeysFromOldest() {
				if lk.Value == k {
					style = lk.KeyNode.Style
					break
				}
			}
		}
		mapped = append(mapped, &pathItem{pi, k, ln, style, nil})
	}

	nb := high.NewNodeBuilder(p, p.low)
	extNode := nb.Render()
	if extNode != nil && extNode.Content != nil {
		var label string
		for u := range extNode.Content {
			if u%2 == 0 {
				label = extNode.Content[u].Value
				continue
			}
			mapped = append(mapped, &pathItem{
				nil, label,
				extNode.Content[u].Line, 0, extNode.Content[u],
			})
		}
	}

	sort.Slice(mapped, func(i, j int) bool {
		return mapped[i].line < mapped[j].line
	})
	for _, mp := range mapped {
		if mp.pi != nil {
			rendered, _ := mp.pi.MarshalYAML()

			kn := utils.CreateStringNode(mp.path)
			kn.Style = mp.style

			m.Content = append(m.Content, kn)
			m.Content = append(m.Content, rendered.(*yaml.Node))
		}
		if mp.rendered != nil {
			m.Content = append(m.Content, utils.CreateStringNode(mp.path))
			m.Content = append(m.Content, mp.rendered)
		}
	}

	return m, nil
}

func (p *Paths) MarshalYAMLInline() (interface{}, error) {
	// map keys correctly.
	m := utils.CreateEmptyMapNode()
	type pathItem struct {
		pi       *PathItem
		path     string
		line     int
		style    yaml.Style
		rendered *yaml.Node
	}
	var mapped []*pathItem

	for k, pi := range p.PathItems.FromOldest() {
		ln := 9999 // default to a high value to weight new content to the bottom.
		var style yaml.Style
		if p.low != nil {
			lpi := p.low.FindPath(k)
			if lpi != nil {
				ln = lpi.ValueNode.Line
			}

			for lk := range p.low.PathItems.KeysFromOldest() {
				if lk.Value == k {
					style = lk.KeyNode.Style
					break
				}
			}
		}
		mapped = append(mapped, &pathItem{pi, k, ln, style, nil})
	}

	nb := high.NewNodeBuilder(p, p.low)
	nb.Resolve = true
	extNode := nb.Render()
	if extNode != nil && extNode.Content != nil {
		var label string
		for u := range extNode.Content {
			if u%2 == 0 {
				label = extNode.Content[u].Value
				continue
			}
			mapped = append(mapped, &pathItem{
				nil, label,
				extNode.Content[u].Line, 0, extNode.Content[u],
			})
		}
	}

	sort.Slice(mapped, func(i, j int) bool {
		return mapped[i].line < mapped[j].line
	})
	for _, mp := range mapped {
		if mp.pi != nil {
			rendered, err := mp.pi.MarshalYAMLInline()
			if err != nil {
				return nil, fmt.Errorf("failed to render path '%s' inline: %w", mp.path, err)
			}

			kn := utils.CreateStringNode(mp.path)
			kn.Style = mp.style

			m.Content = append(m.Content, kn)
			m.Content = append(m.Content, rendered.(*yaml.Node))
		}
		if mp.rendered != nil {
			m.Content = append(m.Content, utils.CreateStringNode(mp.path))
			m.Content = append(m.Content, mp.rendered)
		}
	}

	return m, nil
}
