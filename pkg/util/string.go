package util

func StringRef(value string) *string {
	return &value
}

func StringSliceContains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}

	return false
}
