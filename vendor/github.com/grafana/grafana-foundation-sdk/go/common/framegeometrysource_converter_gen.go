// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func FrameGeometrySourceConverter(input FrameGeometrySource) string {
	calls := []string{
		`common.NewFrameGeometrySourceBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Geohash != nil && *input.Geohash != "" {

		buffer.WriteString(`Geohash(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Geohash))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Latitude != nil && *input.Latitude != "" {

		buffer.WriteString(`Latitude(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Latitude))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Longitude != nil && *input.Longitude != "" {

		buffer.WriteString(`Longitude(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Longitude))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Wkt != nil && *input.Wkt != "" {

		buffer.WriteString(`Wkt(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Wkt))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Lookup != nil && *input.Lookup != "" {

		buffer.WriteString(`Lookup(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Lookup))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Gazetteer != nil && *input.Gazetteer != "" {

		buffer.WriteString(`Gazetteer(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Gazetteer))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
