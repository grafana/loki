// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableFieldOptionsConverter(input TableFieldOptions) string {
	calls := []string{
		`common.NewTableFieldOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Width != nil {

		buffer.WriteString(`Width(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Width))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.MinWidth != nil {

		buffer.WriteString(`MinWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MinWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`Align(`)
		arg0 := cog.Dump(input.Align)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.DisplayMode != nil {

		buffer.WriteString(`DisplayMode(`)
		arg0 := cog.Dump(*input.DisplayMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CellOptions != nil {

		buffer.WriteString(`CellOptions(`)
		arg0 := cog.Dump(*input.CellOptions)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Hidden != nil {

		buffer.WriteString(`Hidden(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Hidden))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Inspect != false {

		buffer.WriteString(`Inspect(`)
		arg0 := fmt.Sprintf("%#v", input.Inspect)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Filterable != nil {

		buffer.WriteString(`Filterable(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Filterable))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.HideHeader != nil {

		buffer.WriteString(`HideHeader(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.HideHeader))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
