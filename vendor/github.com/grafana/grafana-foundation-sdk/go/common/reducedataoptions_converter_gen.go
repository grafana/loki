// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func ReduceDataOptionsConverter(input ReduceDataOptions) string {
	calls := []string{
		`common.NewReduceDataOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Values != nil {

		buffer.WriteString(`Values(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Values))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Limit != nil {

		buffer.WriteString(`Limit(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Limit))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Calcs != nil && len(input.Calcs) >= 1 {

		buffer.WriteString(`Calcs(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Calcs {
			tmpcalcsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpcalcsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Fields != nil && *input.Fields != "" {

		buffer.WriteString(`Fields(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Fields))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
