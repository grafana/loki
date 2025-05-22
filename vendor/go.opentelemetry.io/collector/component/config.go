// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

import (
	"reflect"

	"go.uber.org/multierr"
)

// Config defines the configuration for a component.Component.
//
// Implementations and/or any sub-configs (other types embedded or included in the Config implementation)
// MUST implement the ConfigValidator if any validation is required for that part of the configuration
// (e.g. check if a required field is present).
//
// A valid implementation MUST pass the check componenttest.CheckConfigStruct (return nil error).
type Config any

// As interface types are only used for static typing, a common idiom to find the reflection Type
// for an interface type Foo is to use a *Foo value.
var configValidatorType = reflect.TypeOf((*ConfigValidator)(nil)).Elem()

// ConfigValidator defines an optional interface for configurations to implement to do validation.
type ConfigValidator interface {
	// Validate the configuration and returns an error if invalid.
	Validate() error
}

// ValidateConfig validates a config, by doing this:
//   - Call Validate on the config itself if the config implements ConfigValidator.
func ValidateConfig(cfg Config) error {
	return validate(reflect.ValueOf(cfg))
}

func validate(v reflect.Value) error {
	// Validate the value itself.
	switch v.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Ptr:
		return validate(v.Elem())
	case reflect.Struct:
		var errs error
		errs = multierr.Append(errs, callValidateIfPossible(v))
		// Reflect on the pointed data and check each of its fields.
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			errs = multierr.Append(errs, validate(v.Field(i)))
		}
		return errs
	case reflect.Slice, reflect.Array:
		var errs error
		errs = multierr.Append(errs, callValidateIfPossible(v))
		// Reflect on the pointed data and check each of its fields.
		for i := 0; i < v.Len(); i++ {
			errs = multierr.Append(errs, validate(v.Index(i)))
		}
		return errs
	case reflect.Map:
		var errs error
		errs = multierr.Append(errs, callValidateIfPossible(v))
		iter := v.MapRange()
		for iter.Next() {
			errs = multierr.Append(errs, validate(iter.Key()))
			errs = multierr.Append(errs, validate(iter.Value()))
		}
		return errs
	default:
		return callValidateIfPossible(v)
	}
}

func callValidateIfPossible(v reflect.Value) error {
	// If the value type implements ConfigValidator just call Validate
	if v.Type().Implements(configValidatorType) {
		return v.Interface().(ConfigValidator).Validate()
	}

	// If the pointer type implements ConfigValidator call Validate on the pointer to the current value.
	if reflect.PointerTo(v.Type()).Implements(configValidatorType) {
		// If not addressable, then create a new *V pointer and set the value to current v.
		if !v.CanAddr() {
			pv := reflect.New(reflect.PointerTo(v.Type()).Elem())
			pv.Elem().Set(v)
			v = pv.Elem()
		}
		return v.Addr().Interface().(ConfigValidator).Validate()
	}

	return nil
}
