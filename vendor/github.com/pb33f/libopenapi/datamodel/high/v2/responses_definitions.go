// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
)

// ResponsesDefinitions is a high-level representation of a Swagger / OpenAPI 2 Responses Definitions object.
// that is backed by a low-level one.
//
// ResponsesDefinitions is an object to hold responses to be reused across operations. Response definitions can be
// referenced to the ones defined here. It does not define global operation responses
//   - https://swagger.io/specification/v2/#responsesDefinitionsObject
type ResponsesDefinitions struct {
	Definitions *orderedmap.Map[string, *Response]
	low         *low.ResponsesDefinitions
}

// NewResponsesDefinitions will create a new high-level instance of ResponsesDefinitions from a low-level one.
func NewResponsesDefinitions(responsesDefinitions *low.ResponsesDefinitions) *ResponsesDefinitions {
	rd := new(ResponsesDefinitions)
	rd.low = responsesDefinitions
	responses := orderedmap.New[string, *Response]()
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.Response]]) (asyncResult[*Response], error) {
		return asyncResult[*Response]{
			key:    pair.Key().Value,
			result: NewResponse(pair.Value().Value),
		}, nil
	}
	resultFunc := func(value asyncResult[*Response]) error {
		responses.Set(value.key, value.result)
		return nil
	}

	_ = datamodel.TranslateMapParallel(responsesDefinitions.Definitions, translateFunc, resultFunc)
	rd.Definitions = responses
	return rd
}

// GoLow returns the low-level ResponsesDefinitions used to create the high-level one.
func (r *ResponsesDefinitions) GoLow() *low.ResponsesDefinitions {
	return r.low
}
