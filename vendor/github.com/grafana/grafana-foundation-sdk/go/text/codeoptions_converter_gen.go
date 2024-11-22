// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package text

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func CodeOptionsConverter(input CodeOptions) string {
	calls := []string{
		`text.NewCodeOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Language(`)
		arg0 := cog.Dump(input.Language)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.ShowLineNumbers != false {

		buffer.WriteString(`ShowLineNumbers(`)
		arg0 := fmt.Sprintf("%#v", input.ShowLineNumbers)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ShowMiniMap != false {

		buffer.WriteString(`ShowMiniMap(`)
		arg0 := fmt.Sprintf("%#v", input.ShowMiniMap)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
