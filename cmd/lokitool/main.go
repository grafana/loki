package main

import (
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/lokitool/commands"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	app, global := commands.NewApplication()
	defer global.Close()

	if _, err := app.Parse(args); err != nil {
		log.Error(err)
		return 1
	}
	return 0
}
