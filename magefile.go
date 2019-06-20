// +build mage

package main

import (
	// mage:import
	_ "github.com/grafana/loki/mage"
	"github.com/magefile/mage/sh"
)

func Lint() error {
	return sh.RunWith(map[string]string{"GOGC": "20"}, "golangci-lint", "run")
}

func Test() error {
	return sh.Run("go", "test", "-p=8", "./...")
}
