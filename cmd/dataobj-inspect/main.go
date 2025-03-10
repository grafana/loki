package main

import (
	"flag"
	"log"
	"os"

	"github.com/grafana/loki/v3/pkg/dataobj/tools"
)

func main() {
	flag.Parse()

	for _, f := range flag.Args() {
		printFile(f)
	}
}

func printFile(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}

	tools.Inspect(f, fi.Size())
}
