package main

import (
	"os"

	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/grafana/loki/cmd/lokitool/commands"
)

var (
	ruleCommand commands.RuleCommand
)

func main() {
	app := kingpin.New("lokitool", "A command-line tool to manage loki.").Version(version.Print("lokitool"))
	ruleCommand.Register(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}
