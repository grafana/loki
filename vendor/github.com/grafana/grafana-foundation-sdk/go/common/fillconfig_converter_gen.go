// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func FillConfigConverter(input FillConfig) string {
	calls := []string{
		`common.NewFillConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.FillColor != nil && *input.FillColor != "" {

		buffer.WriteString(`FillColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FillOpacity != nil {

		buffer.WriteString(`FillOpacity(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillOpacity))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FillBelowTo != nil && *input.FillBelowTo != "" {

		buffer.WriteString(`FillBelowTo(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillBelowTo))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
