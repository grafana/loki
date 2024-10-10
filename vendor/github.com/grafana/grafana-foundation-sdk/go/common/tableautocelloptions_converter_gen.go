// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableAutoCellOptionsConverter(input TableAutoCellOptions) string {
	calls := []string{
		`common.NewTableAutoCellOptionsBuilder()`,
	}
	var buffer strings.Builder
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
