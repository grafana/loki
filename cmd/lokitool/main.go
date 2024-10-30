package main

import (
	"fmt"
	"os"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/prometheus/common/version"

	"github.com/grafana/loki/v3/pkg/tool/commands"
)

var (
	ruleCommand  commands.RuleCommand
	auditCommand commands.AuditCommand
)

func main() {
	app := kingpin.New("lokitool", "A command-line tool to manage Loki.")
	ruleCommand.Register(app)
	auditCommand.Register(app)

	app.Command("version", "Get the version of the lokitool CLI").Action(func(_ *kingpin.ParseContext) error {
		fmt.Println(version.Print("loki"))
		return nil
	})

	kingpin.MustParse(app.Parse(os.Args[1:]))
}
