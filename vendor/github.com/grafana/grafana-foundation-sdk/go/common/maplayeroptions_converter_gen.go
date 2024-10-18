// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func MapLayerOptionsConverter(input MapLayerOptions) string {
	calls := []string{
		`common.NewMapLayerOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Type != "" {

		buffer.WriteString(`Type(`)
		arg0 := fmt.Sprintf("%#v", input.Type)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Name != "" {

		buffer.WriteString(`Name(`)
		arg0 := fmt.Sprintf("%#v", input.Name)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Config != nil {

		buffer.WriteString(`Config(`)
		arg0 := cog.Dump(input.Config)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Location != nil {

		buffer.WriteString(`Location(`)
		arg0 := FrameGeometrySourceConverter(*input.Location)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FilterData != nil {

		buffer.WriteString(`FilterData(`)
		arg0 := cog.Dump(input.FilterData)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Opacity != nil {

		buffer.WriteString(`Opacity(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Opacity))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Tooltip != nil {

		buffer.WriteString(`Tooltip(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Tooltip))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
