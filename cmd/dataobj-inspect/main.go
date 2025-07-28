package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kingpin/v2"
)

func exitWithError(err error) {
	fmt.Fprint(os.Stderr, err.Error())
	os.Exit(1)
}

func main() {
	app := kingpin.New("dataobj-inspect", "A command-line tool to inspect data objects.")
	addDumpCommand(app)
	addStatsCommand(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}
