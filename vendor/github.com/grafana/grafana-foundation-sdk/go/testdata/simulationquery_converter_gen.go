// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func SimulationQueryConverter(input SimulationQuery) string {
	calls := []string{
		`testdata.NewSimulationQueryBuilder()`,
	}
	var buffer strings.Builder
	if input.Config != nil {

		buffer.WriteString(`Config(`)
		arg0 := cog.Dump(input.Config)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`Key(`)
		arg0 := KeyConverter(input.Key)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Last != nil {

		buffer.WriteString(`Last(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Last))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Stream != nil {

		buffer.WriteString(`Stream(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Stream))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
