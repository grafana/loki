// Copyright (c) 2013 Mitchell Hashimoto
// SPDX-License-Identifier: MIT
// This code is a modified version of https://github.com/go-viper/mapstructure

package composehook // import "go.opentelemetry.io/collector/confmap/internal/third_party/composehook"

import (
	"errors"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
)

// typedDecodeHook takes a raw DecodeHookFunc (an any) and turns
// it into the proper DecodeHookFunc type, such as DecodeHookFuncType.
func typedDecodeHook(h mapstructure.DecodeHookFunc) mapstructure.DecodeHookFunc {
	// Create variables here so we can reference them with the reflect pkg
	var f1 mapstructure.DecodeHookFuncType
	var f2 mapstructure.DecodeHookFuncKind
	var f3 mapstructure.DecodeHookFuncValue

	// Fill in the variables into this interface and the rest is done
	// automatically using the reflect package.
	potential := []any{f3, f1, f2}

	v := reflect.ValueOf(h)
	vt := v.Type()
	for _, raw := range potential {
		pt := reflect.ValueOf(raw).Type()
		if vt.ConvertibleTo(pt) {
			return v.Convert(pt).Interface()
		}
	}

	return nil
}

// cachedDecodeHook takes a raw DecodeHookFunc (an any) and turns
// it into a closure to be used directly
// if the type fails to convert we return a closure always erroring to keep the previous behavior
func cachedDecodeHook(raw mapstructure.DecodeHookFunc) func(reflect.Value, reflect.Value) (any, error) {
	switch f := typedDecodeHook(raw).(type) {
	case mapstructure.DecodeHookFuncType:
		return func(from reflect.Value, to reflect.Value) (any, error) {
			// CHANGE FROM UPSTREAM: check if from is valid and return nil if not
			if !from.IsValid() {
				return nil, nil
			}
			return f(from.Type(), to.Type(), from.Interface())
		}
	case mapstructure.DecodeHookFuncKind:
		return func(from reflect.Value, to reflect.Value) (any, error) {
			// CHANGE FROM UPSTREAM: check if from is valid and return nil if not
			if !from.IsValid() {
				return nil, nil
			}
			return f(from.Kind(), to.Kind(), from.Interface())
		}
	case mapstructure.DecodeHookFuncValue:
		return func(from reflect.Value, to reflect.Value) (any, error) {
			return f(from, to)
		}
	default:
		return func(reflect.Value, reflect.Value) (any, error) {
			return nil, errors.New("invalid decode hook signature")
		}
	}
}

// ComposeDecodeHookFunc creates a single DecodeHookFunc that
// automatically composes multiple DecodeHookFuncs.
//
// The composed funcs are called in order, with the result of the
// previous transformation.
//
// This is a copy of [mapstructure.ComposeDecodeHookFunc] but with
// validation added.
func ComposeDecodeHookFunc(fs ...mapstructure.DecodeHookFunc) mapstructure.DecodeHookFunc {
	cached := make([]func(reflect.Value, reflect.Value) (any, error), 0, len(fs))
	for _, f := range fs {
		cached = append(cached, cachedDecodeHook(f))
	}
	return func(f reflect.Value, t reflect.Value) (any, error) {
		var err error

		// CHANGE FROM UPSTREAM: check if f is valid before calling f.Interface()
		var data any
		if f.IsValid() {
			data = f.Interface()
		}

		newFrom := f
		for _, c := range cached {
			data, err = c(newFrom, t)
			if err != nil {
				return nil, err
			}
			newFrom = reflect.ValueOf(data)
		}

		return data, nil
	}
}
