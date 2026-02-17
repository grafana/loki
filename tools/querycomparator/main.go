package main

import (
	"os"

	"github.com/alecthomas/kingpin/v2"
)

func main() {
	app := kingpin.New("querycomparator", "A command-line tool to compare query results between two hosts.")
	addCompareCommand(app)
	addMetastoreCommand(app)
	addExecuteCommand(app)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}
