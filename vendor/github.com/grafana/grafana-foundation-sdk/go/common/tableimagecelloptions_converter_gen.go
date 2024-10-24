// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableImageCellOptionsConverter(input TableImageCellOptions) string {
	calls := []string{
		`common.NewTableImageCellOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Alt != nil && *input.Alt != "" {

		buffer.WriteString(`Alt(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Alt))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Title != nil && *input.Title != "" {

		buffer.WriteString(`Title(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Title))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
