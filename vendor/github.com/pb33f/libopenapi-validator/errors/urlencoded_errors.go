package errors

import (
	"fmt"

	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

func InvalidURLEncodedParsing(reason, referenceObject string) *ValidationError {
	return &ValidationError{
		ValidationType:    helpers.URLEncodedValidation,
		ValidationSubType: helpers.Schema,
		Message:           "Unable to parse form-urlencoded body",
		Reason:            fmt.Sprintf("failed to parse form-urlencoded: %s", reason),
		SchemaValidationErrors: []*SchemaValidationFailure{{
			Reason:          reason,
			ReferenceSchema: "",
			ReferenceObject: referenceObject,
		}},
		HowToFix: HowToFixInvalidUrlEncoded,
	}
}

func InvalidTypeEncoding(schema *base.Schema, name, contentType string) *ValidationError {
	line := 1
	col := 0
	if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
		line = low.Type.KeyNode.Line
		col = low.Type.KeyNode.Column
	}

	return &ValidationError{
		ValidationType:    helpers.URLEncodedValidation,
		ValidationSubType: helpers.InvalidTypeEncoding,
		Message:           fmt.Sprintf("The value '%s' could not be parsed to the defined encoding", name),
		Reason:            fmt.Sprintf("The value '%s' is encoded as '%s' in the schema, however the value could not be parsed", name, contentType),
		SpecLine:          line,
		SpecCol:           col,
		Context:           schema,
		HowToFix:          HowToFixInvalidTypeEncoding,
	}
}

func ReservedURLEncodedValue(schema *base.Schema, name, value string) *ValidationError {
	line := 1
	col := 0
	if schema != nil {
		if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
			line = low.Type.KeyNode.Line
			col = low.Type.KeyNode.Column
		}
	}

	return &ValidationError{
		ValidationType:    helpers.URLEncodedValidation,
		ValidationSubType: helpers.ReservedValues,
		Message:           fmt.Sprintf("Form value '%s' contains reserved characters", name),
		Reason:            fmt.Sprintf("The form value '%s' contains reserved characters but allowReserved is false. Value: '%s'", name, value),
		SpecLine:          line,
		SpecCol:           col,
		Context:           schema,
		HowToFix:          HowToFixFormDataReservedCharacters,
	}
}
