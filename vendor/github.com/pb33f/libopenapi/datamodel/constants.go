// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package datamodel contains two sets of models, high and low.
//
// The low-level (or plumbing) models are designed to capture every single detail about specification, including
// all lines, columns, positions, tags, comments and essentially everything you would ever want to know.
// Positions of every key, value and meta-data that is lost when blindly un-marshaling JSON/YAML into a struct.
//
// The high model (porcelain) is a much simpler representation of the low model, keys are simple strings and indices
// are numbers. When developing consumers of the model, the high model is really what you want to use instead of the
// low model, it's much easier to navigate and is designed for easy consumption.
//
// The high model requires the low model to be built. Every high model has a 'GoLow' method that allows the consumer
// to 'drop down' from the porcelain API to the plumbing API, which gives instant access to everything low.
package datamodel

import (
	_ "embed"
)

// Constants used by utilities to determine the version of OpenAPI that we're referring to.
const (
	// OAS2 represents Swagger Documents
	OAS2 = "oas2"

	// OAS3 represents OpenAPI 3.0+ Documents
	OAS3 = "oas3"

	// OAS31 represents OpenAPI 3.1 Documents
	OAS31 = "oas3_1"

	// OAS32 represents OpenAPI 3.2+ Documents
	OAS32 = "oas3_2"
)

// OpenAPI3SchemaData is an embedded version of the OpenAPI 3 Schema
//
//go:embed schemas/oas3-schema.json
var OpenAPI3SchemaData string // embedded OAS3 schema

// OpenAPI31SchemaData is an embedded version of the OpenAPI 3.1 Schema
//
//go:embed schemas/oas31-schema.json
var OpenAPI31SchemaData string // embedded OAS31 schema

//go:embed schemas/oas32-schema.json
var OpenAPI32SchemaData string // embedded OAS32 schema

// OpenAPI2SchemaData is an embedded version of the OpenAPI 2 (Swagger) Schema
//
//go:embed schemas/swagger2-schema.json
var OpenAPI2SchemaData string // embedded OAS3 schema

// OAS3_1Format defines documents that can only be version 3.1
var OAS3_1Format = []string{OAS31}

var OAS3_2Format = []string{OAS32}

// OAS3Format defines documents that can only be version 3.0
var OAS3Format = []string{OAS3}

// OAS3AllFormat defines documents that compose all 3+ versions
var OAS3AllFormat = []string{OAS3, OAS31, OAS32}

// OAS2Format defines documents that compose swagger documents (version 2.0)
var OAS2Format = []string{OAS2}

// AllFormats defines all versions of OpenAPI
var AllFormats = []string{OAS3, OAS31, OAS32, OAS2}
