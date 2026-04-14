// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package what_changed
//
// what changed is a feature that performs an accurate and deep analysis of what has changed between two OpenAPI
// documents. The report generated outlines every single change made between two specifications (left and right)
// rendered in the document hierarchy, so exploring it is the same as exploring the document model.
//
// There are two main functions, one of generating a report for Swagger documents (OpenAPI 2)
// And OpenAPI 3+ documents.
//
// This package uses a combined model for OpenAPI and Swagger changes, it does not break them out into separate
// versions like the datamodel package. The reason for this is to prevent sprawl across versions and to provide
// a single API and model for any application that wants to use this feature.
package what_changed

import (
	"github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/what-changed/model"
)

// CompareOpenAPIDocuments will compare left (original) and right (updated) OpenAPI 3+ documents and extract every change
// made across the entire specification. The report outlines every property changed, everything that was added,
// or removed and which of those changes were breaking.
func CompareOpenAPIDocuments(original, updated *v3.Document) *model.DocumentChanges {
	return model.CompareDocuments(original, updated)
}

// CompareSwaggerDocuments will compare left (original) and a right (updated) Swagger documents and extract every change
// made across the entire specification. The report outlines every property changes, everything that was added,
// or removed and which of those changes were breaking.
func CompareSwaggerDocuments(original, updated *v2.Swagger) *model.DocumentChanges {
	return model.CompareDocuments(original, updated)
}
