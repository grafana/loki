// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Responses is a high-level representation of a Swagger / OpenAPI 2 Responses object, backed by a low level one.
type Responses struct {
	Codes      *orderedmap.Map[string, *Response]
	Default    *Response
	Extensions *orderedmap.Map[string, *yaml.Node]
	low        *low.Responses
}

// NewResponses will create a new high-level instance of Responses from a low-level one.
func NewResponses(responses *low.Responses) *Responses {
	r := new(Responses)
	r.low = responses
	r.Extensions = high.ExtractExtensions(responses.Extensions)

	if !responses.Default.IsEmpty() {
		r.Default = NewResponse(responses.Default.Value)
	}

	if orderedmap.Len(responses.Codes) > 0 {
		resp := orderedmap.New[string, *Response]()
		translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.Response]]) (asyncResult[*Response], error) {
			return asyncResult[*Response]{
				key:    pair.Key().Value,
				result: NewResponse(pair.Value().Value),
			}, nil
		}
		resultFunc := func(value asyncResult[*Response]) error {
			resp.Set(value.key, value.result)
			return nil
		}
		_ = datamodel.TranslateMapParallel(responses.Codes, translateFunc, resultFunc)
		r.Codes = resp
	}

	return r
}

// GoLow will return the low-level object used to create the high-level one.
func (r *Responses) GoLow() *low.Responses {
	return r.low
}
