// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func USAQueryConverter(input USAQuery) string {
	calls := []string{
		`testdata.NewUSAQueryBuilder()`,
	}
	var buffer strings.Builder
	if input.Fields != nil && len(input.Fields) >= 1 {

		buffer.WriteString(`Fields(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Fields {
			tmpfieldsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpfieldsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Mode != nil && *input.Mode != "" {

		buffer.WriteString(`Mode(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Mode))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Period != nil && *input.Period != "" {

		buffer.WriteString(`Period(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Period))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.States != nil && len(input.States) >= 1 {

		buffer.WriteString(`States(`)
		tmparg0 := []string{}
		for _, arg1 := range input.States {
			tmpstatesarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpstatesarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
