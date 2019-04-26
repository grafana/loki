// +build ignore

package main

import (
	"log"
	"time"

	"github.com/grafana/loki/pkg/promtail/server/ui"
	"github.com/prometheus/prometheus/pkg/modtimevfs"
	"github.com/shurcooL/vfsgen"
)

func main() {
	fs := modtimevfs.New(ui.Assets, time.Unix(1, 0))
	err := vfsgen.Generate(fs, vfsgen.Options{
		PackageName:  "ui",
		BuildTags:    "!dev",
		VariableName: "Assets",
	})
	if err != nil {
		log.Fatalln(err)
	}
}
