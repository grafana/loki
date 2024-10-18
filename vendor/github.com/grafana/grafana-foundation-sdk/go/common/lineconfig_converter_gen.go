// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func LineConfigConverter(input LineConfig) string {
	calls := []string{
		`common.NewLineConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.LineColor != nil && *input.LineColor != "" {

		buffer.WriteString(`LineColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LineColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineWidth != nil {

		buffer.WriteString(`LineWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LineWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineInterpolation != nil {

		buffer.WriteString(`LineInterpolation(`)
		arg0 := cog.Dump(*input.LineInterpolation)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineStyle != nil {

		buffer.WriteString(`LineStyle(`)
		arg0 := LineStyleConverter(*input.LineStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.SpanNulls != nil {

		buffer.WriteString(`SpanNulls(`)
		arg0 := cog.Dump(*input.SpanNulls)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
