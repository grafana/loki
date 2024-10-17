// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"
)

func AnnotationTargetConverter(input AnnotationTarget) string {
	calls := []string{
		`dashboard.NewAnnotationTargetBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Limit(`)
		arg0 := fmt.Sprintf("%#v", input.Limit)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`MatchAny(`)
		arg0 := fmt.Sprintf("%#v", input.MatchAny)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Tags != nil && len(input.Tags) >= 1 {

		buffer.WriteString(`Tags(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Tags {
			tmptagsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmptagsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Type != "" {

		buffer.WriteString(`Type(`)
		arg0 := fmt.Sprintf("%#v", input.Type)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
