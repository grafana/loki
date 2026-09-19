// Package main implements the logline-util CLI for inspecting and converting logline index files.
//
// Usage:
//
//	logline-util <command> [flags] <args>
//
// Commands:
//
//	dump-index  Inspect index (.lidx) files
//	convert     Convert an index to a new format version
//	compare     Compare two indexes (verify + benchmark + report)
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "dump-index", "dump-lint":
		runDumpIndex(os.Args[2:])
	case "convert":
		runConvert(os.Args[2:])
	case "compare":
		runCompare(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: logline-util <command> [flags] <args>

Commands:
  dump-index   Inspect index (.lidx) files
  convert      Convert an index to a new format version
  compare      Compare two indexes (verify + benchmark + report)

Use "logline-util <command> -h" for command-specific help.
`)
}
