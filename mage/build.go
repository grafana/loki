package magefile

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const vendor = "github.com/grafana/loki/vendor/"

// Build Namespace groups all targets related to building applications
type Build mg.Namespace

func (Build) Promtail() error {
	return compile("promtail")
}

func (Build) Loki() error {
	return compile("loki")
}

func (Build) Logcli() error {
	return compile("logcli")
}

func compile(app string) error {
	debug := os.Getenv("DEBUG") != ""
	log.Printf("Building %v, debug=%v", app, debug)

	var flags = map[string]string{
		"ldflags": strings.Join(ldflags(debug), " "),
		"tags":    "netgo",
		"o":       fmt.Sprintf("cmd/%s/%s", app, app),
	}

	if debug {
		flags["gcflags"] = "all=-N -l"
	}

	flagSlice := joinMap(flags, "-%v=%v")
	args := append(flagSlice, "./cmd/"+app)

	return sh.RunWith(
		map[string]string{
			"CGO_ENABLED": "0",
			"GOOS":        os.Getenv("GOOS"),
			"GOARCH":      os.Getenv("GOARCH"),
		},
		"go", append([]string{"build"}, args...)...,
	)
}

func ldflags(debug bool) (flags []string) {
	var vars = joinMap(map[string]string{
		vendor + "github.com/prometheus/common/version.Branch":   revParse("--short", "HEAD"),
		vendor + "github.com/prometheus/common/version.Version":  stdout("./tools/image-tag"),
		vendor + "github.com/prometheus/common/version.Revision": revParse("--abbrev-ref", "HEAD"),
	}, "-X %v=%v")

	flags = []string{`-extldflags "-static"`}
	if !debug {
		flags = append(flags, "-s", "-w")
	}
	flags = append(flags, vars...)
	return flags
}
