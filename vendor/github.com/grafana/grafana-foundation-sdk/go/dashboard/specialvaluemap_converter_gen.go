// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"strings"
)

func SpecialValueMapConverter(input SpecialValueMap) string {
	calls := []string{
		`dashboard.NewSpecialValueMapBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Options(`)
		arg0 := DashboardSpecialValueMapOptionsConverter(input.Options)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
