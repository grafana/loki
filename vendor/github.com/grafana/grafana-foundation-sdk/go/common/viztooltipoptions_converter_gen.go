// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func VizTooltipOptionsConverter(input VizTooltipOptions) string {
	calls := []string{
		`common.NewVizTooltipOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Sort(`)
		arg0 := cog.Dump(input.Sort)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.MaxWidth != nil {

		buffer.WriteString(`MaxWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MaxWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.MaxHeight != nil {

		buffer.WriteString(`MaxHeight(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MaxHeight))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
