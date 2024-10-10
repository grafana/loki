// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"
)

func HideSeriesConfigConverter(input HideSeriesConfig) string {
	calls := []string{
		`common.NewHideSeriesConfigBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Tooltip(`)
		arg0 := fmt.Sprintf("%#v", input.Tooltip)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Legend(`)
		arg0 := fmt.Sprintf("%#v", input.Legend)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Viz(`)
		arg0 := fmt.Sprintf("%#v", input.Viz)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
