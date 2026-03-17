package sarama

import (
	"reflect"
	"runtime/debug"
	"sync"
)

var (
	v     string
	vOnce sync.Once
)

func version() string {
	vOnce.Do(func() {
		// Determine our package name without hardcoding a string
		type getPackageName struct{}
		thisPackagePath := reflect.TypeFor[getPackageName]().PkgPath()

		bi, ok := debug.ReadBuildInfo()
		if ok {
			for _, dep := range bi.Deps {
				if dep.Path == thisPackagePath {
					v = dep.Version
					break
				}
			}
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
