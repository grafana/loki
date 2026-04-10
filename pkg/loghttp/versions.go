package loghttp

// Version holds a loghttp version
type Version int

// Valid Version values
const (
	VersionV1 = Version(1)
)

// GetVersion returns the loghttp version for a given path.
func GetVersion(_ string) Version {
	return VersionV1
}
