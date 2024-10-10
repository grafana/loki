// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"
)

func ValueMapConverter(input ValueMap) string {
	calls := []string{
		`dashboard.NewValueMapBuilder()`,
	}
	var buffer strings.Builder
	if input.Options != nil {

		buffer.WriteString(`Options(`)
		arg0 := "map[string]dashboard.ValueMappingResult{"
		for key, arg1 := range input.Options {
			tmpoptionsarg1 := ValueMappingResultConverter(arg1)
			arg0 += "\t" + fmt.Sprintf("%#v", key) + ": " + tmpoptionsarg1 + ","
		}
		arg0 += "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
