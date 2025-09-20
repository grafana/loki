package openshift

// OpenShiftVersion represents OpenShift version information.
type OpenShiftVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// IsAtLeast returns true if this version is at least the specified version.
func (v *OpenShiftVersion) IsAtLeast(major, minor int) bool {
	if v.Major > major {
		return true
	}
	if v.Major == major && v.Minor >= minor {
		return true
	}
	return false
}
