// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func OptionsWithTimezonesConverter(input OptionsWithTimezones) string {
	calls := []string{
		`common.NewOptionsWithTimezonesBuilder()`,
	}
	var buffer strings.Builder
	if input.Timezone != nil && len(input.Timezone) >= 1 {

		buffer.WriteString(`Timezone(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Timezone {
			tmptimezonearg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmptimezonearg1)
		}
		arg0 := "[]common.TimeZone{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
