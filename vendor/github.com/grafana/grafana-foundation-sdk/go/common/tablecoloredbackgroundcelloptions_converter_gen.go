// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableColoredBackgroundCellOptionsConverter(input TableColoredBackgroundCellOptions) string {
	calls := []string{
		`common.NewTableColoredBackgroundCellOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Mode != nil {

		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(*input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ApplyToRow != nil {

		buffer.WriteString(`ApplyToRow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.ApplyToRow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.WrapText != nil {

		buffer.WriteString(`WrapText(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.WrapText))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
