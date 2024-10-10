// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func CSVWaveConverter(input CSVWave) string {
	calls := []string{
		`testdata.NewCSVWaveBuilder()`,
	}
	var buffer strings.Builder
	if input.Labels != nil && *input.Labels != "" {

		buffer.WriteString(`Labels(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Labels))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Name != nil && *input.Name != "" {

		buffer.WriteString(`Name(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Name))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TimeStep != nil {

		buffer.WriteString(`TimeStep(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.TimeStep))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ValuesCSV != nil && *input.ValuesCSV != "" {

		buffer.WriteString(`ValuesCSV(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.ValuesCSV))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
