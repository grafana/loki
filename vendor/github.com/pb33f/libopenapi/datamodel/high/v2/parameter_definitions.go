// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
)

// ParameterDefinitions is a high-level representation of a Swagger / OpenAPI 2 Parameters Definitions object
// that is backed by a low-level one.
//
// ParameterDefinitions holds parameters to be reused across operations. Parameter definitions can be
// referenced to the ones defined here. It does not define global operation parameters
//   - https://swagger.io/specification/v2/#parametersDefinitionsObject
type ParameterDefinitions struct {
	Definitions *orderedmap.Map[string, *Parameter]
	low         *low.ParameterDefinitions
}

// NewParametersDefinitions creates a new instance of a high-level ParameterDefinitions, from a low-level one.
// Every parameter is extracted asynchronously due to the potential depth
func NewParametersDefinitions(parametersDefinitions *low.ParameterDefinitions) *ParameterDefinitions {
	pd := new(ParameterDefinitions)
	pd.low = parametersDefinitions
	params := orderedmap.New[string, *Parameter]()
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.Parameter]]) (asyncResult[*Parameter], error) {
		return asyncResult[*Parameter]{
			key:    pair.Key().Value,
			result: NewParameter(pair.Value().Value),
		}, nil
	}
	resultFunc := func(value asyncResult[*Parameter]) error {
		params.Set(value.key, value.result)
		return nil
	}
	_ = datamodel.TranslateMapParallel(parametersDefinitions.Definitions, translateFunc, resultFunc)
	pd.Definitions = params
	return pd
}

// GoLow returns the low-level ParameterDefinitions instance that backs the low-level one.
func (p *ParameterDefinitions) GoLow() *low.ParameterDefinitions {
	return p.low
}
