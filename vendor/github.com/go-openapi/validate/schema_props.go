// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validate

import (
	"fmt"
	"reflect"

	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
)

type schemaPropsValidator struct {
	Path            string
	In              string
	AllOf           []spec.Schema
	OneOf           []spec.Schema
	AnyOf           []spec.Schema
	Not             *spec.Schema
	Dependencies    spec.Dependencies
	anyOfValidators []*SchemaValidator
	allOfValidators []*SchemaValidator
	oneOfValidators []*SchemaValidator
	notValidator    *SchemaValidator
	Root            interface{}
	KnownFormats    strfmt.Registry
	Options         *SchemaValidatorOptions
}

func (s *schemaPropsValidator) SetPath(path string) {
	s.Path = path
}

func newSchemaPropsValidator(
	path string, in string, allOf, oneOf, anyOf []spec.Schema, not *spec.Schema, deps spec.Dependencies, root interface{}, formats strfmt.Registry,
	opts *SchemaValidatorOptions) *schemaPropsValidator {
	if opts == nil {
		opts = new(SchemaValidatorOptions)
	}

	anyValidators := make([]*SchemaValidator, 0, len(anyOf))
	for i := range anyOf {
		anyValidators = append(anyValidators, newSchemaValidator(&anyOf[i], root, path, formats, opts))
	}
	allValidators := make([]*SchemaValidator, 0, len(allOf))
	for i := range allOf {
		allValidators = append(allValidators, newSchemaValidator(&allOf[i], root, path, formats, opts))
	}
	oneValidators := make([]*SchemaValidator, 0, len(oneOf))
	for i := range oneOf {
		oneValidators = append(oneValidators, newSchemaValidator(&oneOf[i], root, path, formats, opts))
	}

	var notValidator *SchemaValidator
	if not != nil {
		notValidator = newSchemaValidator(not, root, path, formats, opts)
	}

	var s *schemaPropsValidator
	if opts.recycleValidators {
		s = poolOfSchemaPropsValidators.BorrowValidator()
	} else {
		s = new(schemaPropsValidator)
	}

	s.Path = path
	s.In = in
	s.AllOf = allOf
	s.OneOf = oneOf
	s.AnyOf = anyOf
	s.Not = not
	s.Dependencies = deps
	s.anyOfValidators = anyValidators
	s.allOfValidators = allValidators
	s.oneOfValidators = oneValidators
	s.notValidator = notValidator
	s.Root = root
	s.KnownFormats = formats
	s.Options = opts

	return s
}

func (s *schemaPropsValidator) Applies(source interface{}, _ reflect.Kind) bool {
	_, isSchema := source.(*spec.Schema)
	return isSchema
}

func (s *schemaPropsValidator) Validate(data interface{}) *Result {
	var mainResult *Result
	if s.Options.recycleValidators {
		mainResult = poolOfResults.BorrowResult()
	} else {
		mainResult = new(Result)
	}

	// Intermediary error results

	// IMPORTANT! messages from underlying validators
	keepResultAnyOf := poolOfResults.BorrowResult()
	keepResultOneOf := poolOfResults.BorrowResult()
	keepResultAllOf := poolOfResults.BorrowResult()

	if s.Options.recycleValidators {
		defer func() {
			s.redeem()

			// results are redeemed when merged
		}()
	}

	// Validates at least one in anyOf schemas
	var firstSuccess *Result
	if len(s.anyOfValidators) > 0 {
		var bestFailures *Result
		succeededOnce := false
		for _, anyOfSchema := range s.anyOfValidators {
			result := anyOfSchema.Validate(data)
			// We keep inner IMPORTANT! errors no matter what MatchCount tells us
			keepResultAnyOf.Merge(result.keepRelevantErrors())
			if result.IsValid() {
				bestFailures = nil
				succeededOnce = true
				firstSuccess = result
				_ = keepResultAnyOf.cleared()

				break
			}
			// MatchCount is used to select errors from the schema with most positive checks
			if bestFailures == nil || result.MatchCount > bestFailures.MatchCount {
				bestFailures = result
			}
		}

		if !succeededOnce {
			mainResult.AddErrors(mustValidateAtLeastOneSchemaMsg(s.Path))
		}
		if bestFailures != nil {
			mainResult.Merge(bestFailures)
			if firstSuccess != nil && firstSuccess.wantsRedeemOnMerge {
				poolOfResults.RedeemResult(firstSuccess)
			}
		} else if firstSuccess != nil {
			mainResult.Merge(firstSuccess)
		}
	}

	// Validates exactly one in oneOf schemas
	if len(s.oneOfValidators) > 0 {
		var bestFailures *Result
		var firstSuccess *Result
		validated := 0

		for _, oneOfSchema := range s.oneOfValidators {
			result := oneOfSchema.Validate(data)
			// We keep inner IMPORTANT! errors no matter what MatchCount tells us
			keepResultOneOf.Merge(result.keepRelevantErrors())
			if result.IsValid() {
				validated++
				bestFailures = nil
				if firstSuccess == nil {
					firstSuccess = result
				}
				_ = keepResultOneOf.cleared()
				continue
			}
			// MatchCount is used to select errors from the schema with most positive checks
			if validated == 0 && (bestFailures == nil || result.MatchCount > bestFailures.MatchCount) {
				bestFailures = result
			}
		}

		if validated != 1 {
			var additionalMsg string
			if validated == 0 {
				additionalMsg = "Found none valid"
			} else {
				additionalMsg = fmt.Sprintf("Found %d valid alternatives", validated)
			}

			mainResult.AddErrors(mustValidateOnlyOneSchemaMsg(s.Path, additionalMsg))
			if bestFailures != nil {
				mainResult.Merge(bestFailures)
			}
			if firstSuccess != nil && firstSuccess.wantsRedeemOnMerge {
				poolOfResults.RedeemResult(firstSuccess)
			}
		} else if firstSuccess != nil {
			mainResult.Merge(firstSuccess)
			if bestFailures != nil && bestFailures.wantsRedeemOnMerge {
				poolOfResults.RedeemResult(bestFailures)
			}
		}
	}

	// Validates all of allOf schemas
	if len(s.allOfValidators) > 0 {
		validated := 0

		for _, allOfSchema := range s.allOfValidators {
			result := allOfSchema.Validate(data)
			// We keep inner IMPORTANT! errors no matter what MatchCount tells us
			keepResultAllOf.Merge(result.keepRelevantErrors())
			if result.IsValid() {
				validated++
			}
			mainResult.Merge(result)
		}

		if validated != len(s.allOfValidators) {
			additionalMsg := ""
			if validated == 0 {
				additionalMsg = ". None validated"
			}

			mainResult.AddErrors(mustValidateAllSchemasMsg(s.Path, additionalMsg))
		}
	}

	if s.notValidator != nil {
		result := s.notValidator.Validate(data)
		// We keep inner IMPORTANT! errors no matter what MatchCount tells us
		if result.IsValid() {
			mainResult.AddErrors(mustNotValidatechemaMsg(s.Path))
		}
	}

	if s.Dependencies != nil && len(s.Dependencies) > 0 && reflect.TypeOf(data).Kind() == reflect.Map {
		val := data.(map[string]interface{})
		for key := range val {
			if dep, ok := s.Dependencies[key]; ok {

				if dep.Schema != nil {
					mainResult.Merge(
						newSchemaValidator(dep.Schema, s.Root, s.Path+"."+key, s.KnownFormats, s.Options).Validate(data),
					)
					continue
				}

				if len(dep.Property) > 0 {
					for _, depKey := range dep.Property {
						if _, ok := val[depKey]; !ok {
							mainResult.AddErrors(hasADependencyMsg(s.Path, depKey))
						}
					}
				}
			}
		}
	}

	mainResult.Inc()
	// In the end we retain best failures for schema validation
	// plus, if any, composite errors which may explain special cases (tagged as IMPORTANT!).
	return mainResult.Merge(keepResultAllOf, keepResultOneOf, keepResultAnyOf)
}

func (s *schemaPropsValidator) redeem() {
	poolOfSchemaPropsValidators.RedeemValidator(s)
}

func (s *schemaPropsValidator) redeemChildren() {
	for _, v := range s.anyOfValidators {
		v.redeemChildren()
		v.redeem()
	}
	s.anyOfValidators = nil
	for _, v := range s.allOfValidators {
		v.redeemChildren()
		v.redeem()
	}
	s.allOfValidators = nil
	for _, v := range s.oneOfValidators {
		v.redeemChildren()
		v.redeem()
	}
	s.oneOfValidators = nil

	if s.notValidator != nil {
		s.notValidator.redeemChildren()
		s.notValidator.redeem()
		s.notValidator = nil
	}
}
