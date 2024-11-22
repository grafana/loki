// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"
)

func OptionsWithTooltipConverter(input OptionsWithTooltip) string {
	calls := []string{
		`common.NewOptionsWithTooltipBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Tooltip(`)
		arg0 := VizTooltipOptionsConverter(input.Tooltip)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
