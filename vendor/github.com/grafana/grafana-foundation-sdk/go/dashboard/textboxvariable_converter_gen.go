// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TextBoxVariableConverter(input VariableModel) string {
	calls := []string{
		`dashboard.NewTextBoxVariableBuilder(` + fmt.Sprintf("%#v", input.Name) + `)`,
	}
	var buffer strings.Builder
	if input.Name != "" {

		buffer.WriteString(`Name(`)
		arg0 := fmt.Sprintf("%#v", input.Name)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Label != nil && *input.Label != "" {

		buffer.WriteString(`Label(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Label))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Hide != nil {

		buffer.WriteString(`Hide(`)
		arg0 := cog.Dump(*input.Hide)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Description != nil && *input.Description != "" {

		buffer.WriteString(`Description(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Description))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Query != nil {

		buffer.WriteString(`DefaultValue(`)
		arg0 := cog.Dump(*input.Query)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Current != nil {

		buffer.WriteString(`Current(`)
		arg0 := cog.Dump(*input.Current)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Options != nil && len(input.Options) >= 1 {

		buffer.WriteString(`Options(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Options {
			tmpoptionsarg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmpoptionsarg1)
		}
		arg0 := "[]dashboard.VariableOption{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
