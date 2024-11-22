// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"
)

func OptionsWithTextFormattingConverter(input OptionsWithTextFormatting) string {
	calls := []string{
		`common.NewOptionsWithTextFormattingBuilder()`,
	}
	var buffer strings.Builder
	if input.Text != nil {

		buffer.WriteString(`Text(`)
		arg0 := VizTextDisplayOptionsConverter(*input.Text)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
