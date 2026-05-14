// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	v2low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Paths represents a high-level Swagger / OpenAPI Paths object, backed by a low-level one.
type Paths struct {
	PathItems  *orderedmap.Map[string, *PathItem]
	Extensions *orderedmap.Map[string, *yaml.Node]
	low        *v2low.Paths
}

// NewPaths creates a new high-level instance of Paths from a low-level one.
func NewPaths(paths *v2low.Paths) *Paths {
	p := new(Paths)
	p.low = paths
	p.Extensions = high.ExtractExtensions(paths.Extensions)
	pathItems := orderedmap.New[string, *PathItem]()

	translateFunc := func(pair orderedmap.Pair[low.KeyReference[string], low.ValueReference[*v2low.PathItem]]) (asyncResult[*PathItem], error) {
		return asyncResult[*PathItem]{
			key:    pair.Key().Value,
			result: NewPathItem(pair.Value().Value),
		}, nil
	}
	resultFunc := func(result asyncResult[*PathItem]) error {
		pathItems.Set(result.key, result.result)
		return nil
	}
	_ = datamodel.TranslateMapParallel[low.KeyReference[string], low.ValueReference[*v2low.PathItem], asyncResult[*PathItem]](
		paths.PathItems, translateFunc, resultFunc,
	)
	p.PathItems = pathItems
	return p
}

// GoLow returns the low-level Paths instance that backs the high level one.
func (p *Paths) GoLow() *v2low.Paths {
	return p.low
}
