// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableBarGaugeCellOptionsConverter(input TableBarGaugeCellOptions) string {
	calls := []string{
		`common.NewTableBarGaugeCellOptionsBuilder()`,
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
	if input.ValueDisplayMode != nil {

		buffer.WriteString(`ValueDisplayMode(`)
		arg0 := cog.Dump(*input.ValueDisplayMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
