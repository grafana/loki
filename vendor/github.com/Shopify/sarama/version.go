package sarama

import "runtime/debug"

var v string

func version() string {
	if v == "" {
		bi, ok := debug.ReadBuildInfo()
		if ok {
			v = bi.Main.Version
		} else {
			// if we can't read a go module version then they're using a git
			// clone or vendored module so all we can do is report "dev" for
			// the version
			v = "dev"
		}
	}
	return v
}
