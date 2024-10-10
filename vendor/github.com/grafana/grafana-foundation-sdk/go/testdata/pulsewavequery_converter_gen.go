// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func PulseWaveQueryConverter(input PulseWaveQuery) string {
	calls := []string{
		`testdata.NewPulseWaveQueryBuilder()`,
	}
	var buffer strings.Builder
	if input.OffCount != nil {

		buffer.WriteString(`OffCount(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.OffCount))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.OffValue != nil {

		buffer.WriteString(`OffValue(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.OffValue))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.OnCount != nil {

		buffer.WriteString(`OnCount(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.OnCount))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.OnValue != nil {

		buffer.WriteString(`OnValue(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.OnValue))
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

	return strings.Join(calls, ".\t\n")
}
