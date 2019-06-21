// +build mage

package main

import (
	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"

	// mage:import
	"github.com/grafana/loki/mage"
)

func All() error {
	mg.Deps(
		magefile.Build.Promtail,
		magefile.Build.Logcli,
		magefile.Build.Loki,
		Lint,
		Test,
	)
	return nil
}

func Lint() error {
	return sh.RunWith(map[string]string{"GOGC": "20"}, "golangci-lint", "run")
}

func Test() error {
	return sh.Run("go", "test", "-p=8", "./...")
}
