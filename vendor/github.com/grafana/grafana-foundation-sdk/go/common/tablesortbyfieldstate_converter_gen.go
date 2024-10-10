// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableSortByFieldStateConverter(input TableSortByFieldState) string {
	calls := []string{
		`common.NewTableSortByFieldStateBuilder()`,
	}
	var buffer strings.Builder
	if input.DisplayName != "" {

		buffer.WriteString(`DisplayName(`)
		arg0 := fmt.Sprintf("%#v", input.DisplayName)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Desc != nil {

		buffer.WriteString(`Desc(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Desc))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
