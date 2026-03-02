package errors

import (
	"fmt"

	"github.com/pb33f/libopenapi-validator/helpers"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

func MissingPrefix(schema *base.Schema, prefix string) *ValidationError {
	line := 1
	col := 0
	if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
		line = low.Type.KeyNode.Line
		col = low.Type.KeyNode.Column
	}

	return &ValidationError{
		ValidationType:    helpers.XmlValidation,
		ValidationSubType: helpers.XmlValidationPrefix,
		Message:           fmt.Sprintf("The prefix '%s' is defined in the schema, however it's missing from the xml", prefix),
		Reason:            fmt.Sprintf("The prefix '%s' is defined in the schema, however it's missing from the xml content", prefix),
		SpecLine:          line,
		SpecCol:           col,
		Context:           schema,
		HowToFix:          fmt.Sprintf(HowToFixXmlPrefix, prefix),
	}
}

func InvalidPrefix(schema *base.Schema, prefix string) *ValidationError {
	line := 1
	col := 0
	if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
		line = low.Type.KeyNode.Line
		col = low.Type.KeyNode.Column
	}

	return &ValidationError{
		ValidationType:    helpers.XmlValidation,
		ValidationSubType: helpers.XmlValidationPrefix,
		Message:           fmt.Sprintf("The prefix '%s' defined in the schema differs from the xml", prefix),
		Reason:            fmt.Sprintf("The prefix '%s' is defined in the schema, however the xml sent and invalid prefix", prefix),
		SpecCol:           col,
		SpecLine:          line,
		Context:           schema,
		HowToFix:          fmt.Sprintf(HowToFixXmlPrefix, prefix),
	}
}

func MissingNamespace(schema *base.Schema, namespace string) *ValidationError {
	line := 1
	col := 0
	if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
		line = low.Type.KeyNode.Line
		col = low.Type.KeyNode.Column
	}

	return &ValidationError{
		ValidationType:    helpers.XmlValidation,
		ValidationSubType: helpers.XmlValidationNamespace,
		Message:           fmt.Sprintf("The namespace '%s' is defined in the schema, however it's missing from the xml", namespace),
		Reason:            fmt.Sprintf("The namespace '%s' is defined in the schema, however it's missing from the xml content", namespace),
		SpecLine:          line,
		SpecCol:           col,
		Context:           schema,
		HowToFix:          fmt.Sprintf(HowToFixXmlNamespace, namespace),
	}
}

func InvalidNamespace(schema *base.Schema, namespace, expectedNamespace, prefix string) *ValidationError {
	line := 1
	col := 0
	if low := schema.GoLow(); low != nil && low.Type.KeyNode != nil {
		line = low.Type.KeyNode.Line
		col = low.Type.KeyNode.Column
	}

	return &ValidationError{
		ValidationType:    helpers.XmlValidation,
		ValidationSubType: helpers.XmlValidationNamespace,
		Message:           fmt.Sprintf("The namespace from prefix '%s' differs from the xml", prefix),
		Reason: fmt.Sprintf("The namespace from prefix '%s' is declared as '%s' in the schema, however in xml is declared as '%s'",
			prefix, expectedNamespace, namespace),
		SpecLine: line,
		SpecCol:  col,
		Context:  schema,
		HowToFix: fmt.Sprintf(HowToFixXmlNamespace, namespace),
	}
}

func InvalidXMLParsing(reason, referenceObject string) *ValidationError {
	return &ValidationError{
		ValidationType:    helpers.XmlValidation,
		ValidationSubType: helpers.Schema,
		Message:           "xml example is malformed",
		Reason:            fmt.Sprintf("failed to parse xml: %s", reason),
		SchemaValidationErrors: []*SchemaValidationFailure{{
			Reason:          reason,
			ReferenceSchema: "",
			ReferenceObject: referenceObject,
		}},
		HowToFix: HowToFixInvalidXml,
	}
}
