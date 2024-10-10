// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableFooterOptionsConverter(input TableFooterOptions) string {
	calls := []string{
		`common.NewTableFooterOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Show(`)
		arg0 := fmt.Sprintf("%#v", input.Show)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Reducer != nil && len(input.Reducer) >= 1 {

		buffer.WriteString(`Reducer(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Reducer {
			tmpreducerarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpreducerarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Fields != nil && len(input.Fields) >= 1 {

		buffer.WriteString(`Fields(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Fields {
			tmpfieldsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpfieldsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.EnablePagination != nil {

		buffer.WriteString(`EnablePagination(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.EnablePagination))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CountRows != nil {

		buffer.WriteString(`CountRows(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CountRows))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
