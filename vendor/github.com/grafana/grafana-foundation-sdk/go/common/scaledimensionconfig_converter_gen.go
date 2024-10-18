// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func ScaleDimensionConfigConverter(input ScaleDimensionConfig) string {
	calls := []string{
		`common.NewScaleDimensionConfigBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Min(`)
		arg0 := fmt.Sprintf("%#v", input.Min)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Max(`)
		arg0 := fmt.Sprintf("%#v", input.Max)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Fixed != nil {

		buffer.WriteString(`Fixed(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Fixed))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Field != nil && *input.Field != "" {

		buffer.WriteString(`Field(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Field))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Mode != nil {

		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(*input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
