package kotel

import "runtime/debug"

// version is the current release version of the kotel instrumentation.
func version() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, dep := range info.Deps {
			if dep.Path == instrumentationName {
				return dep.Version
			}
		}
	}
	return "unknown"
}

// semVersion is the semantic version to be supplied to tracer/meter creation.
func semVersion() string {
	return "semver:" + version()
}
