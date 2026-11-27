//go:build !validator_novalidatefn

package validator

import (
	"cmp"
	"fmt"
	"reflect"
)

func isValidateFn(fl FieldLevel) bool {
	const defaultParam = `Validate`

	field := fl.Field()
	validateFn := cmp.Or(fl.Param(), defaultParam)

	ok, err := tryCallValidateFn(field, validateFn)
	if err != nil {
		return false
	}

	return ok
}

func tryCallValidateFn(field reflect.Value, validateFn string) (bool, error) {
	method := field.MethodByName(validateFn)
	if field.CanAddr() && !method.IsValid() {
		method = field.Addr().MethodByName(validateFn)
	}

	if !method.IsValid() {
		return false, fmt.Errorf("unable to call %q on type %q: %w",
			validateFn, field.Type().String(), errMethodNotFound)
	}

	returnValues := method.Call([]reflect.Value{})
	if len(returnValues) == 0 {
		return false, fmt.Errorf("unable to use result of method %q on type %q: %w",
			validateFn, field.Type().String(), errMethodReturnNoValues)
	}

	firstReturnValue := returnValues[0]

	switch firstReturnValue.Kind() {
	case reflect.Bool:
		return firstReturnValue.Bool(), nil
	case reflect.Interface:
		errorType := reflect.TypeOf((*error)(nil)).Elem()

		if firstReturnValue.Type().Implements(errorType) {
			return firstReturnValue.IsNil(), nil
		}

		return false, fmt.Errorf("unable to use result of method %q on type %q: %w (got interface %v expect error)",
			validateFn, field.Type().String(), errMethodReturnInvalidType, firstReturnValue.Type().String())
	default:
		return false, fmt.Errorf("unable to use result of method %q on type %q: %w (got %v expect error or bool)",
			validateFn, field.Type().String(), errMethodReturnInvalidType, firstReturnValue.Type().String())
	}
}
