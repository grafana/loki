// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func ResultAssertionsConverter(input ResultAssertions) string {
	calls := []string{
		`testdata.NewResultAssertionsBuilder()`,
	}
	var buffer strings.Builder
	if input.MaxFrames != nil {

		buffer.WriteString(`MaxFrames(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MaxFrames))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Type != nil {

		buffer.WriteString(`Type(`)
		arg0 := cog.Dump(*input.Type)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TypeVersion != nil && len(input.TypeVersion) >= 1 {

		buffer.WriteString(`TypeVersion(`)
		tmparg0 := []string{}
		for _, arg1 := range input.TypeVersion {
			tmptypeVersionarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmptypeVersionarg1)
		}
		arg0 := "[]int64{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
