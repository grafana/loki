// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func LineStyleConverter(input LineStyle) string {
	calls := []string{
		`common.NewLineStyleBuilder()`,
	}
	var buffer strings.Builder
	if input.Fill != nil {

		buffer.WriteString(`Fill(`)
		arg0 := cog.Dump(*input.Fill)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Dash != nil && len(input.Dash) >= 1 {

		buffer.WriteString(`Dash(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Dash {
			tmpdasharg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpdasharg1)
		}
		arg0 := "[]float64{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
