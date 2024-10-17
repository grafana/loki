// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func BarConfigConverter(input BarConfig) string {
	calls := []string{
		`common.NewBarConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.BarAlignment != nil {

		buffer.WriteString(`BarAlignment(`)
		arg0 := cog.Dump(*input.BarAlignment)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.BarWidthFactor != nil {

		buffer.WriteString(`BarWidthFactor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.BarWidthFactor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.BarMaxWidth != nil {

		buffer.WriteString(`BarMaxWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.BarMaxWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
