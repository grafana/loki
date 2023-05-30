package sarama

import (
	"runtime/debug"
	"sync"
)

var (
	v     string
	vOnce sync.Once
)

func version() string {
	vOnce.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		if ok {
			v = bi.Main.Version
		}
		if v == "" || v == "(devel)" {
			// if we can't read a go module version then they're using a git
			// clone or vendored module so all we can do is report "dev" for
			// the version to make a valid ApiVersions request
			v = "dev"
		}
	})
	return v
}
