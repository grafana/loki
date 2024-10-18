// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func SingleStatBaseOptionsConverter(input SingleStatBaseOptions) string {
	calls := []string{
		`common.NewSingleStatBaseOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`ReduceOptions(`)
		arg0 := ReduceDataOptionsConverter(input.ReduceOptions)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Text != nil {

		buffer.WriteString(`Text(`)
		arg0 := VizTextDisplayOptionsConverter(*input.Text)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`Orientation(`)
		arg0 := cog.Dump(input.Orientation)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
