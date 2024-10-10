// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"
)

func OptionsWithLegendConverter(input OptionsWithLegend) string {
	calls := []string{
		`common.NewOptionsWithLegendBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Legend(`)
		arg0 := VizLegendOptionsConverter(input.Legend)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
