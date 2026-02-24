// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"github.com/pb33f/libopenapi/datamodel"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/orderedmap"
)

// SecurityDefinitions is a high-level representation of a Swagger / OpenAPI 2 Security Definitions object, that
// is backed by a low-level one.
//
// A declaration of the security schemes available to be used in the specification. This does not enforce the security
// schemes on the operations and only serves to provide the relevant details for each scheme
//   - https://swagger.io/specification/v2/#securityDefinitionsObject
type SecurityDefinitions struct {
	Definitions *orderedmap.Map[string, *SecurityScheme]
	low         *low.SecurityDefinitions
}

// NewSecurityDefinitions creates a new high-level instance of a SecurityDefinitions from a low-level one.
func NewSecurityDefinitions(definitions *low.SecurityDefinitions) *SecurityDefinitions {
	sd := new(SecurityDefinitions)
	sd.low = definitions
	schemes := orderedmap.New[string, *SecurityScheme]()
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*low.SecurityScheme]]) (asyncResult[*SecurityScheme], error) {
		return asyncResult[*SecurityScheme]{
			key:    pair.Key().Value,
			result: NewSecurityScheme(pair.Value().Value),
		}, nil
	}
	resultFunc := func(value asyncResult[*SecurityScheme]) error {
		schemes.Set(value.key, value.result)
		return nil
	}
	_ = datamodel.TranslateMapParallel(definitions.Definitions, translateFunc, resultFunc)

	sd.Definitions = schemes
	return sd
}

// GoLow returns the low-level SecurityDefinitions instance used to create the high-level one.
func (sd *SecurityDefinitions) GoLow() *low.SecurityDefinitions {
	return sd.low
}
