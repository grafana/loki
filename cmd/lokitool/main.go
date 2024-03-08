package main

import (
	"fmt"
	"os"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/prometheus/common/version"

	"github.com/grafana/loki/pkg/tool/commands"
)

var (
	ruleCommand commands.RuleCommand
)

func main() {
	app := kingpin.New("cortextool", "A command-line tool to manage cortex.")
	ruleCommand.Register(app)

	app.Command("version", "Get the version of the cortextool CLI").Action(func(k *kingpin.ParseContext) error {
		fmt.Println(version.Print("loki"))
		return nil
	})

	kingpin.MustParse(app.Parse(os.Args[1:]))
}
