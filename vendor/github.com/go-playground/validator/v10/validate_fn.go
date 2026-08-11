//go:build validator_novalidatefn

package validator

func isValidateFn(fl FieldLevel) bool {
	panic("validateFn is not supported with 'no-validate-fn' tag")
}
