// Copyright 2025 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package openapi_vocabulary

import (
	"fmt"

	"golang.org/x/text/message"
)

// OpenAPIKeywordError represents an error with an OpenAPI-specific keyword
type OpenAPIKeywordError struct {
	Keyword string
	Message string
}

func (e *OpenAPIKeywordError) Error() string {
	return fmt.Sprintf("OpenAPI keyword '%s': %s", e.Keyword, e.Message)
}

// DiscriminatorPropertyMissingError represents an error when discriminator property is missing
type DiscriminatorPropertyMissingError struct {
	PropertyName string
}

func (e *DiscriminatorPropertyMissingError) KeywordPath() []string {
	return []string{"discriminator"}
}

func (e *DiscriminatorPropertyMissingError) LocalizedString(printer *message.Printer) string {
	return fmt.Sprintf("discriminator property '%s' is missing", e.PropertyName)
}

func (e *DiscriminatorPropertyMissingError) Error() string {
	return fmt.Sprintf("discriminator property '%s' is missing", e.PropertyName)
}

// CoercionError represents an error during scalar type coercion
type CoercionError struct {
	SourceType string
	TargetType string
	Value      string
	Message    string
}

func (e *CoercionError) KeywordPath() []string {
	return []string{"type"}
}

func (e *CoercionError) LocalizedString(printer *message.Printer) string {
	return fmt.Sprintf("cannot coerce %s '%s' to %s: %s", e.SourceType, e.Value, e.TargetType, e.Message)
}

func (e *CoercionError) Error() string {
	return fmt.Sprintf("cannot coerce %s '%s' to %s: %s", e.SourceType, e.Value, e.TargetType, e.Message)
}
