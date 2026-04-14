// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"reflect"
	"slices"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/high"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowV2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// PathItem represents a high-level Swagger / OpenAPI 2 PathItem object backed by a low-level one.
//
// Describes the operations available on a single path. A Path Item may be empty, due to ACL constraints.
// The path itself is still exposed to the tooling, but will not know which operations and parameters
// are available.
//   - https://swagger.io/specification/v2/#pathItemObject
type PathItem struct {
	Ref        string
	Get        *Operation
	Put        *Operation
	Post       *Operation
	Delete     *Operation
	Options    *Operation
	Head       *Operation
	Patch      *Operation
	Parameters []*Parameter
	Extensions *orderedmap.Map[string, *yaml.Node]
	low        *lowV2.PathItem
}

// NewPathItem will create a new high-level PathItem from a low-level one. All paths are built out asynchronously.
func NewPathItem(pathItem *lowV2.PathItem) *PathItem {
	p := new(PathItem)
	p.low = pathItem
	p.Extensions = high.ExtractExtensions(pathItem.Extensions)
	if !pathItem.Parameters.IsEmpty() {
		var params []*Parameter
		for k := range pathItem.Parameters.Value {
			params = append(params, NewParameter(pathItem.Parameters.Value[k].Value))
		}
		p.Parameters = params
	}
	buildOperation := func(method string, op *lowV2.Operation) *Operation {
		return NewOperation(op)
	}

	var wg sync.WaitGroup
	if !pathItem.Get.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Get = buildOperation(lowV2.GetLabel, pathItem.Get.Value)
			wg.Done()
		}()
	}
	if !pathItem.Put.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Put = buildOperation(lowV2.PutLabel, pathItem.Put.Value)
			wg.Done()
		}()
	}
	if !pathItem.Post.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Post = buildOperation(lowV2.PostLabel, pathItem.Post.Value)
			wg.Done()
		}()
	}
	if !pathItem.Patch.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Patch = buildOperation(lowV2.PatchLabel, pathItem.Patch.Value)
			wg.Done()
		}()
	}
	if !pathItem.Delete.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Delete = buildOperation(lowV2.DeleteLabel, pathItem.Delete.Value)
			wg.Done()
		}()
	}
	if !pathItem.Head.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Head = buildOperation(lowV2.HeadLabel, pathItem.Head.Value)
			wg.Done()
		}()
	}
	if !pathItem.Options.IsEmpty() {
		wg.Add(1)
		go func() {
			p.Options = buildOperation(lowV2.OptionsLabel, pathItem.Options.Value)
			wg.Done()
		}()
	}
	wg.Wait()
	return p
}

// GoLow returns the low-level PathItem used to create the high-level one.
func (p *PathItem) GoLow() *lowV2.PathItem {
	return p.low
}

func (p *PathItem) GetOperations() *orderedmap.Map[string, *Operation] {
	o := orderedmap.New[string, *Operation]()

	// TODO: this is a bit of a hack, but it works for now. We might just want to actually pull the data out of the document as a map and split it into the individual operations

	type op struct {
		name string
		op   *Operation
		line int
	}

	getLine := func(field string, idx int) int {
		if p.GoLow() == nil {
			return idx
		}

		l, ok := reflect.ValueOf(p.GoLow()).Elem().FieldByName(field).Interface().(low.NodeReference[*lowV2.Operation])
		if !ok || l.GetKeyNode() == nil {
			return idx
		}

		return l.GetKeyNode().Line
	}

	ops := []op{}

	if p.Get != nil {
		ops = append(ops, op{name: lowV2.GetLabel, op: p.Get, line: getLine("Get", -7)})
	}
	if p.Put != nil {
		ops = append(ops, op{name: lowV2.PutLabel, op: p.Put, line: getLine("Put", -6)})
	}
	if p.Post != nil {
		ops = append(ops, op{name: lowV2.PostLabel, op: p.Post, line: getLine("Post", -5)})
	}
	if p.Delete != nil {
		ops = append(ops, op{name: lowV2.DeleteLabel, op: p.Delete, line: getLine("Delete", -4)})
	}
	if p.Options != nil {
		ops = append(ops, op{name: lowV2.OptionsLabel, op: p.Options, line: getLine("Options", -3)})
	}
	if p.Head != nil {
		ops = append(ops, op{name: lowV2.HeadLabel, op: p.Head, line: getLine("Head", -2)})
	}
	if p.Patch != nil {
		ops = append(ops, op{name: lowV2.PatchLabel, op: p.Patch, line: getLine("Patch", -1)})
	}

	slices.SortStableFunc(ops, func(a op, b op) int {
		return a.line - b.line
	})

	for _, op := range ops {
		o.Set(op.name, op.op)
	}

	return o
}
