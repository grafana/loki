package openshift

// OpenShiftRelease represents OpenShift release information.
type OpenShiftRelease struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// IsAtLeast returns true if this release is at least the specified release.
func (v *OpenShiftRelease) IsAtLeast(major, minor int) bool {
	if v.Major > major {
		return true
	}
	if v.Major == major && v.Minor >= minor {
		return true
	}
	return false
}
