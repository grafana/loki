// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"
)

func StackableFieldConfigConverter(input StackableFieldConfig) string {
	calls := []string{
		`common.NewStackableFieldConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.Stacking != nil {

		buffer.WriteString(`Stacking(`)
		arg0 := StackingConfigConverter(*input.Stacking)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
