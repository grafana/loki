package util

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"text/template"

	"github.com/grafana/loki/pkg/util/build"
)

// versionInfoTmpl contains the template used by Info.
var versionInfoTmpl = `
{{.program}} v{{.version}} (revision: {{.revision}})
  go version:       {{.goVersion}}
  platform:         {{.platform}}
`

// Print returns version information.
func PrintVersion(program string) string {
	m := map[string]string{
		"program":   program,
		"version":   build.Version,
		"revision":  build.Revision,
		"goVersion": runtime.Version(),
		"platform":  runtime.GOOS + "/" + runtime.GOARCH,
	}
	t := template.Must(template.New("version").Parse(versionInfoTmpl))

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "version", m); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}

func PrintError(err error) {
	fmt.Printf("\033[31m%s\033[0m\n", err.Error())
}

func PrintSuccess(msg string) {
	fmt.Printf("\033[32m%s\033[0m\n", msg)
}
