// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package schema_validation

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"

	xj "github.com/basgys/goxml2json"

	liberrors "github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

func (x *xmlValidator) validateXMLWithVersion(schema *base.Schema, xmlString string, log *slog.Logger, version float32) (bool, []*liberrors.ValidationError) {
	var validationErrors []*liberrors.ValidationError

	if schema == nil {
		log.Info("schema is empty and cannot be validated")
		return false, validationErrors
	}

	// parse xml and transform to json structure matching schema
	transformedJSON, err := transformXMLToSchemaJSON(xmlString, schema)
	if err != nil {
		violation := &liberrors.SchemaValidationFailure{
			Reason:          err.Error(),
			Location:        "xml parsing",
			ReferenceSchema: "",
			ReferenceObject: xmlString,
		}
		validationErrors = append(validationErrors, &liberrors.ValidationError{
			ValidationType:         helpers.RequestBodyValidation,
			ValidationSubType:      helpers.Schema,
			Message:                "xml example is malformed",
			Reason:                 fmt.Sprintf("failed to parse xml: %s", err.Error()),
			SchemaValidationErrors: []*liberrors.SchemaValidationFailure{violation},
			HowToFix:               "ensure xml is well-formed and matches schema structure",
		})
		return false, validationErrors
	}

	// validate transformed json against schema using existing validator
	return x.schemaValidator.validateSchemaWithVersion(schema, nil, transformedJSON, log, version)
}

// transformXMLToSchemaJSON converts xml to json structure matching openapi schema.
// applies xml object transformations: name, attribute, wrapped.
func transformXMLToSchemaJSON(xmlString string, schema *base.Schema) (interface{}, error) {
	if xmlString == "" {
		return nil, fmt.Errorf("empty xml content")
	}

	// parse xml using goxml2json with type conversion for numbers only
	// note: we convert floats and ints, but not booleans, since xml content
	// may legitimately contain "true"/"false" as string values
	jsonBuf, err := xj.Convert(strings.NewReader(xmlString), xj.WithTypeConverter(xj.Float, xj.Int))
	if err != nil {
		return nil, fmt.Errorf("malformed xml: %w", err)
	}

	// decode to interface{}
	var rawJSON interface{}
	if err := json.Unmarshal(jsonBuf.Bytes(), &rawJSON); err != nil {
		return nil, fmt.Errorf("failed to decode json: %w", err)
	}

	// apply openapi xml object transformations
	transformed := applyXMLTransformations(rawJSON, schema)
	return transformed, nil
}

// applyXMLTransformations applies openapi xml object rules to match json schema.
// handles xml.name (root unwrapping), xml.attribute (dash prefix), xml.wrapped (array unwrapping).
func applyXMLTransformations(data interface{}, schema *base.Schema) interface{} {
	if schema == nil {
		return data
	}

	// unwrap root element if xml.name is set on schema
	if schema.XML != nil && schema.XML.Name != "" {
		if dataMap, ok := data.(map[string]interface{}); ok {
			if wrapped, exists := dataMap[schema.XML.Name]; exists {
				data = wrapped
			}
		}
	}

	// transform properties based on their xml configurations
	if dataMap, ok := data.(map[string]interface{}); ok {
		if schema.Properties == nil || schema.Properties.Len() == 0 {
			return data
		}

		transformed := make(map[string]interface{}, schema.Properties.Len())

		for pair := schema.Properties.First(); pair != nil; pair = pair.Next() {
			propName := pair.Key()
			propSchemaProxy := pair.Value()
			propSchema := propSchemaProxy.Schema()
			if propSchema == nil {
				continue
			}

			// determine xml element name (defaults to property name)
			xmlName := propName
			if propSchema.XML != nil && propSchema.XML.Name != "" {
				xmlName = propSchema.XML.Name
			}

			// handle xml.attribute: true - attributes are prefixed with dash
			if propSchema.XML != nil && propSchema.XML.Attribute {
				attrKey := "-" + xmlName
				if val, exists := dataMap[attrKey]; exists {
					transformed[propName] = val
					continue
				}
			}

			// handle regular elements
			if val, exists := dataMap[xmlName]; exists {
				// handle wrapped arrays: unwrap container element
				if len(propSchema.Type) > 0 && propSchema.Type[0] == "array" &&
					propSchema.XML != nil && propSchema.XML.Wrapped {
					val = unwrapArrayElement(val, propSchema)
				}

				transformed[propName] = val
			}
		}

		return transformed
	}

	return data
}

// unwrapArrayElement removes wrapping element from xml arrays when xml.wrapped is true.
// example: {"items": {"item": [...]}} becomes [...]
func unwrapArrayElement(val interface{}, propSchema *base.Schema) interface{} {
	wrapMap, ok := val.(map[string]interface{})
	if !ok {
		return val
	}

	// determine item element name
	itemName := "item"
	if propSchema.Items != nil && propSchema.Items.A != nil {
		itemSchema := propSchema.Items.A.Schema()
		if itemSchema != nil && itemSchema.XML != nil && itemSchema.XML.Name != "" {
			itemName = itemSchema.XML.Name
		}
	}

	// unwrap: look for item element inside wrapper
	if unwrapped, exists := wrapMap[itemName]; exists {
		return unwrapped
	}

	return val
}

// IsXMLContentType checks if a media type string represents xml content.
func IsXMLContentType(mediaType string) bool {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	return strings.HasPrefix(mt, "application/xml") ||
		strings.HasPrefix(mt, "text/xml") ||
		strings.HasSuffix(mt, "+xml")
}
