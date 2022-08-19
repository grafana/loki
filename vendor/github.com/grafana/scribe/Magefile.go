//go:build mage
// +build mage

package main

import (
	"fmt"

	"github.com/magefile/mage/sh"
)

func Version() (string, error) {
	return sh.Output("git", "describe", "--tags", "--dirty", "--always")
}

func Build() error {
	version, err := Version()
	if err != nil {
		return err
	}

	// go build \
	// -ldflags \
	// "-X main.Version=$(git describe --tags --dirty --always)" \
	// -o bin/scribe ./plumbing/cmd

	fmt.Println("building version", version)
	return sh.Run("go",
		"build",
		"-ldflags", fmt.Sprintf("-X main.Version=%s", version),
		"-o", "./bin/scribe",
		"./plumbing/cmd",
	)
}

var Default = Build
